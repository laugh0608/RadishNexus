//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/db"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/markdown"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/testsupport/pgfault"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const contextTestPassword = "context synthetic test password only"
const contextOperation = "00000000-0000-4000-8000-000000000044"

type contextFixture struct {
	ctx                                                                     context.Context
	pool                                                                    *pgxpool.Pool
	auth                                                                    *authn.Service
	identity                                                                *authn.IdentityService
	store                                                                   *goldenpostgres.Store
	service                                                                 *goldenpath.ContextService
	configuration                                                           *goldenpath.ConfigurationService
	core                                                                    *goldenpath.Service
	documents                                                               *goldenpath.DocumentService
	workspace                                                               string
	owner                                                                   authn.Session
	principal                                                               authz.Principal
	project, channel, message, thread, decision, ticket, document, proposed string
	member                                                                  authz.Principal
}

func contextDatabase(t *testing.T) *contextFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	t.Cleanup(cancel)
	admin, e := pgx.Connect(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	name := fmt.Sprintf("nexus_context_%d", time.Now().UnixNano())
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	config.ConnConfig.Database = name
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		_, e := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize())
		if e != nil {
			t.Error(e)
		}
		admin.Close(context.Background())
	})
	conn, e := pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	e = db.Migrate(ctx, conn.Conn())
	conn.Release()
	if e != nil {
		t.Fatal(e)
	}
	auth := authn.NewService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	boot, e := auth.Bootstrap(ctx, authn.BootstrapInput{Email: "context.owner@example.test", Password: contextTestPassword, DisplayName: "Context Owner", WorkspaceName: "Context Workspace"})
	if e != nil {
		t.Fatal(e)
	}
	owner, e := auth.Login(ctx, authn.LoginInput{Email: "context.owner@example.test", Password: contextTestPassword})
	if e != nil {
		t.Fatal(e)
	}
	store := goldenpostgres.New(pool)
	f := &contextFixture{ctx: ctx, pool: pool, auth: auth, owner: owner, workspace: boot.WorkspaceID, store: store, principal: authz.Principal{Kind: authz.PrincipalUser, ID: boot.UserID, WorkspaceID: boot.WorkspaceID}}
	f.identity = authn.NewIdentityService(authpostgres.New(pool), auth, "")
	f.service = goldenpath.NewContextService(goldenpostgres.NewContextStore(pool, contextSchemaReady))
	f.configuration = goldenpath.NewConfigurationService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	f.core = goldenpath.NewService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	f.documents = goldenpath.NewDocumentService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	return f
}
func (f *contextFixture) invite(t *testing.T, email string) (authn.Session, authz.Principal) {
	t.Helper()
	invite, e := f.identity.CreateInvitation(f.ctx, f.owner.Token, f.workspace)
	if e != nil {
		t.Fatal(e)
	}
	session, e := f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: invite.Token, Email: email, Password: contextTestPassword, DisplayName: "Invited Context Member"})
	if e != nil {
		t.Fatal(e)
	}
	return session, authz.Principal{Kind: authz.PrincipalUser, ID: session.Account.User.ID, WorkspaceID: f.workspace}
}
func (f *contextFixture) configure(t *testing.T, p authz.Principal, input goldenpath.ConfigurationInput) goldenpath.ConfigurationResult {
	t.Helper()
	if input.ClientOperationID == "" {
		id, e := cp.NewID("op_")
		if e != nil {
			t.Fatal(e)
		}
		input.ClientOperationID = id
	}
	result, e := f.configuration.Configure(f.ctx, contextInvocation(p), input)
	if e != nil {
		t.Fatal(e)
	}
	return result
}
func contextInvocation(p authz.Principal) goldenpath.Invocation {
	return goldenpath.Invocation{Principal: p, SourceKind: "web", CorrelationID: "req_context_test"}
}
func (f *contextFixture) seed(t *testing.T) {
	t.Helper()
	_, member := f.invite(t, "context.member@example.test")
	f.member = member
	team := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: f.workspace, Name: "Context team"})
	project := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: f.workspace, Name: "Context project", Key: "portable", OwnerTeamID: team.Object.ID, Visibility: "restricted", InitialAdminUserID: f.principal.ID})
	f.project = project.Object.ID
	f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "project.member.set", ScopeID: f.project, UserID: member.ID, Role: "contributor"})
	ch := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: f.project, Name: "Context private discussion", Visibility: "restricted", MemberUserIDs: []string{f.principal.ID, member.ID}})
	f.channel = ch.Object.ID
	msg, e := f.core.CreateMessage(f.ctx, contextInvocation(member), goldenpath.CreateMessageInput{ChannelID: f.channel, ClientOperationID: "source-message-operation", Body: "Synthetic discussion. https://example.invalid/not-requested"})
	if e != nil {
		t.Fatal(e)
	}
	f.message = msg.Message.ID
	thr, e := f.core.StartThreadFromMessage(f.ctx, contextInvocation(f.principal), goldenpath.StartThreadFromMessageInput{ChannelID: f.channel, MessageID: f.message, Title: "Context source discussion", Visibility: "restricted"})
	if e != nil {
		t.Fatal(e)
	}
	f.thread = thr.ID
	dec, e := f.core.CreateDecisionFromThread(f.ctx, contextInvocation(f.principal), goldenpath.CreateDecisionInput{ThreadID: f.thread, ClientOperationID: "source-proposal", Question: "Which historical choice?"})
	if e != nil {
		t.Fatal(e)
	}
	f.decision = dec.Decision.ID
	if _, e = f.core.AcceptDecision(f.ctx, contextInvocation(f.principal), goldenpath.AcceptDecisionInput{DecisionID: f.decision, ClientOperationID: "source-acceptance", Outcome: "Accept synthetic option", Rationale: "Preserve evidence"}); e != nil {
		t.Fatal(e)
	}
	proposal, e := f.core.CreateDecisionFromThread(f.ctx, contextInvocation(f.principal), goldenpath.CreateDecisionInput{ThreadID: f.thread, ClientOperationID: "source-second-proposal", Question: "Still a proposal?"})
	if e != nil {
		t.Fatal(e)
	}
	f.proposed = proposal.Decision.ID
	ticket, e := f.core.CreateTicketFromDecision(f.ctx, contextInvocation(f.principal), goldenpath.CreateTicketInput{DecisionID: f.decision, ClientOperationID: "source-ticket", Title: "Historical work"})
	if e != nil {
		t.Fatal(e)
	}
	f.ticket = ticket.Ticket.ID
	states := goldenpath.NewTicketStatusService(f.store, goldenpath.CryptoIDGenerator{})
	for i, status := range []string{"in-progress", "done", "open", "canceled"} {
		if _, e = states.ChangeTicketStatus(f.ctx, contextInvocation(f.principal), goldenpath.TicketStatusInput{TicketID: f.ticket, ClientOperationID: fmt.Sprintf("source-state-%d", i), BaseStatusRevision: i + 1, ToStatus: status, Confirmed: true}); e != nil {
			t.Fatal(e)
		}
	}
	doc, e := f.documents.WriteDocument(f.ctx, contextInvocation(f.principal), "document.create", goldenpath.DocumentInput{TargetID: f.ticket, ClientOperationID: "source-doc", Title: "Original history", FormatVersion: markdown.Format, BodyMarkdown: "# Context\n\nhttps://example.invalid/not-requested\n\nentity://decision/" + f.decision})
	if e != nil {
		t.Fatal(e)
	}
	f.document = doc.Ref.ID
	if _, e = f.documents.WriteDocument(f.ctx, contextInvocation(f.principal), "document.save", goldenpath.DocumentInput{TargetID: f.document, ClientOperationID: "source-doc-save", BaseRevision: 1, Title: "Revised history", FormatVersion: markdown.Format, BodyMarkdown: "# Revised\n\n`<script>literal</script>`"}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.documents.WriteDocument(f.ctx, contextInvocation(f.principal), "document.restore", goldenpath.DocumentInput{TargetID: f.document, ClientOperationID: "source-doc-restore", BaseRevision: 2, RestoreRevision: 1}); e != nil {
		t.Fatal(e)
	}
}
func (f *contextFixture) bundle(t *testing.T) cp.Bundle {
	t.Helper()
	b, e := f.service.Export(f.ctx, f.principal, f.owner.Token, f.project)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func (f *contextFixture) plan(t *testing.T, b cp.Bundle) cp.Plan {
	t.Helper()
	p, e := f.service.Prepare(f.ctx, f.principal, f.owner.Token, b, contextOperation)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func contextMapping(t *testing.T, p cp.Plan, kind, id string) string {
	t.Helper()
	for _, v := range p.Mappings {
		if v.Kind == kind && v.SourceID == id {
			return v.TargetID
		}
	}
	t.Fatal("missing mapping", kind)
	return ""
}
func contextSQL(t *testing.T, f *contextFixture, q string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		t.Fatal(e)
	}
}
func contextCount(t *testing.T, f *contextFixture, table string) int {
	t.Helper()
	var n int
	if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM radishnexus."+table).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func TestContextRoundTripPermissionsAndContinuation(t *testing.T) {
	start := time.Now()
	source := contextDatabase(t)
	source.seed(t)
	exportStart := time.Now()
	b := source.bundle(t)
	exportDuration := time.Since(exportStart)
	d, _, e := cp.Unpack(b)
	if e != nil {
		t.Fatal(e)
	}
	raw := string(cp.Encode(b))
	for _, secret := range []string{contextTestPassword, "context.owner@example.test", "password_hash", "token_digest", "source-message-operation", "source-acceptance", "req_context_test", "client_operation_id"} {
		if strings.Contains(raw, secret) {
			t.Fatal("portable envelope leaked excluded identity or command state")
		}
	}
	// Inspect decoded file bytes too: base64 is transport, never redaction.
	for _, file := range b.Files {
		for _, secret := range []string{contextTestPassword, "@example.test", "password_hash", "token_digest", "source-message-operation", "req_context_test", "client_operation_id"} {
			if strings.Contains(string(file.Content), secret) {
				t.Fatal("portable file leaked excluded field")
			}
		}
	}
	target := contextDatabase(t)
	_, outsider := target.invite(t, "context.outsider@example.test")
	beforeUsers := contextCount(t, target, "users")
	prepareStart := time.Now()
	p := target.plan(t, b)
	prepareDuration := time.Since(prepareStart)
	if contextCount(t, target, "projects") != 0 || contextCount(t, target, "users") != beforeUsers || contextCount(t, target, "context_import_batches") != 0 {
		t.Fatal("prepare wrote target")
	}
	applyStart := time.Now()
	result, e := target.service.Apply(target.ctx, target.principal, target.owner.Token, b, p, p.PlanDigest)
	applyDuration := time.Since(applyStart)
	if e != nil || result.Status != "applied" {
		t.Fatal("apply", e)
	}
	again, e := target.service.Apply(target.ctx, target.principal, target.owner.Token, b, p, p.PlanDigest)
	if e != nil || !reflect.DeepEqual(result, again) {
		t.Fatal("exact replay changed result", e)
	}
	receipt, e := target.service.Result(target.ctx, target.principal, target.owner.Token, p.OperationID)
	if e != nil || !reflect.DeepEqual(receipt, result) || strings.Contains(string(cp.Encode(receipt)), `"mappings"`) || strings.Contains(string(cp.Encode(receipt)), source.document) {
		t.Fatal("result disclosed mappings or changed receipt", e)
	}
	otherSession, e := target.auth.Login(target.ctx, authn.LoginInput{Email: "context.outsider@example.test", Password: contextTestPassword})
	if e != nil {
		t.Fatal(e)
	}
	otherResult, e := target.service.Result(target.ctx, outsider, otherSession.Token, p.OperationID)
	if e != nil || otherResult.Status != "not_observed" || otherResult.ProjectID != "" {
		t.Fatal("result disclosed another actor's receipt", e)
	}
	if contextCount(t, target, "context_import_batches") != 1 || contextCount(t, target, "users") != beforeUsers+len(d.Actors) || contextCount(t, target, "outbox_deliveries") != 0 || contextCount(t, target, "collaboration_command_receipts") != 0 {
		t.Fatal("partial graph, replayed delivery, command or identity")
	}
	for _, v := range []struct{ kind, id string }{{"thread", source.thread}, {"decision", source.decision}, {"ticket", source.ticket}, {"document", source.document}, {"channel", source.channel}, {"project", source.project}} {
		ref := entityref.Ref{Type: v.kind, ID: contextMapping(t, p, v.kind, v.id)}
		marker, e := target.service.Source(target.ctx, target.principal, ref)
		if e != nil || !marker.Imported {
			t.Fatal("source marker missing", e)
		}
		if _, e = target.service.Source(target.ctx, outsider, ref); !errors.Is(e, authz.ErrNotFound) {
			t.Fatal("outsider can discover import", e)
		}
	}
	ticket := contextMapping(t, p, "ticket", source.ticket)
	decision := contextMapping(t, p, "decision", source.decision)
	document := contextMapping(t, p, "document", source.document)
	channel := contextMapping(t, p, "channel", source.channel)
	current, e := target.core.GetNexusView(target.ctx, target.principal, entityref.Ref{Type: "ticket", ID: ticket})
	if e != nil || current.Current.Status != "canceled" || current.Current.StatusRevision != 5 || len(current.Timeline) != 5 {
		t.Fatal("Ticket history did not round trip", e)
	}
	for _, v := range current.Timeline {
		if v.SafeFacts["imported"] != "true" {
			t.Fatal("history not marked")
		}
	}
	dec, e := target.core.GetNexusView(target.ctx, target.principal, entityref.Ref{Type: "decision", ID: decision})
	if e != nil || dec.Current.Status != "accepted" {
		t.Fatal("Decision lost historical acceptance", e)
	}
	for revision := 1; revision <= 3; revision++ {
		original, e := source.documents.ReadDocument(source.ctx, source.principal, source.document, revision)
		if e != nil {
			t.Fatal(e)
		}
		imported, e := target.documents.ReadDocument(target.ctx, target.principal, document, revision)
		if e != nil || original.Current.BodyMarkdown != imported.Current.BodyMarkdown || original.Current.Title != imported.Current.Title || !original.Current.CreatedAt.Equal(imported.Current.CreatedAt) {
			t.Fatal("Document content, restoration or original time changed", e)
		}
	}
	var before string
	if e = target.pool.QueryRow(target.ctx, `SELECT jsonb_agg(to_jsonb(a) ORDER BY event_id)::text FROM radishnexus.activity_items a`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = target.store.RebuildActivityProjection(target.ctx); e != nil {
		t.Fatal(e)
	}
	var after string
	if e = target.pool.QueryRow(target.ctx, `SELECT jsonb_agg(to_jsonb(a) ORDER BY event_id)::text FROM radishnexus.activity_items a`).Scan(&after); e != nil || before != after {
		t.Fatal("Activity rebuild changed imported history", e)
	}
	for _, m := range p.Mappings {
		if m.Kind != "actor" {
			continue
		}
		var active, credentials, sessions int
		if e = target.pool.QueryRow(target.ctx, `SELECT (SELECT count(*) FROM radishnexus.user_accounts WHERE user_id=$1 AND status='active'),(SELECT count(*) FROM radishnexus.local_credentials WHERE user_id=$1),(SELECT count(*) FROM radishnexus.user_sessions WHERE user_id=$1)`, m.TargetID).Scan(&active, &credentials, &sessions); e != nil || active+credentials+sessions != 0 {
			t.Fatal("historical identity active", e)
		}
		if _, e = target.pool.Exec(target.ctx, `UPDATE radishnexus.user_accounts SET status='active' WHERE user_id=$1`, m.TargetID); e == nil {
			t.Fatal("historical actor activated")
		}
		if _, e = target.pool.Exec(target.ctx, `UPDATE radishnexus.workspace_memberships SET status='active' WHERE user_id=$1`, m.TargetID); e == nil {
			t.Fatal("historical membership activated")
		}
	}
	// Existing invitation/configuration commands grant a real new member access.
	target.configure(t, target.principal, goldenpath.ConfigurationInput{Kind: "project.member.set", ScopeID: result.ProjectID, UserID: outsider.ID, Role: "contributor"})
	target.configure(t, target.principal, goldenpath.ConfigurationInput{Kind: "channel.member.add", ScopeID: channel, UserID: outsider.ID})
	if _, e = target.core.CreateMessage(target.ctx, contextInvocation(outsider), goldenpath.CreateMessageInput{ChannelID: channel, ClientOperationID: "source-message-operation", Body: "A new native message after import"}); e != nil {
		t.Fatal("native message failed or source operation leaked", e)
	}
	states := goldenpath.NewTicketStatusService(target.store, goldenpath.CryptoIDGenerator{})
	if _, e = states.ChangeTicketStatus(target.ctx, contextInvocation(outsider), goldenpath.TicketStatusInput{TicketID: ticket, ClientOperationID: "source-state-0", BaseStatusRevision: 5, ToStatus: "open", Confirmed: true}); e != nil {
		t.Fatal("native Ticket continuation failed", e)
	}
	if _, e = target.documents.WriteDocument(target.ctx, contextInvocation(outsider), "document.save", goldenpath.DocumentInput{TargetID: document, ClientOperationID: "source-doc-save", Title: "Native after import", BodyMarkdown: "New native content", FormatVersion: markdown.Format, BaseRevision: 3}); e != nil {
		t.Fatal("native Document continuation failed", e)
	}
	// The still-proposed imported Decision can be confirmed by a real target user.
	proposed := contextMapping(t, p, "decision", source.proposed)
	if _, e = target.core.AcceptDecision(target.ctx, contextInvocation(target.principal), goldenpath.AcceptDecisionInput{DecisionID: proposed, ClientOperationID: "target-native-accept", Outcome: "New native decision", Rationale: "Confirmed here"}); e != nil {
		t.Fatal(e)
	}
	reexport, e := target.service.Export(target.ctx, target.principal, target.owner.Token, result.ProjectID)
	if e != nil {
		t.Fatal("re-export failed", e)
	}
	redata, _, e := cp.Unpack(reexport)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range redata.Objects {
		if v.ID == ticket && len(v.Lineage) != 1 {
			t.Fatal("source lineage disappeared")
		}
	}
	third := contextDatabase(t)
	thirdPlan := third.plan(t, reexport)
	if _, e = third.service.Apply(third.ctx, third.principal, third.owner.Token, reexport, thirdPlan, thirdPlan.PlanDigest); e != nil {
		t.Fatal("second generation import failed", e)
	}
	bytes := len(cp.Encode(b.Manifest))
	for _, file := range b.Files {
		bytes += len(file.Content)
	}
	t.Logf("profile round trip: bytes=%d objects=%d history=%d revisions=%d actors=%d export=%s prepare=%s apply_including_projection=%s verification_total=%s", bytes, len(d.Objects), len(d.History), len(d.Revisions), len(d.Actors), exportDuration, prepareDuration, applyDuration, time.Since(start))
}
func TestContextRejectsConflictsAndUnconfirmedCommit(t *testing.T) {
	source := contextDatabase(t)
	source.seed(t)
	b := source.bundle(t)
	t.Run("nonempty and invalid target", func(t *testing.T) {
		target := contextDatabase(t)
		p := target.plan(t, b)
		changed := p
		changed.TargetIdentity = cp.Hash([]byte("other"))
		if _, e := target.service.Apply(target.ctx, target.principal, target.owner.Token, b, changed, changed.PlanDigest); !errors.Is(e, authz.ErrConflict) {
			t.Fatal(e)
		}
		target.configure(t, target.principal, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: target.workspace, Name: "Already exists"})
		if _, e := target.service.Prepare(target.ctx, target.principal, target.owner.Token, b, contextOperation); !errors.Is(e, authz.ErrConflict) {
			t.Fatal("nonempty prepare accepted", e)
		}
		if _, e := target.service.Apply(target.ctx, target.principal, target.owner.Token, b, p, p.PlanDigest); !errors.Is(e, authz.ErrConflict) {
			t.Fatal("stale empty check accepted", e)
		}
		if contextCount(t, target, "context_import_batches") != 0 || contextCount(t, target, "teams") != 1 {
			t.Fatal("nonempty target changed")
		}
	})
	t.Run("concurrent exact replay", func(t *testing.T) {
		target := contextDatabase(t)
		p := target.plan(t, b)
		var wg sync.WaitGroup
		results := make([]cp.Result, 2)
		errs := make([]error, 2)
		for i := range 2 {
			wg.Go(func() {
				results[i], errs[i] = target.service.Apply(target.ctx, target.principal, target.owner.Token, b, p, p.PlanDigest)
			})
		}
		wg.Wait()
		if errs[0] != nil || errs[1] != nil || !reflect.DeepEqual(results[0], results[1]) || contextCount(t, target, "context_import_batches") != 1 {
			t.Fatal("concurrent apply", errs)
		}
		d, _, e := cp.Unpack(b)
		if e != nil {
			t.Fatal(e)
		}
		p2, e := cp.BuildPlan(b.Manifest, d, p.WorkspaceID, p.TargetIdentity, p.ActorID, "00000000-0000-4000-8000-000000000045")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = target.service.Apply(target.ctx, target.principal, target.owner.Token, b, p2, p2.PlanDigest); !errors.Is(e, authz.ErrConflict) {
			t.Fatal("same package replayed under new operation", e)
		}
		modified := b
		modified.Manifest.PackageID = "pkg_changed"
		if _, e = target.service.Apply(target.ctx, target.principal, target.owner.Token, modified, p, p.PlanDigest); !errors.Is(e, authz.ErrConflict) {
			t.Fatal("changed input reused operation", e)
		}
	})
	t.Run("activity fault rolls back", func(t *testing.T) {
		target := contextDatabase(t)
		p := target.plan(t, b)
		users := contextCount(t, target, "users")
		contextSQL(t, target, `CREATE FUNCTION public.context_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'isolated projection fault'; END $$; CREATE TRIGGER context_fault BEFORE INSERT ON radishnexus.activity_items FOR EACH ROW EXECUTE FUNCTION public.context_fault()`)
		if _, e := target.service.Apply(target.ctx, target.principal, target.owner.Token, b, p, p.PlanDigest); e == nil {
			t.Fatal("projection fault succeeded")
		}
		for _, table := range []string{"projects", "teams", "context_import_batches", "context_import_history", "context_import_mappings", "domain_events", "activity_items"} {
			if contextCount(t, target, table) != 0 {
				t.Fatal("partial import survived", table)
			}
		}
		if contextCount(t, target, "users") != users {
			t.Fatal("historical identity survived failed transaction")
		}
	})
	t.Run("actual commit acknowledgement loss", func(t *testing.T) {
		target := contextDatabase(t)
		p := target.plan(t, b)
		config := target.pool.Config().Copy()
		proxied, finish := pgfault.LoseCommit(t, config.ConnConfig)
		config.ConnConfig = proxied
		config.MaxConns = 1
		pool, e := pgxpool.NewWithConfig(target.ctx, config)
		if e != nil {
			t.Fatal(e)
		}
		service := goldenpath.NewContextService(goldenpostgres.NewContextStore(pool, contextSchemaReady))
		_, e = service.Apply(target.ctx, target.principal, target.owner.Token, b, p, p.PlanDigest)
		pool.Close()
		finish()
		if !errors.Is(e, cp.ErrUnconfirmed) {
			t.Fatal("COMMIT loss not uncertain", e)
		}
		status, e := target.service.Result(target.ctx, target.principal, target.owner.Token, p.OperationID)
		if e != nil || status.Status != "applied" {
			t.Fatal("committed result absent", e)
		}
		again, e := target.service.Apply(target.ctx, target.principal, target.owner.Token, b, p, p.PlanDigest)
		if e != nil || !reflect.DeepEqual(status, again) {
			t.Fatal("uncertain import replayed", e)
		}
	})
}
func TestContextExportAuthorizationAndHiddenClosure(t *testing.T) {
	source := contextDatabase(t)
	source.seed(t)
	outsiderSession, outsider := source.invite(t, "context.unauthorized@example.test")
	if _, e := source.service.Export(source.ctx, outsider, outsiderSession.Token, source.project); !errors.Is(e, cp.ErrIncomplete) {
		t.Fatal("nonadmin exported", e)
	}
	// A second existing owner still has no Project or private-content access.
	contextSQL(t, source, `UPDATE radishnexus.workspace_memberships SET role='owner',role_revision=role_revision+1 WHERE workspace_id=$1 AND user_id=$2`, source.workspace, outsider.ID)
	if _, e := source.service.Export(source.ctx, outsider, outsiderSession.Token, source.project); !errors.Is(e, cp.ErrIncomplete) {
		t.Fatal("owner bypassed Project access", e)
	}
	contextSQL(t, source, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, outsider.ID)
	if _, e := source.service.Export(source.ctx, outsider, outsiderSession.Token, source.project); !errors.Is(e, authn.ErrInvalidSession) {
		t.Fatal("disabled user exported", e)
	}
	// A contributor creates a private Thread and a proposed Decision from it.
	// Project admin can read the Decision but cannot read its private evidence.
	msg, e := source.core.CreateMessage(source.ctx, contextInvocation(source.member), goldenpath.CreateMessageInput{ChannelID: source.channel, ClientOperationID: "hidden-source", Body: "Synthetic private source"})
	if e != nil {
		t.Fatal(e)
	}
	thr, e := source.core.StartThreadFromMessage(source.ctx, contextInvocation(source.member), goldenpath.StartThreadFromMessageInput{ChannelID: source.channel, MessageID: msg.Message.ID, Title: "Private evidence", Visibility: "restricted"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = source.core.CreateDecisionFromThread(source.ctx, contextInvocation(source.member), goldenpath.CreateDecisionInput{ThreadID: thr.ID, ClientOperationID: "hidden-proposal", Question: "Private proposal"}); e != nil {
		t.Fatal(e)
	}

	_, e = source.service.Export(source.ctx, source.principal, source.owner.Token, source.project)
	if !errors.Is(e, cp.ErrIncomplete) || strings.Contains(e.Error(), source.channel) || contextCount(t, source, "context_export_audit") != 0 {
		t.Fatal("incomplete authorized graph was published", e)
	}
}
func TestContextDatabaseRejectsForgedProvenance(t *testing.T) {
	source := contextDatabase(t)
	source.seed(t)
	b := source.bundle(t)
	target := contextDatabase(t)
	p := target.plan(t, b)
	if _, e := target.service.Apply(target.ctx, target.principal, target.owner.Token, b, p, p.PlanDigest); e != nil {
		t.Fatal(e)
	}
	event := contextMapping(t, p, "event", func() string { d, _, _ := cp.Unpack(b); return d.History[0].ID }())
	attempts := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO radishnexus.outbox_deliveries(event_id,consumer,state,attempt_count) VALUES($1,'activity-projector','pending',0)`, []any{event}},
		{`INSERT INTO radishnexus.domain_events(event_id,event_type,schema_version,workspace_id,actor_kind,actor_id,source_kind,source_id,primary_entity_type,primary_entity_id,project_id,correlation_id,occurred_at,payload) VALUES('evt_forged','context.history-recorded',1,$1,'user',$2,'import',$3,'project',$4,$4,'cor_forged',clock_timestamp(),'{}')`, []any{target.workspace, target.principal.ID, p.OperationID, contextMapping(t, p, "project", source.project)}},
		{`UPDATE radishnexus.context_import_history SET original_facts='{}' WHERE event_id=$1`, []any{event}},
		{`INSERT INTO radishnexus.document_revisions(workspace_id,document_id,revision,title,body_markdown,format_version,created_by,created_at,source_event_id) VALUES($1,$2,4,'forged','forged','nexus-markdown-v1',$3,clock_timestamp(),$4)`, []any{target.workspace, contextMapping(t, p, "document", source.document), target.principal.ID, event}},
	}
	for i, attempt := range attempts {
		_, e := target.pool.Exec(target.ctx, attempt.query, attempt.args...)
		var pgerr *pgconn.PgError
		if !errors.As(e, &pgerr) || pgerr.Code != "23514" {
			t.Fatalf("forged provenance case %d expected check violation, got %v", i, e)
		}
	}
	// Direct insertion of a native Ticket transition still needs a real native receipt.
	if _, e := target.pool.Exec(target.ctx, `INSERT INTO radishnexus.domain_events(event_id,event_type,schema_version,workspace_id,actor_kind,actor_id,source_kind,primary_entity_type,primary_entity_id,project_id,correlation_id,occurred_at,payload) VALUES('evt_fake_native','ticket.status-changed',1,$1,'user',$2,'web','ticket',$3,$4,'cor_fake',clock_timestamp(),'{"from_status":"canceled","to_status":"open","from_status_revision":5,"to_status_revision":6}')`, target.workspace, target.principal.ID, contextMapping(t, p, "ticket", source.ticket), contextMapping(t, p, "project", source.project)); e == nil {
		t.Fatal("native receipt check weakened")
	}
}

func contextSchemaReady(ctx context.Context, tx pgx.Tx) error {
	checker, e := db.NewReadinessChecker(tx)
	if e != nil {
		return e
	}
	return checker.CheckReady(ctx)
}

func contextGate(t *testing.T, f *contextFixture, table string) func() {
	t.Helper()
	contextSQL(t, f, `CREATE FUNCTION public.context_gate() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(904400); RETURN NEW; END $$; CREATE TRIGGER context_gate BEFORE INSERT ON radishnexus.`+table+` FOR EACH ROW EXECUTE FUNCTION public.context_gate()`)
	connection, err := f.pool.Acquire(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = connection.Exec(f.ctx, `SELECT pg_advisory_lock(904400)`); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			_, e := connection.Exec(context.Background(), `SELECT pg_advisory_unlock(904400)`)
			if e != nil {
				t.Error(e)
			}
			connection.Release()
		})
	}
	t.Cleanup(release)
	return release
}
func contextWaitGate(t *testing.T, f *contextFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	waitProjectionLock(t, ctx, f.pool, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND locktype='advisory' AND objid=904400 AND NOT granted)`)
}
func TestContextExportSnapshotAndRevocationOrdering(t *testing.T) {
	t.Run("export snapshot precedes later revocation", func(t *testing.T) {
		source := contextDatabase(t)
		source.seed(t)
		release := contextGate(t, source, "context_export_audit")
		type result struct {
			bundle cp.Bundle
			err    error
		}
		done := make(chan result, 1)
		go func() {
			b, e := source.service.Export(source.ctx, source.principal, source.owner.Token, source.project)
			done <- result{b, e}
		}()
		contextWaitGate(t, source)
		message, e := source.core.CreateMessage(source.ctx, contextInvocation(source.principal), goldenpath.CreateMessageInput{ChannelID: source.channel, ClientOperationID: "concurrent-export-message", Body: "Created after export snapshot"})
		if e != nil {
			t.Fatal(e)
		}
		revoke := make(chan error, 1)
		go func() {
			_, e := source.pool.Exec(source.ctx, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, source.principal.ID)
			revoke <- e
		}()
		ctx, cancel := context.WithTimeout(source.ctx, 3*time.Second)
		waitProjectionLock(t, ctx, source.pool, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'UPDATE radishnexus.user_accounts%')`)
		cancel()
		release()
		exported := <-done
		if exported.err != nil {
			t.Fatal(exported.err)
		}
		if e := <-revoke; e != nil {
			t.Fatal(e)
		}
		d, _, e := cp.Unpack(exported.bundle)
		if e != nil {
			t.Fatal(e)
		}
		for _, o := range d.Objects {
			if o.ID == message.Message.ID {
				t.Fatal("export mixed a later object into snapshot")
			}
		}
		if contextCount(t, source, "context_export_audit") != 1 {
			t.Fatal("publication audit absent")
		}
		if b, e := source.service.Export(source.ctx, source.principal, source.owner.Token, source.project); !errors.Is(e, authn.ErrInvalidSession) || b.Manifest.PackageID != "" {
			t.Fatal("revoked account received another package", e)
		}
	})
	t.Run("revocation precedes export", func(t *testing.T) {
		source := contextDatabase(t)
		source.seed(t)
		tx, e := source.pool.Begin(source.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(context.Background())
		if _, e = tx.Exec(source.ctx, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, source.principal.ID); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() {
			b, e := source.service.Export(source.ctx, source.principal, source.owner.Token, source.project)
			if b.Manifest.PackageID != "" {
				e = errors.New("revoked export returned a package")
			}
			done <- e
		}()
		ctx, cancel := context.WithTimeout(source.ctx, 3*time.Second)
		waitProjectionLock(t, ctx, source.pool, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM radishnexus.user_accounts%')`)
		cancel()
		if e = tx.Commit(source.ctx); e != nil {
			t.Fatal(e)
		}
		if e = <-done; e == nil {
			t.Fatal("preceding revocation did not block export")
		}
		if contextCount(t, source, "context_export_audit") != 0 {
			t.Fatal("revoked export published audit")
		}
	})
}
func TestContextCanceledImportRollsBack(t *testing.T) {
	source := contextDatabase(t)
	source.seed(t)
	b := source.bundle(t)
	for _, mode := range []string{"cancel", "terminate-backend"} {
		t.Run(mode, func(t *testing.T) {
			target := contextDatabase(t)
			p := target.plan(t, b)
			users := contextCount(t, target, "users")
			release := contextGate(t, target, "activity_items")
			ctx, cancel := context.WithCancel(target.ctx)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, e := target.service.Apply(ctx, target.principal, target.owner.Token, b, p, p.PlanDigest)
				done <- e
			}()
			contextWaitGate(t, target)
			if mode == "cancel" {
				cancel()
			} else {
				var terminated bool
				e := target.pool.QueryRow(target.ctx, `SELECT pg_terminate_backend(pid) FROM pg_locks WHERE database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND locktype='advisory' AND objid=904400 AND NOT granted`).Scan(&terminated)
				if e != nil || !terminated {
					t.Fatal("backend termination failed", e)
				}
			}
			if e := <-done; e == nil {
				t.Fatal("interrupted import succeeded")
			}
			release()
			for _, table := range []string{"projects", "teams", "context_import_batches", "context_import_history", "context_import_mappings", "domain_events", "activity_items", "outbox_deliveries"} {
				if contextCount(t, target, table) != 0 {
					t.Fatal("interrupted import left partial data", table)
				}
			}
			if contextCount(t, target, "users") != users {
				t.Fatal("interrupted import left historical users")
			}
			status, e := target.service.Result(target.ctx, target.principal, target.owner.Token, p.OperationID)
			if e != nil || status.Status != "not_observed" {
				t.Fatal("interrupted import receipt", e)
			}
		})
	}
}
