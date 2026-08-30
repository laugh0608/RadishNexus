package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

const (
	loginAttemptLimit             = 5
	loginWindowDuration           = time.Minute
	loginTrackedClientLimit       = 4096
	loginPasswordConcurrencyLimit = 4
)

func main() {
	if err := run(); err != nil {
		log.Printf("radishnexus server stopped: %v", err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	address := os.Getenv("RADISHNEXUS_HTTP_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	sessionPolicy, err := httptransport.NewBrowserSessionPolicy(os.Getenv("RADISHNEXUS_PUBLIC_ORIGIN"))
	if err != nil {
		return fmt.Errorf("configure public browser origin: %w", err)
	}
	proxyPolicy, err := httptransport.NewTrustedProxyPolicy(os.Getenv("RADISHNEXUS_TRUSTED_PROXY_CIDRS"))
	if err != nil {
		return fmt.Errorf("configure trusted proxies: %w", err)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return err
	}
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "radishnexus-server"
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return err
	}
	defer pool.Close()
	authService := authn.NewService(
		authpostgres.New(pool),
		authn.NewArgon2idHasher(),
		authn.CryptoSecretGenerator{},
		authn.SystemClock{},
	)
	authHandler := httptransport.NewAuthHandler(
		authService,
		sessionPolicy,
		proxyPolicy,
		httptransport.NewLoginGuard(
			loginAttemptLimit,
			loginWindowDuration,
			loginTrackedClientLimit,
			loginPasswordConcurrencyLimit,
		),
	)

	server := &http.Server{
		Addr:              address,
		Handler:           newHandler(pool, authHandler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	signals, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-signals.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return err
		}
		return nil
	}
}

type databasePinger interface {
	Ping(context.Context) error
}

func newHandler(database databasePinger, authHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /health/ready", func(response http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := database.Ping(ctx); err != nil {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/api/v1/auth", authHandler)
	mux.Handle("/api/v1/auth/", authHandler)

	return httptransport.WithRequestID(mux)
}
