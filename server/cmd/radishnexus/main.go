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

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
	"github.com/laugh0608/RadishNexus/server/internal/platform/realtime"
	"github.com/laugh0608/RadishNexus/server/internal/platform/runtimeconfig"
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
	databaseURL, err := runtimeconfig.DatabaseURL(os.Getenv, os.ReadFile)
	if err != nil {
		return err
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
	webHandler, err := httptransport.NewWebAppHandler(os.Getenv("RADISHNEXUS_WEB_ROOT"))
	if err != nil {
		return fmt.Errorf("configure Web App: %w", err)
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
	realtimeConfig, err := realtime.DefaultConfig()
	if err != nil {
		return err
	}
	realtimeHub, err := realtime.NewHub(realtimeConfig)
	if err != nil {
		return fmt.Errorf("configure Message realtime hub: %w", err)
	}
	defer realtimeHub.Shutdown()
	nexusViewService := goldenpath.NewService(
		goldenpostgres.New(pool),
		goldenpath.CryptoIDGenerator{},
		goldenpath.SystemClock{},
		goldenpath.WithMessageCreatedNotifier(messageRealtimeNotifier{hub: realtimeHub}),
	)
	deploymentNexusViewHandler := httptransport.NewDeploymentNexusViewHandler(
		authService,
		nexusViewService,
		sessionPolicy,
		proxyPolicy,
	)
	channelMessagesHandler := httptransport.NewChannelMessagesHandler(
		authService,
		nexusViewService,
		sessionPolicy,
		proxyPolicy,
	)
	channelEventsHandler := httptransport.NewChannelEventsHandler(
		authService,
		nexusViewService,
		realtimeHub,
		sessionPolicy,
		proxyPolicy,
	)
	collaborationHandler := httptransport.NewCollaborationHandler(
		authService,
		nexusViewService,
		sessionPolicy,
		proxyPolicy,
	)

	server := &http.Server{
		Addr:              address,
		Handler:           newHandler(pool, authHandler, channelMessagesHandler, channelEventsHandler, collaborationHandler, deploymentNexusViewHandler, webHandler),
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
		realtimeHub.Shutdown()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return err
		}
		return nil
	}
}

type messageRealtimeNotifier struct {
	hub *realtime.Hub
}

func (notifier messageRealtimeNotifier) NotifyMessageCreated(notification goldenpath.MessageCreatedNotification) {
	notifier.hub.NotifyMessageCreated(realtime.MessageNotification{
		WorkspaceID: notification.WorkspaceID,
		ChannelID:   notification.ChannelID,
		MessageID:   notification.MessageID,
	})
}

type databasePinger interface {
	Ping(context.Context) error
}

func newHandler(
	database databasePinger,
	authHandler http.Handler,
	channelMessagesHandler http.Handler,
	channelEventsHandler http.Handler,
	collaborationHandler http.Handler,
	deploymentNexusViewHandler http.Handler,
	webHandler http.Handler,
) http.Handler {
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
	mux.HandleFunc("/health/live", healthMethodNotAllowed)
	mux.HandleFunc("/health/ready", healthMethodNotAllowed)
	mux.Handle("/api/v1/auth", authHandler)
	mux.Handle("/api/v1/auth/", authHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages", channelMessagesHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages/", channelMessagesHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/events", channelEventsHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/events/", channelEventsHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/threads/", collaborationHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/decisions/", collaborationHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/tickets/", collaborationHandler)
	mux.Handle("/api/v1/workspaces", deploymentNexusViewHandler)
	mux.Handle("/api/v1/workspaces/", deploymentNexusViewHandler)
	mux.Handle("/", webHandler)

	return httptransport.WithRequestID(mux)
}

func healthMethodNotAllowed(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Allow", http.MethodGet)
	response.WriteHeader(http.StatusMethodNotAllowed)
}
