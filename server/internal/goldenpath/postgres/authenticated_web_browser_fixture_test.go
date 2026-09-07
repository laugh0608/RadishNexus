//go:build integration && browserfixture

package postgres_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
	"github.com/laugh0608/RadishNexus/server/internal/platform/realtime"
)

const authenticatedWebBrowserPassword = "authenticated browser fixture password"

func TestAuthenticatedWebBrowserFixture(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	webRoot := os.Getenv("RADISHNEXUS_WEB_ROOT")
	statePath := os.Getenv("RADISHNEXUS_BROWSER_FIXTURE_STATE")
	stopPath := os.Getenv("RADISHNEXUS_BROWSER_FIXTURE_STOP")
	listenAddress := os.Getenv("RADISHNEXUS_BROWSER_FIXTURE_LISTEN_ADDRESS")
	publicOrigin := os.Getenv("RADISHNEXUS_BROWSER_FIXTURE_PUBLIC_ORIGIN")
	if databaseURL == "" || webRoot == "" || statePath == "" || stopPath == "" || listenAddress == "" || publicOrigin == "" {
		t.Fatal("DATABASE_URL, RADISHNEXUS_WEB_ROOT, RADISHNEXUS_BROWSER_FIXTURE_STATE, RADISHNEXUS_BROWSER_FIXTURE_STOP, RADISHNEXUS_BROWSER_FIXTURE_LISTEN_ADDRESS and RADISHNEXUS_BROWSER_FIXTURE_PUBLIC_ORIGIN are required")
	}

	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgx.Connect() error = %v", err)
	}
	if err := db.Migrate(ctx, connection); err != nil {
		t.Fatalf("db.Migrate() error = %v", err)
	}
	if err := connection.Close(ctx); err != nil {
		t.Fatalf("migration connection Close() error = %v", err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)

	fixture := seedAuthenticatedWebBrowserData(t, ctx, pool)
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	sessionPolicy, err := httptransport.NewBrowserSessionPolicy(publicOrigin)
	if err != nil {
		t.Fatalf("NewBrowserSessionPolicy() error = %v", err)
	}
	proxyPolicy, err := httptransport.NewTrustedProxyPolicy("127.0.0.0/8")
	if err != nil {
		t.Fatalf("NewTrustedProxyPolicy() error = %v", err)
	}

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
		httptransport.NewLoginGuard(5, time.Minute, 64, 2),
	)
	realtimeConfig, err := realtime.DefaultConfig()
	if err != nil {
		t.Fatalf("realtime.DefaultConfig() error = %v", err)
	}
	realtimeHub, err := realtime.NewHub(realtimeConfig)
	if err != nil {
		t.Fatalf("realtime.NewHub() error = %v", err)
	}
	t.Cleanup(realtimeHub.Shutdown)
	viewService := goldenpath.NewService(
		goldenpostgres.New(pool),
		goldenpath.CryptoIDGenerator{},
		goldenpath.SystemClock{},
		goldenpath.WithMessageCreatedNotifier(authenticatedBrowserNotifier{hub: realtimeHub}),
	)
	deploymentHandler := httptransport.NewDeploymentNexusViewHandler(
		authService,
		viewService,
		sessionPolicy,
		proxyPolicy,
	)
	channelHandler := httptransport.NewChannelMessagesHandler(
		authService,
		viewService,
		sessionPolicy,
		proxyPolicy,
	)
	channelEventsHandler := httptransport.NewChannelEventsHandler(
		authService,
		viewService,
		realtimeHub,
		sessionPolicy,
		proxyPolicy,
	)
	collaborationHandler := httptransport.NewCollaborationHandler(
		authService,
		viewService,
		sessionPolicy,
		proxyPolicy,
	)
	webHandler, err := httptransport.NewWebAppHandler(webRoot)
	if err != nil {
		t.Fatalf("NewWebAppHandler() error = %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/v1/auth", authHandler)
	mux.Handle("/api/v1/auth/", authHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages", channelHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages/", channelHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/events", channelEventsHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/threads/", collaborationHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/decisions/", collaborationHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/tickets/", collaborationHandler)
	mux.Handle("/api/v1/workspaces", deploymentHandler)
	mux.Handle("/api/v1/workspaces/", deploymentHandler)
	mux.Handle("/", webHandler)
	server := httptest.NewUnstartedServer(httptransport.WithRequestID(mux))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	state, err := json.Marshal(map[string]string{
		"public_origin":      publicOrigin,
		"channel_path":       "/workspaces/wrk_main/channels/chn_project",
		"thread_path":        "/workspaces/wrk_main/threads/" + fixture.thread.ID,
		"restricted_thread":  fixture.thread.ID,
		"database_container": os.Getenv("RADISHNEXUS_BROWSER_FIXTURE_DATABASE_CONTAINER"),
		"deployment_path":    "/workspaces/wrk_main/deployments/" + fixture.deployment.ID,
		"contributor_login":  "http.contributor",
		"decider_login":      "http.decider",
		"password":           authenticatedWebBrowserPassword,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(statePath, append(state, '\n'), 0o600); err != nil {
		t.Fatalf("write fixture state: %v", err)
	}
	waitForBrowserFixtureStop(t, stopPath)
}

type authenticatedBrowserNotifier struct {
	hub *realtime.Hub
}

func (notifier authenticatedBrowserNotifier) NotifyMessageCreated(notification goldenpath.MessageCreatedNotification) {
	notifier.hub.NotifyMessageCreated(realtime.MessageNotification{
		WorkspaceID: notification.WorkspaceID,
		ChannelID:   notification.ChannelID,
		MessageID:   notification.MessageID,
	})
}

type authenticatedWebFixture struct {
	deployment goldenpath.Deployment
	thread     goldenpath.Thread
}

func seedAuthenticatedWebBrowserData(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) authenticatedWebFixture {
	t.Helper()
	seedGoldenPath(t, ctx, pool)
	seedDeploymentTargets(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.components (
			id, workspace_id, key, name, type, owner_team_id, lifecycle,
			created_by_kind, created_by_id
		) VALUES (
			'cmp_auth', 'wrk_main', 'AUTH-SERVICE', 'Authentication Service',
			'service', 'tem_main', 'active', 'user', 'usr_contributor'
		)
	`); err != nil {
		t.Fatalf("seed browser fixture Component: %v", err)
	}

	store := goldenpostgres.New(pool)
	clock := fixedClock{now: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	service := goldenpath.NewService(store, &prefixedSequenceIDs{}, clock)
	startedAt := time.Date(2026, 8, 30, 11, 50, 0, 0, time.UTC)
	ciRun, err := service.RecordCompletedJenkinsRun(ctx, goldenpath.VerifiedJenkinsDelivery{
		WorkspaceID:   "wrk_main",
		SourceID:      "jenkins-browser-fixture",
		DeliveryID:    "browser-fixture-delivery",
		PayloadSHA256: jenkinsDigestA,
	}, goldenpath.RecordCompletedCIRunInput{
		ComponentID:    "cmp_auth",
		ExternalRunKey: "auth-service/main/browser-fixture",
		Status:         "succeeded",
		StartedAt:      &startedAt,
		CompletedAt:    time.Date(2026, 8, 30, 11, 55, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordCompletedJenkinsRun() error = %v", err)
	}

	deploymentStartedAt := time.Date(2026, 8, 30, 11, 56, 0, 0, time.UTC)
	deployment, err := service.RecordStagingDeployment(
		ctx,
		invocation(principal("usr_contributor"), "cor_browser_fixture_deployment"),
		goldenpath.RecordStagingDeploymentInput{
			EnvironmentID: "env_staging",
			CIRunID:       ciRun.CIRun.ID,
			Status:        "succeeded",
			StartedAt:     &deploymentStartedAt,
			CompletedAt:   time.Date(2026, 8, 30, 11, 59, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatalf("RecordStagingDeployment() error = %v", err)
	}
	messageResult, err := service.CreateMessage(
		ctx,
		invocation(principal("usr_contributor"), "cor_browser_fixture_message"),
		goldenpath.CreateMessageInput{
			ChannelID:         "chn_project",
			ClientOperationID: "browser-fixture:message-1",
			Body:              "Authenticated browser fixture message.",
		},
	)
	if err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}
	thread, err := service.StartThreadFromMessage(
		ctx,
		invocation(principal("usr_contributor"), "cor_browser_fixture_thread"),
		goldenpath.StartThreadFromMessageInput{
			ChannelID:  "chn_project",
			MessageID:  messageResult.Message.ID,
			Title:      "Authenticated collaboration boundary",
			Visibility: "restricted",
		},
	)
	if err != nil {
		t.Fatalf("StartThreadFromMessage() error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.thread_memberships (
			workspace_id, thread_id, user_id
		) VALUES ('wrk_main', $1, 'usr_decider')
	`, thread.ID); err != nil {
		t.Fatalf("seed browser fixture decider Thread membership: %v", err)
	}
	if _, err := store.RebuildActivityProjection(ctx); err != nil {
		t.Fatalf("RebuildActivityProjection() error = %v", err)
	}

	passwordHash, err := authn.NewArgon2idHasher().Hash(authenticatedWebBrowserPassword)
	if err != nil {
		t.Fatalf("hash browser fixture password: %v", err)
	}
	accountCreatedAt := time.Now().UTC().Add(-time.Minute)
	if _, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.local_accounts (
			user_id, login_name, password_hash, created_at, password_changed_at
		) VALUES
			('usr_contributor', 'http.contributor', $1, $2, $2),
			('usr_decider', 'http.decider', $1, $2, $2)
	`, passwordHash, accountCreatedAt); err != nil {
		t.Fatalf("seed browser fixture local account: %v", err)
	}
	return authenticatedWebFixture{deployment: deployment, thread: thread}
}

func waitForBrowserFixtureStop(t *testing.T, stopPath string) {
	t.Helper()
	deadline := time.NewTimer(30 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("browser fixture timed out waiting for stop file")
		case <-ticker.C:
			if _, err := os.Stat(stopPath); err == nil {
				return
			} else if !os.IsNotExist(err) {
				t.Fatalf("inspect fixture stop file: %v", err)
			}
		}
	}
}
