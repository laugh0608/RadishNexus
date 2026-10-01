//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

// Called from the real bootstrap + invitation fixture. Every object and grant
// under test is created by its formal command, never by fixture SQL.
func assertDeliveryConfigurationFromBootstrap(t *testing.T, ctx context.Context, pool *pgxpool.Pool, owner, member authz.Principal, team string) {
	t.Helper()
	store := goldenpostgres.New(pool)
	s := goldenpath.NewConfigurationService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	core := goldenpath.NewService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	call := func(p authz.Principal, in goldenpath.ConfigurationInput) (goldenpath.ConfigurationResult, error) {
		return s.Configure(ctx, goldenpath.Invocation{Principal: p, SourceKind: "web", CorrelationID: "delivery-config"}, in)
	}
	must := func(in goldenpath.ConfigurationInput) goldenpath.ConfigurationResult {
		t.Helper()
		r, e := call(owner, in)
		if e != nil {
			t.Fatal(in.Kind, e)
		}
		return r
	}
	componentInput := goldenpath.ConfigurationInput{Kind: "component.create", ScopeID: owner.WorkspaceID, ClientOperationID: "component", Name: "Service", Key: "service", OwnerTeamID: team, Delivery: &goldenpath.DeliveryConfigurationInput{Type: "service"}}
	if _, e := call(member, componentInput); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("member created Component", e)
	}
	component := must(componentInput).Object
	if retry := must(componentInput); retry.Created || retry.Object.ID != component.ID {
		t.Fatal("creation retry", retry)
	}
	conflict := componentInput
	conflict.ClientOperationID = "other-create"
	if _, e := call(owner, conflict); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("duplicate key", e)
	}
	env := must(goldenpath.ConfigurationInput{Kind: "environment.create", ScopeID: owner.WorkspaceID, ClientOperationID: "environment", Name: "Stage", Key: "stage", OwnerTeamID: team, Delivery: &goldenpath.DeliveryConfigurationInput{Classification: "staging"}}).Object
	for _, kind := range []string{"components", "environments"} {
		page, e := s.ListConfiguration(ctx, member, goldenpath.ConfigurationQuery{Kind: kind, ScopeID: owner.WorkspaceID, Limit: 25})
		if e != nil || len(page.Objects) != 1 {
			t.Fatal("ordinary discovery", kind, page, e)
		}
	}
	var count int
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.environment_deployment_authorizations WHERE environment_id=$1`, env.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("implicit authorization", e, count)
	}
	latest := func() *goldenpath.ConfigurationAuthorization {
		t.Helper()
		page, e := s.ListConfiguration(ctx, owner, goldenpath.ConfigurationQuery{Kind: "environment-authorization", ScopeID: env.ID, UserID: member.ID, Limit: 1})
		if e != nil || len(page.Members) != 1 {
			t.Fatal(e)
		}
		return page.Members[0].Authorization
	}
	if latest() != nil {
		t.Fatal("new member has authorization")
	}
	if _, e := s.ListConfiguration(ctx, member, goldenpath.ConfigurationQuery{Kind: "environment-authorizations", ScopeID: env.ID, Limit: 25}); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("member read grant list", e)
	}
	grant := goldenpath.ConfigurationInput{Kind: "environment.authorization.grant", ScopeID: env.ID, UserID: member.ID, ClientOperationID: "grant", Delivery: &goldenpath.DeliveryConfigurationInput{Confirmed: true}}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, e := call(owner, grant); results <- e })
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal("concurrent exact grant", e)
		}
	}
	a1 := latest()
	if a1 == nil || a1.Status != "active" {
		t.Fatal(a1)
	}
	stale := grant
	stale.ClientOperationID = "stale-null"
	if _, e := call(owner, stale); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("stale expected null", e)
	}
	buildNumber := 0
	build := func() string {
		t.Helper()
		buildNumber++
		key := fmt.Sprintf("configured-%d", buildNumber)
		r, e := core.RecordCompletedJenkinsRun(ctx, goldenpath.VerifiedJenkinsDelivery{WorkspaceID: owner.WorkspaceID, SourceID: "configured-source", DeliveryID: key, PayloadSHA256: strings.Repeat("a", 64)}, goldenpath.RecordCompletedCIRunInput{ComponentID: component.ID, ExternalRunKey: key, Status: "succeeded", CompletedAt: time.Now().UTC().Truncate(time.Millisecond).Add(-time.Minute)})
		if e != nil {
			t.Fatal(e)
		}
		return r.CIRun.ID
	}
	deploymentInput := goldenpath.RecordStagingDeploymentInput{EnvironmentID: env.ID, CIRunID: build(), Status: "succeeded", CompletedAt: time.Now().UTC().Truncate(time.Millisecond), ClientOperationID: "record", Confirmed: true}
	if _, e := core.RecordStagingDeployment(ctx, invocation(owner, "owner-no-grant"), deploymentInput); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("owner bypassed explicit grant", e)
	}
	d1, e := core.RecordStagingDeployment(ctx, invocation(member, "configured-deployment"), deploymentInput)
	if e != nil {
		t.Fatal(e)
	}
	revoke := goldenpath.ConfigurationInput{Kind: "environment.authorization.revoke", ScopeID: env.ID, UserID: member.ID, ClientOperationID: "revoke", Delivery: &goldenpath.DeliveryConfigurationInput{Confirmed: true, ExpectedAuthorization: a1}}
	must(revoke)
	if _, e = core.RecordStagingDeployment(ctx, invocation(member, "revoked-retry"), deploymentInput); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("revoked Deployment retry", e)
	}
	must(grant) // Acknowledges original command; does not restore authority.
	revoked := latest()
	if revoked.Status != "revoked" {
		t.Fatal("old grant revived authorization")
	}
	grant2 := grant
	grant2.ClientOperationID = "regrant"
	grant2.Delivery = &goldenpath.DeliveryConfigurationInput{Confirmed: true, ExpectedAuthorization: revoked}
	must(grant2)
	a2 := latest()
	if a2.ID == a1.ID || a2.Status != "active" {
		t.Fatal("authorization history rewritten", a1, a2)
	}
	must(revoke) // Old receipt must not revoke the new generation.
	if latest().ID != a2.ID || latest().Status != "active" {
		t.Fatal("old revoke damaged new grant")
	}
	revoke.ClientOperationID = "stale-revoke"
	if _, e = call(owner, revoke); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("stale revoke accepted", e)
	}
	deploymentInput.CIRunID = build()
	deploymentInput.ClientOperationID = "record2"
	d2, e := core.RecordStagingDeployment(ctx, invocation(member, "new-generation"), deploymentInput)
	if e != nil {
		t.Fatal(e)
	}
	for id, want := range map[string]string{d1.ID: a1.ID, d2.ID: a2.ID} {
		var got string
		if e = pool.QueryRow(ctx, `SELECT authorization_id FROM radishnexus.deployments WHERE id=$1`, id).Scan(&got); e != nil || got != want {
			t.Fatal("historical grant binding", got, want, e)
		}
	}
	for _, sql := range []string{`UPDATE radishnexus.environment_deployment_authorizations SET status='active',revoked_by=NULL,revoked_at=NULL WHERE id=$1`, `UPDATE radishnexus.environment_deployment_authorizations SET generation=99 WHERE id=$1`, `DELETE FROM radishnexus.environment_deployment_authorizations WHERE id=$1`} {
		if _, e = pool.Exec(ctx, sql, a1.ID); e == nil {
			t.Fatal("mutable authorization history")
		}
	}
	// Inject a failure after changing the grant; receipt, Audit and state roll back.
	if _, e = pool.Exec(ctx, `ALTER TABLE radishnexus.workspace_configuration_audit ADD CONSTRAINT test_delivery_audit_failure CHECK(request_id <> 'delivery-failure')`); e != nil {
		t.Fatal(e)
	}
	revoke.ClientOperationID = "failed-revoke"
	revoke.Delivery = &goldenpath.DeliveryConfigurationInput{Confirmed: true, ExpectedAuthorization: a2}
	if _, e = s.Configure(ctx, goldenpath.Invocation{Principal: owner, SourceKind: "web", CorrelationID: "delivery-failure"}, revoke); e == nil {
		t.Fatal("Audit failure swallowed")
	}
	if latest().Status != "active" {
		t.Fatal("failed Audit left revoked grant")
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.workspace_configuration_receipts WHERE client_operation_id='failed-revoke'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("failed receipt persisted", e)
	}
	// Archive/disable are controlled test setup, not newly exposed product commands.
	if _, e = pool.Exec(ctx, `UPDATE radishnexus.environments SET status='archived' WHERE id=$1`, env.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, member.ID); e != nil {
		t.Fatal(e)
	}
	must(revoke)
	if latest().Status != "revoked" {
		t.Fatal("could not clean archived/disabled grant")
	}
	if _, e = call(owner, grant2); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("archived grant retry accepted", e)
	}
	if _, e = pool.Exec(ctx, `UPDATE radishnexus.user_accounts SET status='active' WHERE user_id=$1`, member.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = core.GetNexusView(ctx, member, entityref.Ref{Type: "deployment", ID: d1.ID}); e != nil {
		t.Fatal("revocation hid history", e)
	}
	var before, after string
	const snapshot = `SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY event_id),'[]'::jsonb)::text FROM radishnexus.activity_items a`
	if e = pool.QueryRow(ctx, snapshot).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = store.RebuildActivityProjection(ctx); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, snapshot).Scan(&after); e != nil || before != after {
		t.Fatal("creation projection rebuild drift", e)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.activity_items WHERE activity_type LIKE 'environment.authorization.%'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("grant leaked to Activity", e)
	}
	assertConcurrentOwnerAuthorizations(t, ctx, pool, owner, member, team)
}

func assertConcurrentOwnerAuthorizations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, owner, member authz.Principal, team string) {
	t.Helper()
	s := goldenpath.NewConfigurationService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	call := func(p authz.Principal, in goldenpath.ConfigurationInput) (goldenpath.ConfigurationResult, error) {
		return s.Configure(ctx, goldenpath.Invocation{Principal: p, SourceKind: "web", CorrelationID: "mutual-owners"}, in)
	}
	r, err := call(owner, goldenpath.ConfigurationInput{Kind: "environment.create", ScopeID: owner.WorkspaceID, ClientOperationID: "mutual-env", Name: "Mutual", Key: "mutual", OwnerTeamID: team, Delivery: &goldenpath.DeliveryConfigurationInput{Classification: "staging"}})
	if err != nil {
		t.Fatal(err)
	}
	// A second owner is controlled test setup; this slice has no role promotion UI.
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, owner.WorkspaceID, member.ID); err != nil {
		t.Fatal(err)
	}
	input := func(user, op string) goldenpath.ConfigurationInput {
		return goldenpath.ConfigurationInput{Kind: "environment.authorization.grant", ScopeID: r.Object.ID, UserID: user, ClientOperationID: op, Delivery: &goldenpath.DeliveryConfigurationInput{Confirmed: true}}
	}
	start, done := make(chan struct{}), make(chan error, 2)
	for _, pair := range [][2]authz.Principal{{owner, member}, {member, owner}} {
		go func() {
			<-start
			_, e := call(pair[0], input(pair[1].ID, "mutual-grant"))
			done <- e
		}()
	}
	close(start)
	for range 2 {
		if err = <-done; err != nil {
			t.Fatal("mutual owner grant", err)
		}
	}
	page, err := s.ListConfiguration(ctx, owner, goldenpath.ConfigurationQuery{Kind: "environment-authorization", ScopeID: r.Object.ID, UserID: owner.ID, Limit: 1})
	if err != nil || len(page.Members) != 1 || page.Members[0].Authorization == nil {
		t.Fatal("owner authorization", page, err)
	}
	self := input(owner.ID, "self-noop")
	self.Delivery.ExpectedAuthorization = page.Members[0].Authorization
	if _, err = call(owner, self); err != nil {
		t.Fatal("explicit self grant", err)
	}
	var unchanged bool
	if err = pool.QueryRow(ctx, `SELECT NOT changed AND authorization_id=$1 FROM radishnexus.workspace_configuration_audit WHERE scope_id=$2 AND actor_id=$3 AND subject_id=$3`, self.Delivery.ExpectedAuthorization.ID, r.Object.ID, owner.ID).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("no-op rewrote provenance", unchanged, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET role='member' WHERE workspace_id=$1 AND user_id=$2`, owner.WorkspaceID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = call(member, input(owner.ID, "mutual-grant")); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("demoted owner replayed receipt", err)
	}
}
