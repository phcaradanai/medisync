// Command core is the MediSync backend: a modular monolith hosting the
// identity, catalog, inventory, dispensing, fulfillment and printing
// bounded contexts.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/adm-chura3inter/medisync/services/core/internal/catalog"
	"github.com/adm-chura3inter/medisync/services/core/internal/dispensing"
	cabinetv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/cabinet/v1/cabinetv1connect"
	catalogv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/catalog/v1/catalogv1connect"
	dispensingv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/dispensing/v1/dispensingv1connect"
	identityv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/identity/v1/identityv1connect"
	inventoryv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/inventory/v1/inventoryv1connect"
	kioskv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/kiosk/v1/kioskv1connect"
	"github.com/adm-chura3inter/medisync/services/core/internal/identity"
	"github.com/adm-chura3inter/medisync/services/core/internal/inventory"
	"github.com/adm-chura3inter/medisync/services/core/internal/cabinet"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/logging"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/postgres"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/ratelimit"
	"github.com/adm-chura3inter/medisync/services/core/internal/printing"
	"github.com/adm-chura3inter/medisync/services/core/internal/vending"
	"github.com/adm-chura3inter/medisync/services/core/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	// Load .env from project root (../../.env relative to services/core/ dir).
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../../.env")

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New(cfg.LogLevel)

	// Log the HTTP address (safe) and that JWT+password+card config is present
	// (properties, never values).
	log.Info("starting core",
		"http_addr", cfg.HTTPAddr,
		"jwt_expiry_seconds", cfg.JWTExpirySeconds,
		"jwt_secret_configured", cfg.JWTSecret != "",
		"admin_password_configured", cfg.AdminBootstrapPassword != "",
		"card_token_hmac_configured", cfg.CardTokenHMACKey != "",
		"login_rate_limit_max", cfg.LoginRateLimitMax,
		"login_rate_limit_window_seconds", cfg.LoginRateLimitWindowSeconds,
		"print_ops_url", cfg.PrintOpsURL,
		"print_ops_fake", cfg.PrintOpsFake,
		"vending_url", cfg.VendingURL,
		"fulfillment_fake", cfg.FulfillmentFake,
	)

	startupCtx, cancelStartup := context.WithTimeout(context.Background(),
		time.Duration(cfg.StartupTimeoutSeconds)*time.Second)
	defer cancelStartup()

	pool, err := postgres.NewPool(startupCtx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := postgres.Migrate(startupCtx, pool, migrations.FS, log); err != nil {
		return err
	}
	log.Info("database ready")

	nc, err := natsx.Connect(startupCtx, cfg.NATSURL, log)
	if err != nil {
		return err
	}
	defer func() {
		if err := nc.Drain(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("drain nats: %w", err))
		}
	}()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("jetstream context: %w", err)
	}
	if err := natsx.EnsureStreams(startupCtx, js); err != nil {
		return err
	}
	log.Info("nats streams ready")

	auditw := audit.NewWriter(pool)
	consumer := dispensing.NewConsumer(js, dispensing.NewStore(pool), auditw, log)
	stopConsumer, err := consumer.Start(startupCtx)
	if err != nil {
		return err
	}
	defer stopConsumer()

	// ── Identity ─────────────────────────────────────────────────────
	cardHasher, err := identity.NewCardTokenHasher(cfg.CardTokenHMACKey)
	if err != nil {
		return fmt.Errorf("card-token hasher: %w", err)
	}
	identityStore := identity.NewStoreWithHasher(pool, cardHasher)

	jwtMgr, err := identity.NewJWTManager(
		cfg.JWTSecret,
		time.Duration(cfg.JWTExpirySeconds)*time.Second,
		nil, // nil clock uses real time
	)
	if err != nil {
		return fmt.Errorf("jwt manager: %w", err)
	}

	// Hash the bootstrap password with bcrypt before storage.
	// The plaintext never appears in any log or persisted row.
	created, err := bootstrapAdmin(startupCtx, identityStore, cfg.AdminBootstrapPassword)
	if err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}
	// Drop the remaining reference after bootstrap. Go strings cannot be
	// securely zeroed, so the process must still avoid logging or retaining it.
	cfg.AdminBootstrapPassword = ""
	if created {
		log.Info("admin user created")
	} else {
		log.Info("admin user already exists, skipping seed")
	}

	mux := http.NewServeMux()
	path, handler := newIdentityHandler(identityStore, jwtMgr, cfg)
	mux.Handle(path, handler)

	// Project management (SYSADMIN-only).
	projectPath, projectHandler := newProjectHandler(identityStore)
	mux.Handle(projectPath, projectHandler)

	// Kiosk provisioning and kiosk-token authentication.
	kioskStore := identity.NewKioskStore(pool)
	kioskPath, kioskHandler := newKioskHandler(kioskStore, jwtMgr, cfg)
	mux.Handle(kioskPath, kioskHandler)

	// ── Cabinet ──────────────────────────────────────────────────────
	cabinetStore := cabinet.NewStore(pool)
	cabinetServer := cabinet.NewServer(cabinetStore, newCabinetTokenParser(jwtMgr))
	cabinetPath, cabinetHandler := cabinetv1connect.NewCabinetServiceHandler(cabinetServer)
	mux.Handle(cabinetPath, cabinetHandler)

	// ── Catalog ─────────────────────────────────────────────────────
	catalogStore := catalog.NewStore(pool, auditw)
	catalogServer := catalog.NewCatalogServerWithAuth(catalogStore, auditw, newCatalogTokenParser(jwtMgr))
	catalogPath, catalogHandler := catalogv1connect.NewCatalogServiceHandler(catalogServer)
	mux.Handle(catalogPath, catalogHandler)

	// ── Inventory ──────────────────────────────────────────────────
	inventoryStore := inventory.NewStore(pool, auditw)
	inventoryServer := inventory.NewInventoryServerWithAuth(inventoryStore, auditw, js, newInventoryTokenParser(jwtMgr))
	inventoryPath, inventoryHandler := inventoryv1connect.NewInventoryServiceHandler(inventoryServer)
	mux.Handle(inventoryPath, inventoryHandler)

	// ── Dispensing ────────────────────────────────────────────────
	dispensingStore := dispensing.NewStore(pool)
	dispensingServer := dispensing.NewDispensingServer(
		dispensingStore, pool,
		newDispensingTokenParser(jwtMgr),
		auditw,
	)
	dispensingPath, dispensingHandler := dispensingv1connect.NewDispensingServiceHandler(dispensingServer)
	mux.Handle(dispensingPath, dispensingHandler)

	// Outbox publisher: polls dispensing.outbox and publishes to NATS.
	// Runs in its own goroutine; stopped before NATS drain on shutdown.
	publisherCtx, cancelPublisher := context.WithCancel(context.Background())
	defer cancelPublisher()
	outboxPub := dispensing.NewOutboxPublisher(pool, js, log)
	go outboxPub.Start(publisherCtx)

	// Dispense completion consumer: listens to medisync.dispense.completed
	// and medisync.dispense.failed, transitions prescription state.
	completionConsumer := dispensing.NewCompletionConsumer(js, pool, dispensingStore, auditw, log)
	stopCompletion, err := completionConsumer.Start(startupCtx)
	if err != nil {
		return err
	}
	defer stopCompletion()

	// ── Printing ──────────────────────────────────────────────────────
	printClient := printing.NewClientFromConfig(cfg)
	printConsumer := printing.NewConsumer(js, printClient, auditw, log)
	stopPrintConsumer, err := printConsumer.Start(startupCtx)
	if err != nil {
		return err
	}
	defer stopPrintConsumer()

	// ── Vending ───────────────────────────────────────────────────────
	// DispenseRequestedConsumer bridges dispense.requested → fulfillment.requested.
	dispReqConsumer := dispensing.NewDispenseRequestedConsumer(js, log)
	stopDispReq, err := dispReqConsumer.Start(startupCtx)
	if err != nil {
		return err
	}
	defer stopDispReq()

	vendingClient := vending.NewClientFromConfig(cfg)
	vendingConsumer := vending.NewConsumer(js, vendingClient, auditw, log)
	stopVendingConsumer, err := vendingConsumer.Start(startupCtx)
	if err != nil {
		return err
	}
	defer stopVendingConsumer()

	// ── HTTP server ──────────────────────────────────────────────────
	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: mux,
		// ReadHeaderTimeout prevents slow-loris attacks.
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", srv.Addr, err)
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Serve(listener)
	}()

	log.Info("core started", "http_addr", cfg.HTTPAddr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve http: %w", err)
		}
		return nil
	}

	log.Info("shutting down")

	// Shutdown the HTTP server with a deadline.
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve http during shutdown: %w", err)
	}

	log.Info("shutdown complete")
	return nil
}

