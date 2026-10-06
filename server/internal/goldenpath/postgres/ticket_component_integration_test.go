//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
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

// Only identities and permission conditions come from the shared database seed.
// Every tested Ticket follows Message -> Thread -> accepted Decision -> Ticket.
func componentTicket(t *testing.T, ctx context.Context, s *goldenpath.Service, channel, key string) (goldenpath.Ticket, string) {
	t.Helper()
	inv := invocation(principal("usr_decider"), "ticket-component-"+key)
	inv.SourceKind = "web"
	m, e := s.CreateMessage(ctx, inv, goldenpath.CreateMessageInput{ChannelID: channel, ClientOperationID: key, Body: "Component evidence " + key})
	if e != nil {
		t.Fatal(e)
	}
	th, e := s.StartThreadFromMessage(ctx, inv, goldenpath.StartThreadFromMessageInput{ChannelID: channel, MessageID: m.Message.ID, Title: "Evidence " + key, Visibility: "restricted"})
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.CreateDecisionFromThread(ctx, inv, goldenpath.CreateDecisionInput{ThreadID: th.ID, ClientOperationID: key, Question: "Proceed " + key})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.AcceptDecision(ctx, inv, goldenpath.AcceptDecisionInput{DecisionID: d.Decision.ID, ClientOperationID: key, Outcome: "Proceed", Rationale: "Reviewed evidence"}); e != nil {
		t.Fatal(e)
	}
	ticket, e := s.CreateTicketFromDecision(ctx, inv, goldenpath.CreateTicketInput{DecisionID: d.Decision.ID, ClientOperationID: key, Title: "Work " + key})
	if e != nil {
		t.Fatal(e)
	}
	return ticket.Ticket, th.ID
}
func ticketComponentFixture(t *testing.T) (context.Context, *pgxpool.Pool, *goldenpath.Service, *goldenpath.TicketComponentService, *goldenpath.ConfigurationService, func(string) string) {
	t.Helper()
	ctx, pool := documentDatabase(t)
	if _, e := pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET role='owner' WHERE workspace_id='wrk_main' AND user_id='usr_admin'`); e != nil {
		t.Fatal(e)
	}
	store := goldenpostgres.New(pool)
	core := goldenpath.NewService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	relations := goldenpath.NewTicketComponentService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	cfg := goldenpath.NewConfigurationService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	component := func(key string) string {
		t.Helper()
		r, e := cfg.Configure(ctx, goldenpath.Invocation{Principal: principal("usr_admin"), SourceKind: "web", CorrelationID: "tc-component"}, goldenpath.ConfigurationInput{Kind: "component.create", ScopeID: "wrk_main", ClientOperationID: key, Key: key, Name: "Component " + key, OwnerTeamID: "tem_main", Delivery: &goldenpath.DeliveryConfigurationInput{Type: "service"}})
		if e != nil {
			t.Fatal(e)
		}
		return r.Object.ID
	}
	return ctx, pool, core, relations, cfg, component
}
func tcInvocation(user string) goldenpath.Invocation {
	return goldenpath.Invocation{Principal: principal(user), SourceKind: "web", CorrelationID: "tc-test"}
}

func TestTicketComponentHTTPHistoryAndCurrentPermissions(t *testing.T) {
	ctx, pool, core, s, _, component := ticketComponentFixture(t)
	ticket, thread := componentTicket(t, ctx, core, "chn_restricted", "main")
	cid := component("tc-main")
	if _, e := core.GetNexusView(ctx, principal("usr_contributor"), entityref.Ref{Type: "thread", ID: thread}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("restricted evidence unexpectedly readable", e)
	}
	// Real Session and CSRF, with only test credential digests stored.
	token, csrf := deploymentHTTPToken(61), deploymentHTTPToken(62)
	td, cd := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(csrf))
	now := time.Now()
	if _, e := pool.Exec(ctx, `INSERT INTO radishnexus.user_sessions(id,user_id,token_digest,csrf_token_digest,created_at,expires_at) VALUES('ses_tc','usr_contributor',$1,$2,$3,$4)`, td[:], cd[:], now, now.Add(authn.SessionLifetime)); e != nil {
		t.Fatal(e)
	}
	auth := authn.NewService(authpostgres.New(pool), nil, nil, fixedClock{now: now})
	policy, _ := httptransport.NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := httptransport.NewTrustedProxyPolicy("127.0.0.1/32")
	mux := http.NewServeMux()
	httptransport.RegisterTicketComponentRoutes(mux, httptransport.NewTicketComponentHandler(auth, s, policy, proxy))
	views := httptransport.NewCollaborationHandler(auth, core, policy, proxy)
	request := func(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "https://nexus.example.test/api/v1/workspaces/wrk_main"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: token})
		r.AddCookie(&http.Cookie{Name: httptransport.CSRFCookieName, Value: csrf})
		r.Header.Set(httptransport.CSRFHeaderName, csrf)
		r.Header.Set("Origin", "https://nexus.example.test")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httptransport.WithRequestID(handler).ServeHTTP(w, r)
		return w
	}
	body := fmt.Sprintf(`{"client_operation_id":"link","component_id":%q,"confirmed":true}`, cid)
	path := "/tickets/" + ticket.ID + "/components"
	w := request(mux, "POST", path, body)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			LinkID string `json:"link_id"`
		}
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	first := out.Data.LinkID
	w = request(mux, "POST", path, body)
	if w.Code != 200 || !strings.Contains(w.Body.String(), first) {
		t.Fatal("HTTP retry", w.Code, w.Body.String())
	}
	inv := tcInvocation("usr_contributor")
	link := goldenpath.TicketComponentInput{TicketID: ticket.ID, ComponentID: cid, ClientOperationID: "link", Confirmed: true}
	changed := link
	changed.ComponentID = component("tc-other")
	if _, e := s.WriteTicketComponent(ctx, inv, "ticket.component.link", changed); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("changed receipt", e)
	}
	changed = link
	changed.ClientOperationID = "duplicate"
	if _, e := s.WriteTicketComponent(ctx, inv, "ticket.component.link", changed); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("duplicate active pair", e)
	}
	assertPair := func(want string) {
		t.Helper()
		for _, ref := range []entityref.Ref{{Type: "ticket", ID: ticket.ID}, {Type: "component", ID: cid}} {
			page, e := s.ListTicketComponents(ctx, principal("usr_reader"), ref, goldenpath.DiscoveryPageInput{Limit: 25})
			if e != nil {
				t.Fatal(e)
			}
			if want == "" {
				if len(page.Links) != 0 {
					t.Fatal("removed relation visible", page)
				}
			} else if len(page.Links) != 1 || page.Links[0].ID != want || page.Links[0].CanUnlink || page.CanLink {
				t.Fatal("viewer projection", page)
			}
		}
	}
	assertPair(first)
	w = request(views, "GET", "/tickets/"+ticket.ID+"/nexus-view", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"relation_state":"active"`) || strings.Contains(w.Body.String(), thread) {
		t.Fatal("Ticket source/Timeline contract", w.Code, w.Body.String())
	}
	original, e := core.GetNexusView(ctx, principal("usr_contributor"), entityref.Ref{Type: "ticket", ID: ticket.ID})
	if e != nil {
		t.Fatal(e)
	}
	if len(original.Relations) != 2 || len(original.Timeline) != 2 {
		t.Fatal("missing source or component", original)
	}
	unlink := goldenpath.TicketComponentInput{TicketID: ticket.ID, LinkID: first, ClientOperationID: "unlink", Confirmed: true}
	must := func(kind string, in goldenpath.TicketComponentInput) goldenpath.TicketComponentResult {
		t.Helper()
		r, e := s.WriteTicketComponent(ctx, inv, kind, in)
		if e != nil {
			t.Fatal(kind, e)
		}
		return r
	}
	must("ticket.component.unlink", unlink)
	assertPair("")
	if r := must("ticket.component.link", link); r.LinkID != first || r.Created {
		t.Fatal("historical receipt", r)
	}
	assertPair("")
	changedUnlink := unlink
	changedUnlink.ClientOperationID = "fresh-removed"
	if _, e = s.WriteTicketComponent(ctx, inv, "ticket.component.unlink", changedUnlink); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("fresh unlink on removed", e)
	}
	link.ClientOperationID = "relink"
	second := must("ticket.component.link", link).LinkID
	if second == first {
		t.Fatal("reused removed identity")
	}
	must("ticket.component.unlink", unlink)
	assertPair(second)
	view, e := core.GetNexusView(ctx, principal("usr_contributor"), entityref.Ref{Type: "ticket", ID: ticket.ID})
	if e != nil || !reflect.DeepEqual(view.Current, original.Current) || len(view.Timeline) != 4 {
		t.Fatal("Current mutated or Timeline incomplete", view, e)
	}
	if _, e = goldenpostgres.New(pool).RebuildActivityProjection(ctx); e != nil {
		t.Fatal(e)
	}
	rebuilt, e := core.GetNexusView(ctx, principal("usr_contributor"), entityref.Ref{Type: "ticket", ID: ticket.ID})
	if e != nil || !reflect.DeepEqual(view, rebuilt) {
		t.Fatal("projection rebuild drift", e)
	}
	var leaked int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.activity_items WHERE target_type='component' AND target_id=$1 AND activity_type LIKE 'ticket.%'`, cid).Scan(&leaked); e != nil || leaked != 0 {
		t.Fatal("private Ticket Activity reached Component", leaked, e)
	}
	for _, user := range []string{"usr_reader", "usr_admin"} {
		if user == "usr_admin" {
			if _, e = pool.Exec(ctx, `DELETE FROM radishnexus.project_memberships WHERE user_id='usr_admin' AND project_id='prj_auth'`); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = s.WriteTicketComponent(ctx, tcInvocation(user), "ticket.component.link", changed); !errors.Is(e, authz.ErrForbidden) {
			t.Fatal("viewer/owner without Project role wrote", user, e)
		}
	}
	for _, lifecycle := range []string{"planned", "deprecated", "retired"} {
		if _, e = pool.Exec(ctx, `UPDATE radishnexus.components SET lifecycle=$1 WHERE id=$2`, lifecycle, cid); e != nil {
			t.Fatal(e)
		}
		_, e = s.WriteTicketComponent(ctx, inv, "ticket.component.link", link)
		if lifecycle == "retired" {
			if !errors.Is(e, authz.ErrConflict) {
				t.Fatal("retired receipt bypass", e)
			}
		} else if e != nil {
			t.Fatal("nonretired exact retry", e)
		}
	}
	must("ticket.component.unlink", goldenpath.TicketComponentInput{TicketID: ticket.ID, LinkID: second, ClientOperationID: "retired-unlink", Confirmed: true})
	must("ticket.component.unlink", unlink)
	for _, scenario := range []struct {
		sql, restore string
		want         error
	}{
		{`UPDATE radishnexus.projects SET status='archived' WHERE id='prj_auth'`, `UPDATE radishnexus.projects SET status='active' WHERE id='prj_auth'`, authz.ErrForbidden},
		{`UPDATE radishnexus.project_memberships SET role='viewer' WHERE user_id='usr_contributor'`, `UPDATE radishnexus.project_memberships SET role='contributor' WHERE user_id='usr_contributor'`, authz.ErrForbidden},
		{`UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE user_id='usr_contributor'`, `UPDATE radishnexus.workspace_memberships SET status='active' WHERE user_id='usr_contributor'`, authz.ErrNotFound},
		{`UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id='usr_contributor'`, `UPDATE radishnexus.user_accounts SET status='active' WHERE user_id='usr_contributor'`, authz.ErrNotFound},
	} {
		if _, e = pool.Exec(ctx, scenario.sql); e != nil {
			t.Fatal(e)
		}
		if _, e = s.WriteTicketComponent(ctx, inv, "ticket.component.unlink", unlink); !errors.Is(e, scenario.want) {
			t.Fatal("receipt skipped current authorization", e)
		}
		if _, e = pool.Exec(ctx, scenario.restore); e != nil {
			t.Fatal(e)
		}
	}
	foreign := inv
	foreign.Principal.WorkspaceID = "wrk_other"
	if _, e = s.WriteTicketComponent(ctx, foreign, "ticket.component.unlink", unlink); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("cross workspace", e)
	}
	for _, sql := range []string{`UPDATE radishnexus.entity_links SET state='active',removed_by_id=NULL,removed_by_kind=NULL,removed_at=NULL,removal_reason=NULL WHERE id=$1`, `DELETE FROM radishnexus.entity_links WHERE id=$1`, `UPDATE radishnexus.entity_links SET metadata='{"unsafe":true}' WHERE id=$1`} {
		if _, e = pool.Exec(ctx, sql, first); e == nil {
			t.Fatal("mutable relation provenance")
		}
	}
}

func TestTicketComponentFilteredPaginationAndConcurrentWriters(t *testing.T) {
	ctx, pool, core, s, cfg, component := ticketComponentFixture(t)
	cid := component("tc-paging")
	owner := tcInvocation("usr_admin")
	project, e := cfg.Configure(ctx, owner, goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: "wrk_main", ClientOperationID: "hidden-project", Key: "hidden", Name: "Private work", OwnerTeamID: "tem_main", InitialAdminUserID: "usr_admin", Visibility: "restricted"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = cfg.Configure(ctx, owner, goldenpath.ConfigurationInput{Kind: "project.member.set", ScopeID: project.Object.ID, UserID: "usr_decider", ClientOperationID: "hidden-decider", Role: "decider"}); e != nil {
		t.Fatal(e)
	}
	ch, e := cfg.Configure(ctx, owner, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: project.Object.ID, ClientOperationID: "hidden-channel", Name: "Hidden", Visibility: "project", MemberUserIDs: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	expected := []string{}
	hidden := []string{}
	for i := range 30 {
		channel := "chn_project"
		if i%7 != 0 {
			channel = ch.Object.ID
		}
		ticket, _ := componentTicket(t, ctx, core, channel, fmt.Sprintf("page-%02d", i))
		if _, e = s.WriteTicketComponent(ctx, tcInvocation("usr_decider"), "ticket.component.link", goldenpath.TicketComponentInput{TicketID: ticket.ID, ComponentID: cid, ClientOperationID: "page", Confirmed: true}); e != nil {
			t.Fatal(e)
		}
		if channel == "chn_project" {
			expected = append(expected, ticket.ID)
		} else {
			hidden = append(hidden, ticket.ID)
		}
	}
	sort.Strings(expected)
	got := []string{}
	after := ""
	for {
		page, e := s.ListTicketComponents(ctx, principal("usr_reader"), entityref.Ref{Type: "component", ID: cid}, goldenpath.DiscoveryPageInput{Limit: 2, AfterID: after})
		if e != nil {
			t.Fatal(e)
		}
		for _, link := range page.Links {
			got = append(got, link.Ticket.ID)
			if link.CanUnlink {
				t.Fatal("viewer write capability")
			}
		}
		if page.NextID == "" {
			break
		}
		if page.NextID == after || page.NextID != page.Links[len(page.Links)-1].Ticket.ID {
			t.Fatal("cursor disclosure", page)
		}
		after = page.NextID
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatal("authorized rows consumed incorrectly", got, expected)
	}
	relations, e := goldenpostgres.New(pool).ListRelations(ctx, principal("usr_reader"), entityref.Ref{Type: "component", ID: cid})
	if e != nil || len(relations) != len(expected) {
		t.Fatal("core reverse projection leaks hidden rows", relations, e)
	}
	if _, e = s.ListTicketComponents(ctx, principal("usr_reader"), entityref.Ref{Type: "ticket", ID: hidden[0]}, goldenpath.DiscoveryPageInput{Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("private Ticket direct read", e)
	}
	// Independent actor receipts still serialize on the same Component pair.
	ticket, _ := componentTicket(t, ctx, core, "chn_project", "race")
	input := goldenpath.TicketComponentInput{TicketID: ticket.ID, ComponentID: cid, ClientOperationID: "race", Confirmed: true}
	outcomes := make(chan error, 2)
	start := make(chan struct{})
	for _, user := range []string{"usr_contributor", "usr_decider"} {
		go func() {
			<-start
			_, e := s.WriteTicketComponent(ctx, tcInvocation(user), "ticket.component.link", input)
			outcomes <- e
		}()
	}
	close(start)
	success, conflicts := 0, 0
	for range 2 {
		e := <-outcomes
		if e == nil {
			success++
		} else if errors.Is(e, authz.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal(success, conflicts)
	}
	second := component("tc-many")
	for _, user := range []string{"usr_admin", "usr_decider"} {
		input.ComponentID = second
		input.ClientOperationID = user
		r, e := s.WriteTicketComponent(ctx, tcInvocation(user), "ticket.component.link", input)
		if e != nil {
			t.Fatal("role link", e)
		}
		if _, e = s.WriteTicketComponent(ctx, tcInvocation(user), "ticket.component.unlink", goldenpath.TicketComponentInput{TicketID: ticket.ID, LinkID: r.LinkID, ClientOperationID: user, Confirmed: true}); e != nil {
			t.Fatal("role unlink", e)
		}
	}
}

func TestTicketComponentFivePointAtomicRollback(t *testing.T) {
	ctx, pool, core, s, _, component := ticketComponentFixture(t)
	ticket, _ := componentTicket(t, ctx, core, "chn_project", "rollback")
	cid := component("tc-rollback")
	tables := []string{"entity_links", "collaboration_command_receipts", "domain_events", "outbox_deliveries", "activity_items"}
	snapshot := func() []int {
		t.Helper()
		counts := []int{}
		for _, table := range tables {
			var n int
			if e := pool.QueryRow(ctx, "SELECT count(*) FROM radishnexus."+table).Scan(&n); e != nil {
				t.Fatal(e)
			}
			counts = append(counts, n)
		}
		return counts
	}
	for _, table := range tables {
		before := snapshot()
		if _, e := pool.Exec(ctx, `CREATE FUNCTION radishnexus.fail_tc() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected Ticket Component failure'; END $$; CREATE TRIGGER fail_tc BEFORE INSERT ON radishnexus.`+table+` FOR EACH ROW EXECUTE FUNCTION radishnexus.fail_tc()`); e != nil {
			t.Fatal(e)
		}
		_, e := s.WriteTicketComponent(ctx, tcInvocation("usr_contributor"), "ticket.component.link", goldenpath.TicketComponentInput{TicketID: ticket.ID, ComponentID: cid, ClientOperationID: table, Confirmed: true})
		if e == nil || !strings.Contains(e.Error(), "injected Ticket Component failure") {
			t.Fatal("failure hidden", table, e)
		}
		if after := snapshot(); !reflect.DeepEqual(before, after) {
			t.Fatal("partial transaction", table, before, after)
		}
		if _, e = pool.Exec(ctx, `DROP TRIGGER fail_tc ON radishnexus.`+table+`; DROP FUNCTION radishnexus.fail_tc()`); e != nil {
			t.Fatal(e)
		}
	}
}

