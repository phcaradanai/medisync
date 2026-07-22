// Command core is the MediSync backend: a modular monolith hosting the
// identity, catalog, inventory, dispensing, fulfillment and printing
// bounded contexts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"github.com/joho/godotenv"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/adm-chura3inter/medisync/services/core/internal/catalog"
	"github.com/adm-chura3inter/medisync/services/core/internal/dispensing"
	catalogv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/catalog/v1/catalogv1connect"
	dispensingv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/dispensing/v1/dispensingv1connect"
	identityv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/identity/v1/identityv1connect"
	inventoryv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/inventory/v1/inventoryv1connect"
	kioskv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/kiosk/v1/kioskv1connect"
	"github.com/adm-chura3inter/medisync/services/core/internal/identity"
	"github.com/adm-chura3inter/medisync/services/core/internal/inventory"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/logging"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/postgres"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/ratelimit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/tracing"
	"github.com/adm-chura3inter/medisync/services/core/internal/printing"
	"github.com/adm-chura3inter/medisync/services/core/internal/scanner"
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
		"vending_routes_configured", cfg.VendingEndpointsJSON != "",
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
	scannerBroker := scanner.NewBroker()
	scannerConsumer := scanner.NewConsumer(js, scannerBroker, log)
	stopScannerConsumer, err := scannerConsumer.Start(startupCtx)
	if err != nil {
		return err
	}
	defer stopScannerConsumer()

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
	rpcOptions := []connect.HandlerOption{
		connect.WithInterceptors(tracing.NewInterceptor(log)),
	}
	path, handler := newIdentityHandler(identityStore, jwtMgr, cfg, auditw, rpcOptions...)
	mux.Handle(path, handler)

	// Project management (SYSADMIN-only).
	projectPath, projectHandler := newProjectHandler(identityStore, auditw, rpcOptions...)
	mux.Handle(projectPath, projectHandler)

	// Kiosk provisioning and kiosk-token authentication.
	kioskStore := identity.NewKioskStore(pool)
	kioskPath, kioskHandler := newKioskHandler(kioskStore, jwtMgr, cfg, auditw, rpcOptions...)
	mux.Handle(kioskPath, kioskHandler)

	// ── Catalog ─────────────────────────────────────────────────────
	catalogStore := catalog.NewStore(pool, auditw)
	catalogServer := catalog.NewCatalogServerWithAuth(catalogStore, auditw, newCatalogTokenParser(jwtMgr))
	catalogPath, catalogHandler := catalogv1connect.NewCatalogServiceHandler(catalogServer, rpcOptions...)
	mux.Handle(catalogPath, catalogHandler)

	// ── Inventory ──────────────────────────────────────────────────
	inventoryStore := inventory.NewStore(pool, auditw)
	inventoryServer := inventory.NewInventoryServerWithAuth(inventoryStore, auditw, js, newInventoryTokenParser(jwtMgr))
	inventoryPath, inventoryHandler := inventoryv1connect.NewInventoryServiceHandler(inventoryServer, rpcOptions...)
	mux.Handle(inventoryPath, inventoryHandler)

	// ── Dispensing ────────────────────────────────────────────────
	dispensingStore := dispensing.NewStore(pool)
	reaperCtx, cancelReaper := context.WithCancel(context.Background())
	defer cancelReaper()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-reaperCtx.Done():
				return
			case <-ticker.C:
				if count, err := dispensingStore.ExpireStaleTransactions(reaperCtx, pool.Begin); err != nil {
					log.Error("expire stale dispense transactions", "error", err)
				} else if count > 0 {
					log.Info("expired stale dispense transactions", "count", count)
				}
			}
		}
	}()
	dispensingServer := dispensing.NewDispensingServer(
		dispensingStore, pool,
		newDispensingTokenParser(jwtMgr),
		auditw,
	)
	dispensingPath, dispensingHandler := dispensingv1connect.NewDispensingServiceHandler(dispensingServer, rpcOptions...)
	mux.Handle(dispensingPath, dispensingHandler)

	// Health check endpoint — reports DB, NATS, and consumer status.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		status := map[string]any{"status": "ok"}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		// DB ping
		if err := pool.Ping(ctx); err != nil {
			status["database"] = map[string]any{"status": "error", "message": err.Error()}
		} else {
			status["database"] = "ok"
		}

		// NATS status (check if JetStream is available)
		if _, err := js.AccountInfo(ctx); err != nil {
			status["nats"] = map[string]any{"status": "error", "message": err.Error()}
		} else {
			status["nats"] = "ok"
		}

		w.Header().Set("Content-Type", "application/json")
		if status["database"] != "ok" || status["nats"] != "ok" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		fmt.Fprintf(w, `{"status":"%s","database":"%s","nats":"%s"}`,
			func() string {
				if status["database"] != "ok" || status["nats"] != "ok" {
					return "degraded"
				}
				return "ok"
			}(),
			status["database"], status["nats"])
	})

	// Audit log endpoint — paginated read access to audit trail.
	mux.HandleFunc("/audit", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		projectID := q.Get("project_id")
		pageSize := int32(50)
		if ps := q.Get("page_size"); ps != "" {
			fmt.Sscanf(ps, "%d", &pageSize)
		}
		pageToken := q.Get("page_token")

		entries, total, nextToken, err := audit.List(r.Context(), pool, projectID, pageSize, pageToken)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"error":"%s"}`, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"entries": entries, "total_count": total, "next_page_token": nextToken,
		})
	})

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

	vendingRouter, err := vending.NewRouterFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("configure vending router: %w", err)
	}
	dispensingServer.SetHardwareHealthChecker(func(ctx context.Context, kioskCode string) error {
		client, err := vendingRouter.ClientFor(kioskCode)
		if err != nil {
			return err
		}
		return client.Health(ctx)
	})
	vendingConsumer := vending.NewRoutedConsumer(js, vendingRouter, dispensingStore, auditw, log)
	stopVendingConsumer, err := vendingConsumer.Start(startupCtx)
	if err != nil {
		return err
	}
	defer stopVendingConsumer()

	// Physical scanner stream. Vending agents publish QR/barcode/NFC frames to
	// Core's JetStream subject; this endpoint only fans out events for the
	// authenticated kiosk code in the URL.
	mux.HandleFunc("/api/v1/kiosks/", func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/api/v1/kiosks/"
		tail := strings.TrimPrefix(r.URL.Path, prefix)
		parts := strings.Split(strings.Trim(tail, "/"), "/")
		if r.Method != http.MethodGet || len(parts) != 3 || parts[1] != "scanner" || parts[2] != "events" {
			http.NotFound(w, r)
			return
		}
		kioskCode := parts[0]
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "missing kiosk authorization", http.StatusUnauthorized)
			return
		}
		claims, parseErr := jwtMgr.ParseKiosk(strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")))
		if parseErr != nil || claims.Code != kioskCode {
			http.Error(w, "kiosk authorization does not match route", http.StatusForbidden)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, _ := w.(http.Flusher)
		ready, _ := json.Marshal(map[string]string{"kiosk_code": kioskCode})
		if _, err := fmt.Fprintf(w, "event: ready\ndata: %s\n\n", ready); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		events, unsubscribe := scannerBroker.Subscribe(r.Context(), kioskCode)
		defer unsubscribe()
		keepAlive := time.NewTicker(25 * time.Second)
		defer keepAlive.Stop()
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return
				}
				data, marshalErr := json.Marshal(event)
				if marshalErr != nil {
					log.Warn("marshal kiosk scanner event failed", "kiosk_code", kioskCode, "error", marshalErr.Error())
					continue
				}
				if _, writeErr := fmt.Fprintf(w, "event: scan\ndata: %s\n\n", data); writeErr != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			case <-keepAlive.C:
				if _, writeErr := fmt.Fprint(w, ": keepalive\n\n"); writeErr != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			case <-r.Context().Done():
				return
			}
		}
	})

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

func newIdentityHandler(store identity.UserStore, tokens identity.TokenManager, cfg config.Config, auditw *audit.Writer, options ...connect.HandlerOption) (string, http.Handler) {
	authService := identity.NewAuthService(store, tokens)

	// Create rate limiters for login endpoints.
	window := time.Duration(cfg.LoginRateLimitWindowSeconds) * time.Second
	idLimiter := ratelimit.New(cfg.LoginRateLimitMax, window)
	ipLimiter := ratelimit.New(cfg.LoginRateLimitMax, window)

	var identityStore *identity.Store
	if s, ok := store.(*identity.Store); ok {
		identityStore = s
	}

	server := identity.NewIdentityServerWithRateLimit(authService, identityStore, idLimiter, ipLimiter, auditw)
	return identityv1connect.NewIdentityServiceHandler(server, options...)
}

func newProjectHandler(store *identity.Store, auditw *audit.Writer, options ...connect.HandlerOption) (string, http.Handler) {
	server := identity.NewProjectServer(store, auditw)
	return identityv1connect.NewProjectServiceHandler(server, options...)
}

// newDispensingTokenParser adapts identity.JWTManager to dispensing.TokenParser.
// The dispensing handler defines its own TokenClaims type to avoid a circular
// dependency on package identity.
func newKioskHandler(store identity.KioskStore, jwtMgr *identity.JWTManager, cfg config.Config, auditw *audit.Writer, options ...connect.HandlerOption) (string, http.Handler) {
	window := time.Duration(cfg.LoginRateLimitWindowSeconds) * time.Second
	idLimiter := ratelimit.New(cfg.LoginRateLimitMax, window)
	ipLimiter := ratelimit.New(cfg.LoginRateLimitMax, window)
	server := identity.NewKioskServerWithRateLimit(store, jwtMgr, jwtMgr, idLimiter, ipLimiter, auditw)
	return kioskv1connect.NewKioskServiceHandler(server, options...)
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
			Subject:   kClaims.Code,
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