type adminSeeder interface {
	SeedAdmin(context.Context, string) (bool, error)
}

func bootstrapAdmin(ctx context.Context, store adminSeeder, password string) (bool, error) {
	passwordHash, err := identity.HashPassword(password)
	if err != nil {
		return false, fmt.Errorf("hash bootstrap password: %w", err)
	}
	return store.SeedAdmin(ctx, passwordHash)
}

func newIdentityHandler(store identity.UserStore, tokens identity.TokenManager, cfg config.Config) (string, http.Handler) {
	authService := identity.NewAuthService(store, tokens)

	// Create rate limiters for login endpoints.
	window := time.Duration(cfg.LoginRateLimitWindowSeconds) * time.Second
	idLimiter := ratelimit.New(cfg.LoginRateLimitMax, window)
	ipLimiter := ratelimit.New(cfg.LoginRateLimitMax, window)

	var identityStore *identity.Store
	if s, ok := store.(*identity.Store); ok {
		identityStore = s
	}

	server := identity.NewIdentityServerWithRateLimit(authService, identityStore, idLimiter, ipLimiter)
	return identityv1connect.NewIdentityServiceHandler(server)
}

func newProjectHandler(store *identity.Store) (string, http.Handler) {
	server := identity.NewProjectServer(store)
	return identityv1connect.NewProjectServiceHandler(server)
}

