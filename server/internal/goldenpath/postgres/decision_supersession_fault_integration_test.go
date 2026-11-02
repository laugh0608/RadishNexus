//go:build integration

package postgres_test

import (
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/testsupport/pgfault"
)

func TestDecisionSupersessionAtomicFailuresAndLostCommit(t *testing.T) {
	f, ids := supersessionFixture(t)
	input := supersessionInput(ids[0], ids[1], "atomic-supersession")
	tables := []string{"decisions", "entity_links", "collaboration_command_receipts", "domain_events", "outbox_deliveries", "activity_items"}
	baseline := map[string]int{}
	for _, table := range tables {
		baseline[table] = contextCount(t, f, table)
	}
	ordered := append([]string{}, ids[:2]...)
	sort.Strings(ordered)
	for _, table := range tables {
		action, condition := "INSERT", ""
		if table == "decisions" {
			action = "UPDATE"
			condition = fmt.Sprintf(" WHEN (NEW.id='%s')", ordered[1])
		}
		if table == "activity_items" {
			condition = fmt.Sprintf(" WHEN (NEW.target_id='%s')", ids[1])
		}
		contextSQL(t, f, `CREATE FUNCTION public.supersession_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'isolated supersession failure'; END $$; CREATE TRIGGER supersession_fault BEFORE `+action+` ON radishnexus.`+table+` FOR EACH ROW`+condition+` EXECUTE FUNCTION public.supersession_fault()`)
		if _, e := f.core.SupersedeDecision(f.ctx, contextInvocation(f.principal), input); e == nil {
			t.Fatal("ignored fault", table)
		}
		contextSQL(t, f, `DROP TRIGGER supersession_fault ON radishnexus.`+table+`; DROP FUNCTION public.supersession_fault()`)
		for _, check := range tables {
			if contextCount(t, f, check) != baseline[check] {
				t.Fatal("partial write", table, check)
			}
		}
		var changed int
		if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM radishnexus.decisions WHERE id=ANY($1::text[]) AND (status<>'accepted' OR decision_revision<>2)`, ids).Scan(&changed); e != nil || changed != 0 {
			t.Fatal("partial revision", table, changed, e)
		}
	}
	config := f.pool.Config().Copy()
	proxy, finish := pgfault.LoseCommit(t, config.ConnConfig)
	config.ConnConfig = proxy
	config.MaxConns = 1
	faulty, e := pgxpool.NewWithConfig(f.ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	core := goldenpath.NewService(goldenpostgres.New(faulty), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	_, e = core.SupersedeDecision(f.ctx, contextInvocation(f.principal), input)
	faulty.Close()
	finish()
	if e == nil {
		t.Fatal("lost commit confirmation reported success")
	}
	result, e := f.core.SupersedeDecision(f.ctx, contextInvocation(f.principal), input)
	if e != nil {
		t.Fatal("committed original replay", e)
	}
	var events, activities int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*),(SELECT count(*) FROM radishnexus.activity_items WHERE event_id=$1) FROM radishnexus.domain_events WHERE event_type='decision.superseded'`, func() string {
		var id string
		if e := f.pool.QueryRow(f.ctx, `SELECT source_event_id FROM radishnexus.entity_links WHERE id=$1`, result.LinkID).Scan(&id); e != nil {
			t.Fatal(e)
		}
		return id
	}()).Scan(&events, &activities); e != nil || events != 1 || activities != 2 {
		t.Fatal("duplicate or incomplete effect", events, activities, e)
	}
	for _, sql := range []string{`UPDATE radishnexus.entity_links SET metadata='{}' WHERE id=$1`, `DELETE FROM radishnexus.entity_links WHERE id=$1`, `UPDATE radishnexus.entity_links SET state='removed',removed_by_kind='user',removed_by_id='usr_fake',removed_at=clock_timestamp(),removal_reason='fake removal' WHERE id=$1`} {
		if _, e = f.pool.Exec(f.ctx, sql, result.LinkID); e == nil {
			t.Fatal("mutable supersession")
		}
	}
	if _, e = f.pool.Exec(f.ctx, `UPDATE radishnexus.decisions SET status='accepted' WHERE id=$1`, ids[0]); e == nil {
		t.Fatal("superseded reopened")
	}
}

