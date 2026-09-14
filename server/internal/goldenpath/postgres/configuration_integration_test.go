//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestFoundationConfigurationFromBootstrap(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	database := fmt.Sprintf("nexus_foundation_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{database}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{database}.Sanitize()); e != nil {
			t.Error(e)
		}
	}()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = database
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Migrate(ctx, conn.Conn())
	conn.Release()
	if err != nil {
		t.Fatal(err)
	}
	authStore := authpostgres.New(pool)
	auth := authn.NewService(authStore, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	bootstrap, err := auth.Bootstrap(ctx, authn.BootstrapInput{Email: "foundation-owner@example.test", DisplayName: "Owner", WorkspaceName: "Foundation", Password: "test only foundation password"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.Login(ctx, authn.LoginInput{Email: "foundation-owner@example.test", Password: "test only foundation password"})
	if err != nil {
		t.Fatal(err)
	}
	identity := authn.NewIdentityService(authStore, auth, "")
	invitation, err := identity.CreateInvitation(ctx, session.Token, bootstrap.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	memberSession, err := identity.AcceptInvitation(ctx, authn.AcceptInvitationInput{InvitationToken: invitation.Token, Email: "foundation-member@example.test", DisplayName: "Member", Password: "test only member password"})
	if err != nil {
		t.Fatal(err)
	}
	owner := authz.Principal{Kind: authz.PrincipalUser, ID: bootstrap.UserID, WorkspaceID: bootstrap.WorkspaceID}
	member := owner
	member.ID = memberSession.Account.User.ID
	store := goldenpostgres.New(pool)
	service := goldenpath.NewConfigurationService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	call := func(p authz.Principal, in goldenpath.ConfigurationInput) (goldenpath.ConfigurationResult, error) {
		return service.Configure(ctx, goldenpath.Invocation{Principal: p, SourceKind: "web", CorrelationID: "req_foundation_test"}, in)
	}
	must := func(in goldenpath.ConfigurationInput) goldenpath.ConfigurationResult {
		t.Helper()
		r, e := call(owner, in)
		if e != nil {
			t.Fatalf("%s: %v", in.Kind, e)
		}
		return r
	}
	team := must(goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: owner.WorkspaceID, ClientOperationID: "team", Name: "Team"})
	projectInput := goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: owner.WorkspaceID, ClientOperationID: "project", Name: "Project", Key: "foundation", OwnerTeamID: team.Object.ID, InitialAdminUserID: owner.ID, Visibility: "restricted"}
	var wg sync.WaitGroup
	outcomes := make(chan goldenpath.ConfigurationResult, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Go(func() { r, e := call(owner, projectInput); outcomes <- r; failures <- e })
	}
	wg.Wait()
	close(outcomes)
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	var project goldenpath.ConfigurationResult
	created := 0
	for r := range outcomes {
		if project.Object.ID != "" && project.Object.ID != r.Object.ID {
			t.Fatal("duplicate projects")
		}
		project = r
		if r.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatal("expected exactly one creation", created)
	}
	changed := projectInput
	changed.Name = "Changed"
	if _, e := call(owner, changed); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("changed replay", e)
	}
	if _, e := service.ReadConfiguration(ctx, member, "project", project.Object.ID); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("uninvited project visible", e)
	}
	setRole := goldenpath.ConfigurationInput{Kind: "project.member.set", ScopeID: project.Object.ID, UserID: member.ID, ClientOperationID: "grant", Role: "contributor"}
	must(setRole)
	if _, e := call(member, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: project.Object.ID, ClientOperationID: "unauthorized", Name: "No", Visibility: "project", MemberUserIDs: []string{}}); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("contributor administered project", e)
	}
	channel := must(goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: project.Object.ID, ClientOperationID: "channel", Name: "Private", Visibility: "restricted", MemberUserIDs: []string{owner.ID, member.ID}})
	var initialGrants bool
	if err = pool.QueryRow(ctx, `SELECT granted_user_ids @> ARRAY[$1,$2]::text[] AND cardinality(granted_user_ids)=2 FROM radishnexus.workspace_configuration_audit WHERE command_kind='channel.create' AND result_id=$3`, owner.ID, member.ID, channel.Object.ID).Scan(&initialGrants); err != nil || !initialGrants {
		t.Fatal("initial channel grants missing from audit", err)
	}
	messaging := goldenpath.NewService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	messageInput := goldenpath.CreateMessageInput{ChannelID: channel.Object.ID, ClientOperationID: "message", Body: "First member message"}
	message, err := messaging.CreateMessage(ctx, invocation(member, "cor_foundation_message"), messageInput)
	if err != nil {
		t.Fatal(err)
	}
	if message.Message.ID == "" {
		t.Fatal("no message")
	}
	thread, err := messaging.StartThreadFromMessage(ctx, invocation(member, "cor_foundation_thread"), goldenpath.StartThreadFromMessageInput{ChannelID: channel.Object.ID, MessageID: message.Message.ID, Title: "Private thread", Visibility: "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	role := "contributor"
	must(goldenpath.ConfigurationInput{Kind: "project.member.remove", ScopeID: project.Object.ID, UserID: member.ID, ClientOperationID: "revoke", ExpectedRole: &role})
	if _, e := messaging.CreateMessage(ctx, invocation(member, "cor_foundation_retry"), messageInput); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("revoked retry succeeded", e)
	}
	// Replaying the OLD successful grant acknowledges the operation, never grants again.
	must(setRole)
	if _, e := service.ReadConfiguration(ctx, member, "project", project.Object.ID); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("old receipt restored permission", e)
	}
	setRole.ClientOperationID = "regrant"
	must(setRole)
	if e := store.AuthorizeChannelRead(ctx, member, channel.Object.ID); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("old channel grant revived", e)
	}
	must(goldenpath.ConfigurationInput{Kind: "channel.member.add", ScopeID: channel.Object.ID, UserID: member.ID, ClientOperationID: "join-channel"})
	var count int
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.thread_memberships WHERE workspace_id=$1 AND thread_id=$2 AND user_id=$3`, owner.WorkspaceID, thread.ID, member.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("old private thread grant revived", count, e)
	}
	page, e := service.ListConfiguration(ctx, owner, goldenpath.ConfigurationQuery{Kind: "project-members", ScopeID: project.Object.ID, Limit: 1})
	if e != nil || len(page.Members) != 1 || page.NextID == "" {
		t.Fatal(page, e)
	}
	for _, kind := range []string{"teams", "members"} {
		if _, e = service.ListConfiguration(ctx, member, goldenpath.ConfigurationQuery{Kind: kind, ScopeID: owner.WorkspaceID, Limit: 25}); !errors.Is(e, authz.ErrForbidden) {
			t.Fatal("member directory access", e)
		}
	}
	// Inject an audit failure AFTER the business row is inserted.
	if _, e = pool.Exec(ctx, `ALTER TABLE radishnexus.workspace_configuration_audit ADD CONSTRAINT test_audit_failure CHECK(request_id <> 'req_fail_audit')`); e != nil {
		t.Fatal(e)
	}
	_, e = service.Configure(ctx, goldenpath.Invocation{Principal: owner, SourceKind: "web", CorrelationID: "req_fail_audit"}, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: owner.WorkspaceID, Name: "Rollback Team", ClientOperationID: "rollback"})
	if e == nil {
		t.Fatal("audit failure was swallowed")
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.teams WHERE name='Rollback Team'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial business commit", count, e)
	}
	if _, e = pool.Exec(ctx, `UPDATE radishnexus.workspace_configuration_receipts SET payload_sha256=repeat('0',64)`); e == nil {
		t.Fatal("mutable receipt")
	}
	if _, e = pool.Exec(ctx, `DELETE FROM radishnexus.workspace_configuration_audit`); e == nil {
		t.Fatal("mutable audit")
	}
	var before, after int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.activity_items WHERE activity_type IN ('project.created','channel.created')`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = store.RebuildActivityProjection(ctx); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.activity_items WHERE activity_type IN ('project.created','channel.created')`).Scan(&after); e != nil || before != 2 || after != before {
		t.Fatal("rebuild drift", before, after, e)
	}
}