// newDispensingTokenParser adapts identity.JWTManager to dispensing.TokenParser.
// The dispensing handler defines its own TokenClaims type to avoid a circular
// dependency on package identity.
func newKioskHandler(store identity.KioskStore, jwtMgr *identity.JWTManager, cfg config.Config) (string, http.Handler) {
	window := time.Duration(cfg.LoginRateLimitWindowSeconds) * time.Second
	idLimiter := ratelimit.New(cfg.LoginRateLimitMax, window)
	ipLimiter := ratelimit.New(cfg.LoginRateLimitMax, window)
	server := identity.NewKioskServerWithRateLimit(store, jwtMgr, jwtMgr, idLimiter, ipLimiter)
	return kioskv1connect.NewKioskServiceHandler(server)
}

func newDispensingTokenParser(mgr *identity.JWTManager) *dispensingTokenParser {
	return &dispensingTokenParser{mgr: mgr}
}

type dispensingTokenParser struct {
	mgr *identity.JWTManager
}

func (p *dispensingTokenParser) Parse(tokenString string) (*dispensing.TokenClaims, error) {
	// Try kiosk token first.
	kClaims, kErr := p.mgr.ParseKiosk(tokenString)
	if kErr == nil {
		return &dispensing.TokenClaims{
			Subject:   kClaims.Code,
			Role:      "KIOSK",
			ProjectID: kClaims.ProjectID,
		}, nil
	}

	claims, err := p.mgr.Parse(tokenString)
	if err != nil {
		return nil, err
	}
	return &dispensing.TokenClaims{
		Subject:   claims.Subject,
		Role:      claims.Role,
		ProjectID: claims.ProjectID,
		WardIDs:   claims.WardIDs,
	}, nil
}

// ── Cabinet token parser adapter ──────────────────────────────────

func newCabinetTokenParser(mgr *identity.JWTManager) *cabinetTokenParser {
	return &cabinetTokenParser{mgr: mgr}
}

type cabinetTokenParser struct {
	mgr *identity.JWTManager
}

func (p *cabinetTokenParser) Parse(tokenString string) (*cabinet.TokenClaims, error) {
	claims, err := p.mgr.Parse(tokenString)
	if err != nil {
		return nil, err
	}
	return &cabinet.TokenClaims{
		Subject:   claims.Subject,
		Role:      claims.Role,
		ProjectID: claims.ProjectID,
		WardIDs:   claims.WardIDs,
	}, nil
}

// ── Catalog token parser adapter ──────────────────────────────────

func newCatalogTokenParser(mgr *identity.JWTManager) *catalogTokenParser {
	return &catalogTokenParser{mgr: mgr}
}

type catalogTokenParser struct {
	mgr *identity.JWTManager
}

func (p *catalogTokenParser) Parse(tokenString string) (catalog.TokenClaimser, error) {
	claims, err := p.mgr.Parse(tokenString)
	if err != nil {
		return nil, err
	}
	return catalog.Claims{
		Subject:   claims.Subject,
		Role:      claims.Role,
		ProjectID: claims.ProjectID,
	}, nil
}

// ── Inventory token parser adapter ────────────────────────────────

func newInventoryTokenParser(mgr *identity.JWTManager) *inventoryTokenParser {
	return &inventoryTokenParser{mgr: mgr}
}

type inventoryTokenParser struct {
	mgr *identity.JWTManager
}

func (p *inventoryTokenParser) Parse(tokenString string) (inventory.TokenClaimser, error) {
	// Try kiosk token first (KIOSK role for refill operations).
	kClaims, kErr := p.mgr.ParseKiosk(tokenString)
	if kErr == nil {
		return inventory.Claims{
			Subject:   kClaims.Subject,
			Role:      "KIOSK",
			ProjectID: kClaims.ProjectID,
		}, nil
	}
	// Fall back to user token (ADMIN role).
	claims, err := p.mgr.Parse(tokenString)
	if err != nil {
		return nil, err
	}
	return inventory.Claims{
		Subject:   claims.Subject,
		Role:      claims.Role,
		ProjectID: claims.ProjectID,
	}, nil
}