func TestDecisionSupersessionCurrentPermissions(t *testing.T) {
	f, ids := supersessionFixture(t)
	input := supersessionInput(ids[0], ids[1], "permission-supersession")
	check := func(want error) {
		t.Helper()
		if _, e := f.core.SupersedeDecision(f.ctx, contextInvocation(f.member), input); !errors.Is(e, want) {
			t.Fatal("permission outcome", e, want)
		}
	}
	check(authz.ErrForbidden)
	// Permission fault fixtures only; all Decisions were created by formal commands.
	contextSQL(t, f, `UPDATE radishnexus.project_memberships SET role='decider' WHERE project_id=$1 AND user_id=$2`, f.project, f.member.ID)
	check(authz.ErrForbidden)
	v, e := f.core.GetNexusView(f.ctx, f.member, entityref.Ref{Type: "decision", ID: ids[1]})
	if e != nil || v.DecisionCapabilities.RetryDecision || v.DecisionCapabilities.Supersede {
		t.Fatal("private evidence authorized action", e)
	}
	contextSQL(t, f, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,management_role) VALUES($1,$2,$3,'member')`, f.workspace, f.thread, f.member.ID)
	for _, role := range []string{"viewer", "contributor"} {
		contextSQL(t, f, `UPDATE radishnexus.project_memberships SET role=$1 WHERE project_id=$2 AND user_id=$3`, role, f.project, f.member.ID)
		check(authz.ErrForbidden)
	}
	contextSQL(t, f, `UPDATE radishnexus.workspace_memberships SET role='owner',role_revision=role_revision+1 WHERE workspace_id=$1 AND user_id=$2`, f.workspace, f.member.ID)
	check(authz.ErrForbidden)
	contextSQL(t, f, `UPDATE radishnexus.workspace_memberships SET role='member',role_revision=role_revision+1 WHERE workspace_id=$1 AND user_id=$2`, f.workspace, f.member.ID)
	contextSQL(t, f, `UPDATE radishnexus.project_memberships SET role='decider' WHERE project_id=$1 AND user_id=$2`, f.project, f.member.ID)
	contextSQL(t, f, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, f.member.ID)
	check(authn.ErrInvalidSession)
	contextSQL(t, f, `UPDATE radishnexus.user_accounts SET status='active' WHERE user_id=$1`, f.member.ID)
	contextSQL(t, f, `UPDATE radishnexus.projects SET status='archived' WHERE id=$1`, f.project)
	check(authz.ErrConflict)
	contextSQL(t, f, `UPDATE radishnexus.projects SET status='active' WHERE id=$1`, f.project)
	for _, state := range []string{"suspended", "removed"} {
		contextSQL(t, f, `UPDATE radishnexus.workspace_memberships SET status=$1 WHERE workspace_id=$2 AND user_id=$3`, state, f.workspace, f.member.ID)
		check(authz.ErrNotFound)
		contextSQL(t, f, `UPDATE radishnexus.workspace_memberships SET status='active' WHERE workspace_id=$1 AND user_id=$2`, f.workspace, f.member.ID)
	}
	result, e := f.core.SupersedeDecision(f.ctx, contextInvocation(f.member), input)
	if e != nil {
		t.Fatal("authorized decider", e)
	}
	contextSQL(t, f, `DELETE FROM radishnexus.thread_memberships WHERE workspace_id=$1 AND thread_id=$2 AND user_id=$3`, f.workspace, f.thread, f.member.ID)
	check(authz.ErrForbidden)
	if result.SupersessionReason != input.SupersessionReason {
		t.Fatal("reason changed")
	}
}

func TestDecisionSupersessionBothEvidenceAndScopes(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	inv := contextInvocation(f.principal)
	makeAccepted := func(principal authz.Principal, channel, operation string) (string, string) {
		t.Helper()
		message, err := f.core.CreateMessage(f.ctx, contextInvocation(principal), goldenpath.CreateMessageInput{ChannelID: channel, ClientOperationID: operation + "-message", Body: "Synthetic independent evidence"})
		if err != nil {
			t.Fatal(err)
		}
		thread, err := f.core.StartThreadFromMessage(f.ctx, contextInvocation(principal), goldenpath.StartThreadFromMessageInput{ChannelID: channel, MessageID: message.Message.ID, Title: operation, Visibility: "restricted"})
		if err != nil {
			t.Fatal(err)
		}
		decision, err := f.core.CreateDecisionFromThread(f.ctx, contextInvocation(principal), goldenpath.CreateDecisionInput{ThreadID: thread.ID, ClientOperationID: operation + "-proposal", Question: operation})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.core.AcceptDecision(f.ctx, contextInvocation(principal), goldenpath.AcceptDecisionInput{DecisionID: decision.Decision.ID, ClientOperationID: operation + "-accept", Outcome: "Explicit accepted choice", Rationale: "Current readable evidence"}); err != nil {
			t.Fatal(err)
		}
		return decision.Decision.ID, thread.ID
	}
	newID, newThread := makeAccepted(f.principal, f.channel, "independent-source")
	input := supersessionInput(f.decision, newID, "both-sources")
	// Only permission fixtures use SQL; both Decisions and their distinct evidence
	// Threads were created and accepted through formal commands.
	contextSQL(t, f, `UPDATE radishnexus.project_memberships SET role='decider' WHERE project_id=$1 AND user_id=$2`, f.project, f.member.ID)
	for _, readable := range []string{f.thread, newThread} {
		contextSQL(t, f, `DELETE FROM radishnexus.thread_memberships WHERE user_id=$1 AND thread_id=ANY($2::text[])`, f.member.ID, []string{f.thread, newThread})
		contextSQL(t, f, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,management_role) VALUES($1,$2,$3,'member')`, f.workspace, readable, f.member.ID)
		if _, err := f.core.SupersedeDecision(f.ctx, contextInvocation(f.member), input); !errors.Is(err, authz.ErrForbidden) {
			t.Fatal("one readable source authorized pair", readable, err)
		}
	}
	contextSQL(t, f, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,management_role) VALUES($1,$2,$3,'member')`, f.workspace, f.thread, f.member.ID)
	if _, err := f.core.SupersedeDecision(f.ctx, contextInvocation(f.member), input); err != nil {
		t.Fatal("both independent sources", err)
	}

	projectIn := func(principal authz.Principal, workspace, key string) string {
		t.Helper()
		team := f.configure(t, principal, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: workspace, Name: key})
		project := f.configure(t, principal, goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: workspace, Name: key, Key: key, OwnerTeamID: team.Object.ID, InitialAdminUserID: principal.ID, Visibility: "workspace"})
		channel := f.configure(t, principal, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: project.Object.ID, Name: key, Visibility: "project", MemberUserIDs: []string{}})
		decision, _ := makeAccepted(principal, channel.Object.ID, key)
		return decision
	}
	otherProjectDecision := projectIn(f.principal, f.workspace, "supersession-other-project")
	if _, err := f.core.SupersedeDecision(f.ctx, inv, supersessionInput(newID, otherProjectDecision, "cross-project")); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("cross Project", err)
	}
	// Bootstrap is single-use: this additional Workspace and owner membership
	// are foundation scope fixtures, with all business facts still formal.
	contextSQL(t, f, `INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_supersession_other','Other supersession Workspace')`)
	contextSQL(t, f, `INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,role) VALUES('wrk_supersession_other',$1,'owner')`, f.principal.ID)
	foreign := f.principal
	foreign.WorkspaceID = "wrk_supersession_other"
	foreignDecision := projectIn(foreign, foreign.WorkspaceID, "supersession-foreign")
	if _, err := f.core.SupersedeDecision(f.ctx, inv, supersessionInput(newID, foreignDecision, "cross-workspace")); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("cross Workspace", err)
	}
}
