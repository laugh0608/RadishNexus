//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

func assertStagingDeploymentSlice(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *goldenpostgres.Store,
	service *goldenpath.Service,
	ciRun goldenpath.CIRun,
	sourceDelivery goldenpath.VerifiedJenkinsDelivery,
	sourceInput goldenpath.RecordCompletedCIRunInput,
) {
	t.Helper()
	seedDeploymentTargets(t, ctx, pool)

	startedAt := time.Date(2026, 8, 28, 11, 56, 0, 0, time.UTC)
	completedAt := time.Date(2026, 8, 28, 11, 59, 0, 0, time.UTC)
	input := goldenpath.RecordStagingDeploymentInput{
		EnvironmentID: "env_staging",
		CIRunID:       ciRun.ID,
		Status:        "succeeded",
		StartedAt:     &startedAt,
		CompletedAt:   completedAt,
	}

	_, err := service.RecordStagingDeployment(
		ctx,
		invocation(principal("usr_reader"), "cor_unauthorized_deployment"),
		input,
	)
	if !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("unauthorized RecordStagingDeployment() error = %v, want forbidden", err)
	}
	assertDeploymentCounts(t, ctx, pool, 0, 2, 5, 2)

	productionInput := input
	productionInput.EnvironmentID = "env_production"
	_, err = service.RecordStagingDeployment(
		ctx,
		invocation(principal("usr_contributor"), "cor_production_denied"),
		productionInput,
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("production RecordStagingDeployment() error = %v, want conflict", err)
	}
	assertDeploymentCounts(t, ctx, pool, 0, 2, 5, 2)

	deployment, err := service.RecordStagingDeployment(
		ctx,
		invocation(principal("usr_contributor"), "cor_staging_deployment"),
		input,
	)
	if err != nil {
		t.Fatalf("RecordStagingDeployment() error = %v", err)
	}
	if deployment.ID == "" || deployment.WorkspaceID != "wrk_main" ||
		deployment.EnvironmentID != "env_staging" || deployment.CIRunID != ciRun.ID ||
		deployment.Status != "succeeded" || deployment.RecordedBy != "usr_contributor" ||
		deployment.SourceKind != "api" || !deployment.CompletedAt.Equal(completedAt) {
		t.Fatalf("staging Deployment = %#v", deployment)
	}
	assertDeploymentCounts(t, ctx, pool, 1, 3, 6, 3)
	assertDeploymentAuditFields(t, ctx, pool, deployment.ID)

	_, err = service.RecordStagingDeployment(
		ctx,
		invocation(principal("usr_contributor"), "cor_duplicate_deployment"),
		input,
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("duplicate staging Deployment error = %v, want conflict", err)
	}
	assertDeploymentCounts(t, ctx, pool, 1, 3, 6, 3)

	otherCIRunID := loadOtherSucceededCIRunID(t, ctx, pool, ciRun.ID)
	existingEventID := loadDomainEventID(t, ctx, pool, "deployment.recorded", deployment.ID)
	atomicFailureService := goldenpath.NewService(store, &fixedIDs{values: []string{
		"dpl_atomic_failure", "lnk_atomic_deployment", existingEventID,
	}}, fixedClock{now: time.Date(2026, 8, 28, 12, 1, 0, 0, time.UTC)})
	atomicFailureInput := input
	atomicFailureInput.CIRunID = otherCIRunID
	_, err = atomicFailureService.RecordStagingDeployment(
		ctx,
		invocation(principal("usr_contributor"), "cor_atomic_deployment"),
		atomicFailureInput,
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("duplicate event staging Deployment error = %v, want conflict", err)
	}
	assertAbsent(t, ctx, pool, "radishnexus.deployments", "dpl_atomic_failure")
	assertAbsent(t, ctx, pool, "radishnexus.entity_links", "lnk_atomic_deployment")
	assertDeploymentCounts(t, ctx, pool, 1, 3, 6, 3)

	assertDeploymentDatabaseConstraints(t, ctx, pool, deployment, otherCIRunID)

	projected, err := store.RebuildActivityProjection(ctx)
	if err != nil {
		t.Fatalf("RebuildActivityProjection() with Deployment error = %v", err)
	}
	if projected != 6 {
		t.Fatalf("RebuildActivityProjection() projected = %d, want 6", projected)
	}
	assertTableCount(t, ctx, pool, "radishnexus.activity_items", 6)
	assertDeploymentActivityFacts(t, ctx, pool, deployment.ID, ciRun.ID)
	assertDeploymentNexusView(
		t,
		ctx,
		pool,
		service,
		deployment,
		input,
		sourceDelivery,
		sourceInput,
	)
}