func TestTicketComponentPermissionSerialization(t *testing.T) {
	for _, change := range []struct {
		name, sql, pattern string
		want               error
	}{
		{"archive", `UPDATE radishnexus.projects SET status='archived' WHERE id='prj_auth'`, "%FROM radishnexus.projects%", authz.ErrForbidden},
		{"role", `UPDATE radishnexus.project_memberships SET role='viewer' WHERE user_id='usr_contributor' AND project_id='prj_auth'`, "%FROM radishnexus.project_memberships%", authz.ErrForbidden},
		{"membership", `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE user_id='usr_contributor' AND workspace_id='wrk_main'`, "%FROM radishnexus.workspace_memberships%", authz.ErrNotFound},
		{"account", `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id='usr_contributor'`, "%FROM radishnexus.user_accounts%", authz.ErrNotFound},
	} {
		for _, writeFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/write-first-%v", change.name, writeFirst), func(t *testing.T) {
				ctx, pool, core, s, _, component := ticketComponentFixture(t)
				ticket, _ := componentTicket(t, ctx, core, "chn_project", "permission")
				cid := component("tc-permission")
				in := goldenpath.TicketComponentInput{TicketID: ticket.ID, ComponentID: cid, ClientOperationID: "permission", Confirmed: true}
				call := func() error {
					_, e := s.WriteTicketComponent(ctx, tcInvocation("usr_contributor"), "ticket.component.link", in)
					return e
				}
				wait := func(pattern string) {
					t.Helper()
					timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					for {
						var waiting bool
						if e := pool.QueryRow(timeout, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE $1)`, pattern).Scan(&waiting); e != nil {
							t.Fatal(e)
						}
						if waiting {
							return
						}
						select {
						case <-timeout.Done():
							t.Fatal("permission lock barrier", timeout.Err())
						case <-time.After(5 * time.Millisecond):
						}
					}
				}
				if !writeFirst {
					tx, e := pool.Begin(ctx)
					if e != nil {
						t.Fatal(e)
					}
					defer tx.Rollback(ctx)
					if _, e = tx.Exec(ctx, change.sql); e != nil {
						t.Fatal(e)
					}
					done := make(chan error, 1)
					go func() { done <- call() }()
					wait(change.pattern)
					if e = tx.Commit(ctx); e != nil {
						t.Fatal(e)
					}
					if e = <-done; !errors.Is(e, change.want) {
						t.Fatal("write crossed permission change", e)
					}
				} else {
					if _, e := pool.Exec(ctx, `CREATE FUNCTION radishnexus.tc_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(34,1); RETURN NEW; END $$; CREATE TRIGGER tc_barrier BEFORE INSERT ON radishnexus.entity_links FOR EACH ROW EXECUTE FUNCTION radishnexus.tc_barrier()`); e != nil {
						t.Fatal(e)
					}
					gate, e := pool.Begin(ctx)
					if e != nil {
						t.Fatal(e)
					}
					defer gate.Rollback(ctx)
					if _, e = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(34,1)`); e != nil {
						t.Fatal(e)
					}
					done, changed := make(chan error, 1), make(chan error, 1)
					go func() { done <- call() }()
					wait("%INSERT INTO radishnexus.entity_links%")
					go func() { _, e := pool.Exec(ctx, change.sql); changed <- e }()
					wait(change.sql)
					if e = gate.Commit(ctx); e != nil {
						t.Fatal(e)
					}
					if e = <-done; e != nil {
						t.Fatal("authorized write failed", e)
					}
					if e = <-changed; e != nil {
						t.Fatal("permission change failed", e)
					}
				}
				if e := call(); !errors.Is(e, change.want) {
					t.Fatal("retry bypassed current permission", e)
				}
				var count int
				if e := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.entity_links WHERE from_type='ticket' AND from_id=$1 AND relation_type='affects'`, ticket.ID).Scan(&count); e != nil {
					t.Fatal(e)
				}
				want := 0
				if writeFirst {
					want = 1
				}
				if count != want {
					t.Fatal("wrong serial outcome", count, want)
				}
			})
		}
	}
}
