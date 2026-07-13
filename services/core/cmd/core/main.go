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

	"github.com/nats-io/nats.go/jetstream"

	"github.com/adm-chura3inter/medisync/services/core/internal/dispensing"
	identityv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/identity/v1/identityv1connect"
	"github.com/adm-chura3inter/medisync/services/core/internal/identity"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/logging"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/postgres"
	"github.com/adm-chura3inter/medisync/services/core/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() (runErr error) {
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
	path, handler := newIdentityHandler(identityStore, jwtMgr)
	mux.Handle(path, handler)

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

func newIdentityHandler(store identity.UserStore, tokens identity.TokenManager) (string, http.Handler) {
	authService := identity.NewAuthService(store, tokens)
	server := identity.NewIdentityServer(authService)
	return identityv1connect.NewIdentityServiceHandler(server)
}