func assertDeploymentNexusView(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	service *goldenpath.Service,
	deployment goldenpath.Deployment,
	input goldenpath.RecordStagingDeploymentInput,
	sourceDelivery goldenpath.VerifiedJenkinsDelivery,
	sourceInput goldenpath.RecordCompletedCIRunInput,
) {
	t.Helper()
	target := entityref.Ref{Type: "deployment", ID: deployment.ID}
	reader := principal("usr_reader")
	view, err := service.GetNexusView(ctx, reader, target)
	if err != nil {
		t.Fatalf("active Workspace member GetNexusView(Deployment) error = %v", err)
	}
	if view.Current.Ref != target || view.Current.Status != deployment.Status ||
		view.Current.Title != "" || view.Current.GoverningProjectID != "" ||
		!view.Current.UpdatedAt.Equal(deployment.RecordedAt) {
		t.Fatalf("Deployment Current identity and status = %#v", view.Current)
	}
	if view.Current.StartedAt == nil || !view.Current.StartedAt.Equal(*input.StartedAt) ||
		view.Current.CompletedAt == nil || !view.Current.CompletedAt.Equal(input.CompletedAt) ||
		view.Current.RecordedAt == nil || !view.Current.RecordedAt.Equal(deployment.RecordedAt) {
		t.Fatalf("Deployment Current times = %#v, Deployment = %#v", view.Current, deployment)
	}
	wantEnvironment := entityref.Ref{Type: "environment", ID: input.EnvironmentID}
	if view.Current.Environment == nil ||
		view.Current.Environment.State != goldenpath.ProjectionVisible ||
		view.Current.Environment.Ref != wantEnvironment ||
		view.Current.Environment.Title != "Staging" {
		t.Fatalf("Deployment Current Environment = %#v", view.Current.Environment)
	}
	wantCIRun := entityref.Ref{Type: "ci-run", ID: input.CIRunID}
	if view.Current.CIRun == nil ||
		view.Current.CIRun.State != goldenpath.ProjectionVisible ||
		view.Current.CIRun.Ref != wantCIRun || view.Current.CIRun.Title != "CI Run" {
		t.Fatalf("Deployment Current CI Run = %#v", view.Current.CIRun)
	}
	if len(view.Relations) != 1 ||
		view.Relations[0].State != goldenpath.ProjectionVisible ||
		view.Relations[0].RelationType != "deploys" ||
		view.Relations[0].Target != wantCIRun || view.Relations[0].Title != "CI Run" {
		t.Fatalf("Deployment Relations = %#v", view.Relations)
	}
	if len(view.Timeline) != 1 {
		t.Fatalf("Deployment Timeline length = %d, want 1: %#v", len(view.Timeline), view.Timeline)
	}
	item := view.Timeline[0]
	if item.ActivityType != "deployment.recorded" || item.Actor.Kind != "user" ||
		item.Actor.ID != deployment.RecordedBy || !item.OccurredAt.Equal(input.CompletedAt) ||
		len(item.SafeFacts) != 1 || item.SafeFacts["status"] != deployment.Status ||
		len(item.Subjects) != 2 ||
		item.Subjects[0].State != goldenpath.ProjectionVisible ||
		item.Subjects[0].Ref != wantEnvironment || item.Subjects[0].Title != "Staging" ||
		item.Subjects[1].State != goldenpath.ProjectionVisible ||
		item.Subjects[1].Ref != wantCIRun || item.Subjects[1].Title != "CI Run" {
		t.Fatalf("Deployment Timeline item = %#v", item)
	}

	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("encode Deployment Nexus View error = %v", err)
	}
	for _, forbidden := range []string{
		"dpa_staging_contributor",
		sourceDelivery.SourceID,
		sourceDelivery.DeliveryID,
		sourceDelivery.PayloadSHA256,
		sourceInput.ExternalRunKey,
		"receipt",
		"secret",
	} {
		if strings.Contains(strings.ToLower(string(encoded)), strings.ToLower(forbidden)) {
			t.Fatalf("Deployment Nexus View leaks forbidden value %q: %s", forbidden, encoded)
		}
	}
	assertDeploymentNexusViewHTTP(t, ctx, pool, service, deployment, sourceDelivery, sourceInput)

	if _, err := pool.Exec(ctx, `
		UPDATE radishnexus.environments
		SET status = 'archived', updated_at = clock_timestamp()
		WHERE workspace_id = 'wrk_main' AND id = 'env_staging'
	`); err != nil {
		t.Fatalf("archive Environment for read test error = %v", err)
	}
	if _, err := service.GetNexusView(ctx, reader, target); err != nil {
		t.Fatalf("archived Environment Deployment GetNexusView() error = %v", err)
	}

	nonMember := authz.Principal{
		Kind: authz.PrincipalUser, ID: "usr_not_a_member", WorkspaceID: "wrk_main",
	}
	if _, err := service.GetNexusView(ctx, nonMember, target); !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("non-member Deployment GetNexusView() error = %v, want not found", err)
	}
	crossWorkspace := authz.Principal{
		Kind: authz.PrincipalUser, ID: "usr_contributor", WorkspaceID: "wrk_other",
	}
	if _, err := service.GetNexusView(ctx, crossWorkspace, target); !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("cross-Workspace Deployment GetNexusView() error = %v, want not found", err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE radishnexus.workspace_memberships
		SET status = 'suspended'
		WHERE workspace_id = 'wrk_main' AND user_id = 'usr_reader'
	`); err != nil {
		t.Fatalf("suspend Workspace member for Deployment read error = %v", err)
	}
	defer func() {
		if _, restoreErr := pool.Exec(ctx, `
			UPDATE radishnexus.workspace_memberships
			SET status = 'active'
			WHERE workspace_id = 'wrk_main' AND user_id = 'usr_reader'
		`); restoreErr != nil {
			t.Errorf("restore Workspace member after Deployment read error = %v", restoreErr)
		}
	}()
	if _, err := service.GetNexusView(ctx, reader, target); !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("suspended member Deployment GetNexusView() error = %v, want not found", err)
	}
}

func assertDeploymentNexusViewHTTP(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	service *goldenpath.Service,
	deployment goldenpath.Deployment,
	sourceDelivery goldenpath.VerifiedJenkinsDelivery,
	sourceInput goldenpath.RecordCompletedCIRunInput,
) {
	t.Helper()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	passwordHash, err := authn.NewArgon2idHasher().Hash("integration reader password")
	if err != nil {
		t.Fatalf("hash HTTP integration password: %v", err)
	}
	sessionToken := deploymentHTTPToken(7)
	csrfToken := deploymentHTTPToken(8)
	tokenDigest := sha256.Sum256([]byte(sessionToken))
	csrfDigest := sha256.Sum256([]byte(csrfToken))
	if _, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.local_accounts (
			user_id, login_name, password_hash, created_at, password_changed_at
		) VALUES ('usr_reader', 'http.reader', $1, $2, $2)
	`, passwordHash, now); err != nil {
		t.Fatalf("seed HTTP integration local account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.user_sessions (
			id, user_id, token_digest, csrf_token_digest, created_at, expires_at
		) VALUES ('ses_deployment_http', 'usr_reader', $1, $2, $3, $4)
	`, tokenDigest[:], csrfDigest[:], now, now.Add(authn.SessionLifetime)); err != nil {
		t.Fatalf("seed HTTP integration Session: %v", err)
	}
	authService := authn.NewService(authpostgres.New(pool), nil, nil, fixedClock{now: now})
	sessionPolicy, err := httptransport.NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatalf("NewBrowserSessionPolicy() error = %v", err)
	}
	proxyPolicy, err := httptransport.NewTrustedProxyPolicy("10.0.0.0/8")
	if err != nil {
		t.Fatalf("NewTrustedProxyPolicy() error = %v", err)
	}
	handler := httptransport.WithRequestID(httptransport.NewDeploymentNexusViewHandler(
		authService,
		service,
		sessionPolicy,
		proxyPolicy,
	))

	request := deploymentHTTPRequest("wrk_main", deployment.ID, sessionToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("HTTP Deployment Nexus View = status %d, headers %#v, body %q", response.Code, response.Header(), response.Body.String())
	}
	encoded := response.Body.String()
	for _, required := range []string{
		deployment.ID,
		deployment.EnvironmentID,
		deployment.CIRunID,
		`"status":"succeeded"`,
		`"relation_type":"deploys"`,
		`"activity_type":"deployment.recorded"`,
	} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("HTTP Deployment Nexus View missing %q: %s", required, encoded)
		}
	}
	for _, forbidden := range []string{
		"dpa_staging_contributor",
		sourceDelivery.SourceID,
		sourceDelivery.DeliveryID,
		sourceDelivery.PayloadSHA256,
		sourceInput.ExternalRunKey,
		"authorization",
		"projection_version",
		"safe_facts",
		"secret",
	} {
		if strings.Contains(strings.ToLower(encoded), strings.ToLower(forbidden)) {
			t.Fatalf("HTTP Deployment Nexus View leaks forbidden value %q: %s", forbidden, encoded)
		}
	}

	for _, test := range []struct {
		workspaceID  string
		deploymentID string
	}{
		{workspaceID: "wrk_other", deploymentID: deployment.ID},
		{workspaceID: "wrk_main", deploymentID: "dpl_unknown"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, deploymentHTTPRequest(test.workspaceID, test.deploymentID, sessionToken))
		if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
			t.Fatalf("HTTP unreadable Deployment = status %d, body %q", response.Code, response.Body.String())
		}
	}
}

func deploymentHTTPRequest(workspaceID string, deploymentID string, token string) *http.Request {
	request := httptest.NewRequest(
		http.MethodGet,
		"https://nexus.example.test/api/v1/workspaces/"+workspaceID+"/deployments/"+deploymentID+"/nexus-view",
		nil,
	)
	request.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: token})
	return request
}

func deploymentHTTPToken(value byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{value}, 32))
}

func seedDeploymentTargets(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.environments (
			id, workspace_id, key, name, classification, owner_team_id,
			status, created_by_kind, created_by_id
		) VALUES
			('env_staging', 'wrk_main', 'STAGING', 'Staging', 'staging',
			 'tem_main', 'active', 'user', 'usr_admin'),
			('env_production', 'wrk_main', 'PRODUCTION', 'Production', 'production',
			 'tem_main', 'active', 'user', 'usr_admin');
		INSERT INTO radishnexus.environment_deployment_authorizations (
			id, workspace_id, environment_id, user_id, granted_by
		) VALUES
			('dpa_staging_contributor', 'wrk_main', 'env_staging', 'usr_contributor', 'usr_admin'),
			('dpa_production_contributor', 'wrk_main', 'env_production', 'usr_contributor', 'usr_admin');
	`)
	if err != nil {
		t.Fatalf("seed Deployment targets error = %v", err)
	}
}

func assertDeploymentCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	deployments int,
	links int,
	events int,
	outbox int,
) {
	t.Helper()
	for table, want := range map[string]int{
		"radishnexus.deployments":       deployments,
		"radishnexus.entity_links":      links,
		"radishnexus.domain_events":     events,
		"radishnexus.outbox_deliveries": outbox,
	} {
		assertTableCount(t, ctx, pool, table, want)
	}
}

func assertDeploymentAuditFields(t *testing.T, ctx context.Context, pool *pgxpool.Pool, deploymentID string) {
	t.Helper()
	var authorizationID string
	var recordedBy string
	var sourceKind string
	var sourceID *string
	if err := pool.QueryRow(ctx, `
		SELECT authorization_id, recorded_by, source_kind, source_id
		FROM radishnexus.deployments
		WHERE id = $1
	`, deploymentID).Scan(&authorizationID, &recordedBy, &sourceKind, &sourceID); err != nil {
		t.Fatalf("load Deployment audit fields error = %v", err)
	}
	if authorizationID != "dpa_staging_contributor" || recordedBy != "usr_contributor" ||
		sourceKind != "api" || sourceID != nil {
		t.Fatalf("Deployment audit fields = authorization %q, actor %q, source %q/%v",
			authorizationID, recordedBy, sourceKind, sourceID)
	}
}

