//go:build integration && browserfixture

package postgres_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
	"github.com/laugh0608/RadishNexus/server/internal/platform/realtime"
	"github.com/laugh0608/RadishNexus/server/internal/platform/runtimeconfig"
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

	firstVisit := os.Getenv("RADISHNEXUS_BROWSER_SETUP") == "1"
	foundation := os.Getenv("RADISHNEXUS_BROWSER_FOUNDATION") == "1"
	var fixture authenticatedWebFixture
	if !foundation && !firstVisit {
		fixture = seedAuthenticatedWebBrowserData(t, ctx, pool)
	}
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
	ownerWorkspace := ""
	if foundation && !firstVisit {
		first, err := authService.Bootstrap(ctx, authn.BootstrapInput{Email: "foundation.owner@example.test", DisplayName: "Foundation Owner", WorkspaceName: "Foundation Workspace", Password: authenticatedWebBrowserPassword})
		if err != nil {
			t.Fatal(err)
		}
		ownerWorkspace = first.WorkspaceID
	}
	setupCode, err := runtimeconfig.SetupCode(os.Getenv, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	setupService, err := authn.NewSetupService(authService, authpostgres.New(pool), setupCode)
	if err != nil {
		t.Fatal(err)
	}
	readiness, err := db.NewReadinessChecker(pool)
	if err != nil {
		t.Fatal(err)
	}
	authenticationGuard := httptransport.NewLoginGuard(5, time.Minute, 64, 2)
	identityHandler := httptransport.NewIdentityHandler(authn.NewIdentityService(authpostgres.New(pool), authService, ""), authService, nil, sessionPolicy, proxyPolicy, authenticationGuard)
	authHandler := httptransport.NewAuthHandler(
		authService,
		sessionPolicy,
		proxyPolicy,
		authenticationGuard,
		identityHandler,
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
	mux.Handle("/api/v1/setup", httptransport.NewSetupHandler(setupService, readiness, sessionPolicy, proxyPolicy, authenticationGuard))
	mux.Handle("/api/v1/auth", authHandler)
	mux.Handle("/api/v1/auth/", authHandler)
	discoveryHandler := httptransport.NewDiscoveryHandler(authService, goldenpath.NewDiscoveryService(goldenpostgres.New(pool)), sessionPolicy, proxyPolicy)
	configurationHandler := httptransport.NewConfigurationHandler(authService, goldenpath.NewConfigurationService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{}), sessionPolicy, proxyPolicy)
	httptransport.RegisterConfigurationRoutes(mux, discoveryHandler, configurationHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/projects/", discoveryHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/invitations", identityHandler)
	mux.Handle("/auth/complete", identityHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages", channelHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages/", channelHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/events", channelEventsHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/threads/", collaborationHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/decisions/", collaborationHandler)
	mux.Handle("/api/v1/workspaces/{workspace_id}/tickets/", collaborationHandler)
	httptransport.RegisterTicketStatusRoutes(mux, httptransport.NewTicketStatusHandler(authService, goldenpath.NewTicketStatusService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{}), sessionPolicy, proxyPolicy))
	httptransport.RegisterTicketComponentRoutes(mux, httptransport.NewTicketComponentHandler(authService, goldenpath.NewTicketComponentService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{}), sessionPolicy, proxyPolicy))
	httptransport.RegisterDocumentRoutes(mux, httptransport.NewDocumentHandler(authService, goldenpath.NewDocumentService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{}), sessionPolicy, proxyPolicy))
	httptransport.RegisterStagingDeploymentRoutes(mux, httptransport.NewStagingDeploymentHandler(authService, viewService, sessionPolicy, proxyPolicy))
	httptransport.RegisterRepositoryCIRunRoutes(mux, httptransport.NewRepositoryCIRunsHandler(authService, viewService, sessionPolicy, proxyPolicy))
	httptransport.RegisterCIRunRoutes(mux, httptransport.NewCIRunNexusViewHandler(authService, viewService, sessionPolicy, proxyPolicy))
	mux.Handle("/api/v1/workspaces", deploymentHandler)
	mux.Handle("/api/v1/workspaces/", deploymentHandler)
	mux.Handle("/", webHandler)
	var app http.Handler = mux
	if os.Getenv("RADISHNEXUS_BROWSER_CHECKOUT") == "1" {
		// Test-only local configuration handoff lets the browser create its own
		// Repository/Component first. Requests still traverse the real HMAC handler.
		configPath := statePath + ".jenkins.json"
		t.Cleanup(func() { _ = os.Remove(configPath) })
		app = httptransport.WithJenkinsDeliveries(app, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, e := jenkins.ReadFile(configPath, jenkins.MaxConfig)
			if e != nil {
				http.Error(w, "fixture source unavailable", 503)
				return
			}
			sources, e := jenkins.LoadSources(body, func(string, int64) ([]byte, error) { return []byte(strings.Repeat("ab", 32)), nil })
			if e != nil {
				http.Error(w, "fixture source invalid", 503)
				return
			}
			for _, source := range sources {
				if e = goldenpostgres.New(pool).ValidateJenkinsSource(r.Context(), source.WorkspaceID, source.ComponentID, source.ID, source.Job, source.CheckoutBinding); e != nil {
					http.Error(w, "fixture binding invalid", 503)
					return
				}
			}
			httptransport.NewJenkinsDeliveryHandler(sources, viewService, sessionPolicy, proxyPolicy, time.Now, func(httptransport.JenkinsDeliveryLog) {}).ServeHTTP(w, r)
		}))
	}
	server := httptest.NewUnstartedServer(httptransport.WithRequestID(app))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	state, err := json.Marshal(map[string]string{
		"public_origin":        publicOrigin,
		"foundation_workspace": ownerWorkspace,
		"foundation_login":     "foundation.owner@example.test",
		"channel_path":         "/workspaces/wrk_main/channels/chn_project",
		"thread_path":          "/workspaces/wrk_main/threads/" + fixture.thread.ID,
		"restricted_thread":    fixture.thread.ID,
		"database_container":   os.Getenv("RADISHNEXUS_BROWSER_FIXTURE_DATABASE_CONTAINER"),
		"ci_run_path":          "/workspaces/wrk_main/ci-runs/" + fixture.ciRunID,
		"deployment_path":      "/workspaces/wrk_main/deployments/" + fixture.deployment.ID,
		"contributor_login":    "http.contributor@example.test",
		"decider_login":        "http.decider@example.test",
		"password":             authenticatedWebBrowserPassword,
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
	ciRunID    string
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
	accountCreatedAt := time.Now().UTC().Add(-time.Minute)
	if _, err := pool.Exec(ctx, `INSERT INTO radishnexus.user_accounts (user_id, status, created_at) VALUES ('usr_contributor', 'active', $1), ('usr_decider', 'active', $1)`, accountCreatedAt); err != nil {
		t.Fatalf("seed identity accounts: %v", err)
	}
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

	if os.Getenv("RADISHNEXUS_BROWSER_CHECKOUT") == "1" {
		if _, e := pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET role='owner' WHERE workspace_id='wrk_main' AND user_id='usr_contributor'`); e != nil {
			t.Fatal(e)
		}
		cfg := goldenpath.NewConfigurationService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
		repo, e := cfg.Configure(ctx, tcInvocation("usr_contributor"), goldenpath.ConfigurationInput{Kind: "repository.create", ScopeID: "wrk_main", ClientOperationID: "browser-checkout", Name: "Synthetic checkout source", Repository: &goldenpath.RepositoryConfigurationInput{RepositoryMetadata: goldenpath.RepositoryMetadata{Provider: "gitea", ProviderOrigin: "https://git.example.test", ExternalID: "browser-123", WebURL: "https://git.example.test/team/source", DefaultBranch: "main"}}})
		if e != nil {
			t.Fatal(e)
		}
		source := jenkins.Source{ID: "browser_checkout", WorkspaceID: "wrk_main", ComponentID: "cmp_auth", Job: "synthetic/build", CheckoutBinding: &goldenpath.CICheckoutBinding{BindingID: "browser_binding", RepositoryID: repo.Object.ID, Provider: "gitea", ProviderOrigin: "https://git.example.test", ExternalID: "browser-123", Profile: goldenpath.JenkinsCheckoutProfile, EffectiveFromBuild: 1}}
		p := jenkins.Payload{Version: 2, Job: source.Job, Number: 1, Result: "SUCCESS", CompletedAt: time.Now().UTC().Truncate(time.Millisecond), Checkout: &goldenpath.CICheckout{BindingID: "browser_binding", State: "observed", Revision: &goldenpath.GitRevision{Algorithm: "sha1", OID: strings.Repeat("a", 40)}}}
		live := goldenpath.NewService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
		r := recordCheckoutHTTP(t, ctx, live, source, p)
		ciRun.CIRun.ID = r.CIRunID
		p.Number = 2
		p.Checkout.State = "unavailable"
		p.Checkout.Revision = nil
		reason := "checkout_not_observed"
		p.Checkout.Reason = &reason
		recordCheckoutHTTP(t, ctx, live, source, p)
	}

	var deployment goldenpath.Deployment
	if os.Getenv("RADISHNEXUS_BROWSER_STAGING") != "1" {
		deploymentStartedAt := time.Date(2026, 8, 30, 11, 56, 0, 0, time.UTC)
		deployment, err = service.RecordStagingDeployment(
			ctx,
			invocation(principal("usr_contributor"), "cor_browser_fixture_deployment"),
			goldenpath.RecordStagingDeploymentInput{
				ClientOperationID: "staging-test", Confirmed: true,
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

	passwordHash, err := authn.NewArgon2idHasher().Hash(authenticatedWebBrowserPassword)
	if err != nil {
		t.Fatalf("hash browser fixture password: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.local_credentials (
			user_id, email, password_hash, created_at, password_changed_at
		) VALUES
			('usr_contributor', 'http.contributor@example.test', $1, $2, $2),
			('usr_decider', 'http.decider@example.test', $1, $2, $2)
	`, passwordHash, accountCreatedAt); err != nil {
		t.Fatalf("seed browser fixture local account: %v", err)
	}
	return authenticatedWebFixture{deployment: deployment, thread: thread, ciRunID: ciRun.CIRun.ID}
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