func loadOtherSucceededCIRunID(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	excludedID string,
) string {
	t.Helper()
	var ciRunID string
	if err := pool.QueryRow(ctx, `
		SELECT id
		FROM radishnexus.ci_runs
		WHERE workspace_id = 'wrk_main' AND status = 'succeeded' AND id <> $1
		ORDER BY id
		LIMIT 1
	`, excludedID).Scan(&ciRunID); err != nil {
		t.Fatalf("load another succeeded CI Run error = %v", err)
	}
	return ciRunID
}

func assertDeploymentDatabaseConstraints(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	deployment goldenpath.Deployment,
	otherCIRunID string,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		UPDATE radishnexus.deployments SET status = 'failed' WHERE id = $1
	`, deployment.ID)
	assertPGCode(t, err, "23514", "immutable Deployment fact")

	_, err = pool.Exec(ctx, `
		UPDATE radishnexus.environments
		SET classification = 'production'
		WHERE id = 'env_staging'
	`)
	assertPGCode(t, err, "23514", "immutable Environment classification")

	_, err = pool.Exec(ctx, `
		INSERT INTO radishnexus.deployments (
			id, workspace_id, environment_id, ci_run_id, authorization_id,
			status, completed_at, recorded_by, source_kind, recorded_at
		) VALUES (
			'dpl_production_rejected', 'wrk_main', 'env_production', $1,
			'dpa_production_contributor', 'succeeded', clock_timestamp(),
			'usr_contributor', 'api', clock_timestamp()
		)
	`, otherCIRunID)
	assertPGCode(t, err, "23514", "database staging-only Deployment constraint")
}

func assertDeploymentActivityFacts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	deploymentID string,
	ciRunID string,
) {
	t.Helper()
	var subjects []byte
	var safeFacts []byte
	var actorKind string
	var actorID string
	if err := pool.QueryRow(ctx, `
		SELECT subject_refs, safe_facts, actor_kind, actor_id
		FROM radishnexus.activity_items
		WHERE target_type = 'deployment' AND target_id = $1
	`, deploymentID).Scan(&subjects, &safeFacts, &actorKind, &actorID); err != nil {
		t.Fatalf("load Deployment Activity facts error = %v", err)
	}
	var decodedSubjects []map[string]string
	if err := json.Unmarshal(subjects, &decodedSubjects); err != nil {
		t.Fatalf("decode Deployment Activity subjects error = %v", err)
	}
	var decodedFacts map[string]string
	if err := json.Unmarshal(safeFacts, &decodedFacts); err != nil {
		t.Fatalf("decode Deployment Activity safe facts error = %v", err)
	}
	if len(decodedSubjects) != 2 || decodedSubjects[0]["type"] != "environment" ||
		decodedSubjects[0]["id"] != "env_staging" || decodedSubjects[1]["type"] != "ci-run" ||
		decodedSubjects[1]["id"] != ciRunID || len(decodedFacts) != 1 ||
		decodedFacts["status"] != "succeeded" || actorKind != "user" || actorID != "usr_contributor" {
		t.Fatalf("Deployment Activity subjects = %#v, facts = %#v, actor = %s/%s",
			decodedSubjects, decodedFacts, actorKind, actorID)
	}
}
