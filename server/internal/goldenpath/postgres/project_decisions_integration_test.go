//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
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
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

// This fixture starts from an empty migrated database. The owner, invited users,
// Team, Projects, roles and Channels are created through their official services.
// SQL below only establishes unsupported permission conditions or corrupts data
// for explicitly labelled rejection probes; it never creates Decision facts.
type projectDecisionFixture struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	core      *goldenpath.Service
	cfg       *goldenpath.ConfigurationService
	discovery *goldenpath.DiscoveryService
	auth      *authn.Service
	users     map[string]authz.Principal
	sessions  map[string]authn.Session
	team      string
	next      int
}

type projectDecisionIDs struct{ next int }

func (ids *projectDecisionIDs) NewID(prefix string) (string, error) {
	ids.next++
	return fmt.Sprintf("%sdiscovery_%08d", prefix, ids.next), nil
}

func newProjectDecisionFixture(t *testing.T) *projectDecisionFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("nexus_project_decisions_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Error(err)
		}
		admin.Close(cleanup)
	})
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Migrate(ctx, conn.Conn())
	conn.Release()
	if err != nil {
		t.Fatal(err)
	}
	as := authpostgres.New(pool)
	auth := authn.NewService(as, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	const password = "test only decision discovery password"
	bootstrap, err := auth.Bootstrap(ctx, authn.BootstrapInput{Email: "decision-owner@example.test", DisplayName: "Decision Owner", WorkspaceName: "Decision Discovery", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	ownerSession, err := auth.Login(ctx, authn.LoginInput{Email: "decision-owner@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	owner := authz.Principal{Kind: authz.PrincipalUser, ID: bootstrap.UserID, WorkspaceID: bootstrap.WorkspaceID}
	f := &projectDecisionFixture{ctx: ctx, pool: pool, auth: auth, users: map[string]authz.Principal{"owner": owner}, sessions: map[string]authn.Session{"owner": ownerSession}}
	identity := authn.NewIdentityService(as, auth, "")
	for _, role := range []string{"reader", "contributor", "decider"} {
		invite, err := identity.CreateInvitation(ctx, ownerSession.Token, owner.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		session, err := identity.AcceptInvitation(ctx, authn.AcceptInvitationInput{InvitationToken: invite.Token, Email: "decision-" + role + "@example.test", DisplayName: role, Password: password})
		if err != nil {
			t.Fatal(err)
		}
		f.users[role] = authz.Principal{Kind: authz.PrincipalUser, ID: session.Account.User.ID, WorkspaceID: owner.WorkspaceID}
		f.sessions[role] = session
	}
	store := goldenpostgres.New(pool)
	f.core = goldenpath.NewService(store, &projectDecisionIDs{}, goldenpath.SystemClock{})
	f.cfg = goldenpath.NewConfigurationService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	f.discovery = goldenpath.NewDiscoveryService(store)
	f.team = f.configure(t, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: owner.WorkspaceID, Name: "Decision Team"}).Object.ID
	return f
}
func (f *projectDecisionFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}
func (f *projectDecisionFixture) configure(t *testing.T, in goldenpath.ConfigurationInput) goldenpath.ConfigurationResult {
	t.Helper()
	f.next++
	in.ClientOperationID = fmt.Sprintf("decision-config-%04d", f.next)
	result, err := f.cfg.Configure(f.ctx, invocation(f.users["owner"], "decision-config"), in)
	if err != nil {
		t.Fatalf("%s: %v", in.Kind, err)
	}
	return result
}
func (f *projectDecisionFixture) project(t *testing.T, key, visibility string) (string, string) {
	t.Helper()
	owner := f.users["owner"]
	p := f.configure(t, goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: owner.WorkspaceID, Name: "Decision " + key, Key: key, OwnerTeamID: f.team, InitialAdminUserID: owner.ID, Visibility: visibility}).Object.ID
	for _, role := range []string{"reader", "contributor", "decider"} {
		value := role
		if role == "reader" {
			value = "viewer"
		}
		f.configure(t, goldenpath.ConfigurationInput{Kind: "project.member.set", ScopeID: p, UserID: f.users[role].ID, Role: value})
	}
	ch := f.configure(t, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: p, Name: "Decision discussion", Visibility: "project", MemberUserIDs: []string{}}).Object.ID
	return p, ch
}
func (f *projectDecisionFixture) thread(t *testing.T, channel, key, visibility string) string {
	t.Helper()
	inv := invocation(f.users["owner"], "decision-source-"+key)
	m, err := f.core.CreateMessage(f.ctx, inv, goldenpath.CreateMessageInput{ChannelID: channel, ClientOperationID: key, Body: "SOURCE_SECRET_MESSAGE_" + key})
	if err != nil {
		t.Fatal(err)
	}
	th, err := f.core.StartThreadFromMessage(f.ctx, inv, goldenpath.StartThreadFromMessageInput{ChannelID: channel, MessageID: m.Message.ID, Title: "SOURCE_SECRET_THREAD_" + key, Visibility: visibility})
	if err != nil {
		t.Fatal(err)
	}
	return th.ID
}
func (f *projectDecisionFixture) propose(t *testing.T, thread, key, question string, accepted bool) goldenpath.Decision {
	t.Helper()
	d, err := f.core.CreateDecisionFromThread(f.ctx, invocation(f.users["owner"], "decision-propose-"+key), goldenpath.CreateDecisionInput{ThreadID: thread, ClientOperationID: key, Question: question})
	if err != nil {
		t.Fatal(err)
	}
	if accepted {
		result, err := f.core.AcceptDecision(f.ctx, invocation(f.users["owner"], "decision-accept-"+key), goldenpath.AcceptDecisionInput{DecisionID: d.Decision.ID, ClientOperationID: key, Outcome: "OUTCOME_SECRET_" + key, Rationale: "RATIONALE_SECRET_" + key})
		if err != nil {
			t.Fatal(err)
		}
		return result.Decision
	}
	return d.Decision
}
func (f *projectDecisionFixture) page(t *testing.T, role, project string, limit int, after string) goldenpath.DiscoveryPage {
	t.Helper()
	p, err := f.discovery.ListProjectDecisions(f.ctx, f.users[role], project, goldenpath.DiscoveryPageInput{Limit: limit, AfterID: after})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func (f *projectDecisionFixture) accept(role, id, key string) error {
	_, err := f.core.AcceptDecision(f.ctx, invocation(f.users[role], "decision-permission"), goldenpath.AcceptDecisionInput{DecisionID: id, ClientOperationID: key, Outcome: "Approved", Rationale: "Evidence checked"})
	return err
}
func (f *projectDecisionFixture) handler(t *testing.T) http.Handler {
	t.Helper()
	policy, err := httptransport.NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := httptransport.NewTrustedProxyPolicy("127.0.0.1/32")
	if err != nil {
		t.Fatal(err)
	}
	return httptransport.WithRequestID(httptransport.NewDiscoveryHandler(f.auth, f.discovery, policy, proxy))
}
func (f *projectDecisionFixture) get(t *testing.T, h http.Handler, role, project, query string) *httptest.ResponseRecorder {
	t.Helper()
	path := "https://nexus.example.test/api/v1/workspaces/" + f.users["owner"].WorkspaceID + "/projects/" + project + "/decisions" + query
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if role != "" {
		r.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: f.sessions[role].Token})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Cache-Control") != "private, no-store" || !strings.Contains(w.Header().Get("Vary"), "Cookie") || w.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing private response metadata", w.Header())
	}
	return w
}
func assertDecisionDiscoveryNoSource(t *testing.T, body string) {
	t.Helper()
	for _, marker := range []string{"SOURCE_SECRET_", "OUTCOME_SECRET_", "RATIONALE_SECRET_", "evidence", "rationale", "outcome", "capabilities", "receipt", "hidden_count"} {
		if strings.Contains(body, marker) {
			t.Fatalf("Decision list leaked %q: %s", marker, body)
		}
	}
}

func TestProjectDecisionDiscoveryPermissionsPaginationAndHTTP(t *testing.T) {
	f := newProjectDecisionFixture(t)
	project, channel := f.project(t, "decisions", "workspace")
	h := f.handler(t)
	reader := f.users["reader"]
	owner := f.users["owner"]
	// P01 and D01: no Project role still retains the workspace read baseline.
	viewer := "viewer"
	f.configure(t, goldenpath.ConfigurationInput{Kind: "project.member.remove", ScopeID: project, UserID: reader.ID, ExpectedRole: &viewer})
	if p := f.page(t, "reader", project, 25, ""); len(p.Items) != 0 || p.NextID != "" {
		t.Fatal("empty Project", p)
	}
	if w := f.get(t, h, "reader", project, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal(w.Code, w.Body.String())
	}
	source := f.thread(t, channel, "pages", "project")
	decisions := []goldenpath.Decision{}
	question := "中文 <script>alert('plain text')</script> **问题** " + strings.Repeat("很长的问题", 40)
	for i := 0; i < 27; i++ {
		d := f.propose(t, source, fmt.Sprintf("page-%03d", i), question, i%2 == 0)
		decisions = append(decisions, d)
		if i == 24 || i == 25 {
			p := f.page(t, "reader", project, 25, "")
			if len(p.Items) != 25 || (p.NextID != "") != (i == 25) {
				t.Fatal("25/26 page boundary", i, p)
			}
		}
	}
	ids := make([]string, len(decisions))
	byID := map[string]goldenpath.Decision{}
	for i, d := range decisions {
		ids[i] = d.ID
		byID[d.ID] = d
	}
	sort.Strings(ids)
	for _, limit := range []int{1, 25, 50} {
		got := []string{}
		after := ""
		for pages := 0; ; pages++ {
			if pages > len(ids) {
				t.Fatal("pagination failed to terminate")
			}
			p := f.page(t, "reader", project, limit, after)
			for _, item := range p.Items {
				d, ok := byID[item.Ref.ID]
				if !ok || item.Ref.Type != "decision" || item.Title != d.Question || item.Status != d.Status {
					t.Fatal("projection mismatch", item, d)
				}
				if _, err := f.core.GetNexusView(f.ctx, reader, item.Ref); err != nil {
					t.Fatal("list/detail drift", err)
				}
				got = append(got, item.Ref.ID)
			}
			if p.NextID == "" {
				break
			}
			if len(p.Items) != limit || p.NextID != p.Items[len(p.Items)-1].Ref.ID || p.NextID <= after {
				t.Fatal("invalid page boundary", p)
			}
			after = p.NextID
		}
		if !reflect.DeepEqual(got, ids) {
			t.Fatal("incomplete or duplicate fixed set", limit, got, ids)
		}
	}
	if err := f.accept("reader", decisions[1].ID, "no-project-role"); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("read implied accept", err)
	}
	// D02: same questions in another Project are distinct, never deduplicated or expanded.
	other, otherChannel := f.project(t, "other-decisions", "workspace")
	otherSource := f.thread(t, otherChannel, "other", "project")
	otherDecision := f.propose(t, otherSource, "other", question, true)
	if p := f.page(t, "reader", other, 50, ""); len(p.Items) != 1 || p.Items[0].Ref.ID != otherDecision.ID {
		t.Fatal("governing Project isolation", p)
	}
	if p := f.page(t, "reader", project, 50, ""); len(p.Items) != 27 {
		t.Fatal("other Project expanded result", p)
	}
	// Bootstrap is the only Workspace-creation entry point and is intentionally
	// single-use. This additional Workspace/membership is a foundation permission
	// fixture; its Team, Project, Channel and Decision still use real commands.
	f.exec(t, `INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_decision_other','Other Decision Workspace')`)
	f.exec(t, `INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,role) VALUES('wrk_decision_other',$1,'owner')`, owner.ID)
	foreignFixture := *f
	foreignOwner := owner
	foreignOwner.WorkspaceID = "wrk_decision_other"
	foreignFixture.users = map[string]authz.Principal{"owner": foreignOwner}
	foreignTeam := foreignFixture.configure(t, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: foreignOwner.WorkspaceID, Name: "Other Workspace Team"}).Object.ID
	foreignProject := foreignFixture.configure(t, goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: foreignOwner.WorkspaceID, Name: "Other Workspace Project", Key: "foreign-decisions", OwnerTeamID: foreignTeam, InitialAdminUserID: owner.ID, Visibility: "workspace"}).Object.ID
	foreignChannel := foreignFixture.configure(t, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: foreignProject, Name: "Other Workspace Channel", Visibility: "project", MemberUserIDs: []string{}}).Object.ID
	foreignSource := foreignFixture.thread(t, foreignChannel, "foreign-source", "project")
	foreignDecision := foreignFixture.propose(t, foreignSource, "foreign-question", question, true)
	if p := foreignFixture.page(t, "owner", foreignProject, 25, ""); len(p.Items) != 1 || p.Items[0].Ref.ID != foreignDecision.ID {
		t.Fatal("foreign Workspace command/read fixture", p)
	}
	if w := f.get(t, h, "reader", foreignProject, ""); w.Code != 404 || strings.Contains(w.Body.String(), foreignDecision.ID) {
		t.Fatal("cross Workspace Project disclosure", w.Code, w.Body.String())
	}
	if _, err := f.core.GetNexusView(f.ctx, reader, entityref.Ref{Type: "decision", ID: foreignDecision.ID}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("cross Workspace Decision disclosure", err)
	}
	// D03: syntactically valid absent boundaries require no object lookup.
	boundary := ids[10] + "x"
	p := f.page(t, "reader", project, 50, boundary)
	want := []string{}
	for _, id := range ids {
		if id > boundary {
			want = append(want, id)
		}
	}
	got := []string{}
	for _, item := range p.Items {
		got = append(got, item.Ref.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("absent boundary mismatch", got, want)
	}
	first := f.page(t, "reader", project, 25, "")
	// A second command service deliberately generates an ID before the consumed
	// boundary, modelling an independent writer without SQL-inserting facts.
	lateCore := goldenpath.NewService(goldenpostgres.New(f.pool), &fixedIDs{values: []string{"dec_000_before_boundary", "lnk_before_boundary", "evt_before_boundary"}}, goldenpath.SystemClock{})
	inserted, err := lateCore.CreateDecisionFromThread(f.ctx, invocation(owner, "decision-independent-writer"), goldenpath.CreateDecisionInput{ThreadID: source, ClientOperationID: "before-boundary", Question: "Inserted before cursor"})
	if err != nil {
		t.Fatal(err)
	}
	if inserted.Decision.ID >= first.NextID {
		t.Fatal("fixture must insert before consumed boundary")
	}
	next := f.page(t, "reader", project, 25, first.NextID)
	for _, item := range next.Items {
		if item.Ref.ID <= first.NextID {
			t.Fatal("page crossed keyset boundary", item)
		}
	}
	if fresh := f.page(t, "reader", project, 50, ""); len(fresh.Items) != 28 || fresh.Items[0].Ref.ID != inserted.Decision.ID {
		t.Fatal("refresh missed earlier insertion", fresh)
	}
	// Accept an item after the first page. The next page sees current state and
	// keeps the same ID position; there is no cross-request state snapshot.
	last := decisions[25]
	if err := f.accept("owner", last.ID, "between-pages-accept"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range f.page(t, "reader", project, 25, first.NextID).Items {
		if item.Ref.ID == last.ID {
			found = true
			if item.Status != "accepted" {
				t.Fatal("stale status", item)
			}
		}
	}
	if !found {
		t.Fatal("updated last-page Decision missing")
	}
	// Real Session-backed transport only exposes ref/title/status.
	w := f.get(t, h, "reader", project, "?limit=50")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	assertDecisionDiscoveryNoSource(t, w.Body.String())
	var wire struct {
		Data struct {
			Items      []map[string]json.RawMessage `json:"items"`
			NextCursor *string                      `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Data.Items) != 28 {
		t.Fatal(w.Body.String())
	}
	for _, item := range wire.Data.Items {
		if len(item) != 3 || item["ref"] == nil || item["title"] == nil || item["status"] == nil {
			t.Fatal("expanded public DTO", item)
		}
	}
	// Exercise opaque transport cursors against the real Store as well as the
	// service-level ID boundary, including exact full-set order and no repeats.
	transportIDs := []string{}
	query := "?limit=25"
	for pages := 0; ; pages++ {
		if pages > 2 {
			t.Fatal("HTTP pagination failed to terminate")
		}
		response := f.get(t, h, "reader", project, query)
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		var page struct {
			Data struct {
				Items []struct {
					Ref entityref.Ref `json:"ref"`
				} `json:"items"`
				NextCursor *string `json:"next_cursor"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Data.Items {
			transportIDs = append(transportIDs, item.Ref.ID)
		}
		if page.Data.NextCursor == nil {
			break
		}
		query = "?limit=25&after=" + url.QueryEscape(*page.Data.NextCursor)
	}
	expectedIDs := append([]string{inserted.Decision.ID}, ids...)
	if !reflect.DeepEqual(transportIDs, expectedIDs) {
		t.Fatal("HTTP fixed-set pagination mismatch", transportIDs, expectedIDs)
	}
	deciderRole := "decider"
	f.configure(t, goldenpath.ConfigurationInput{Kind: "project.member.remove", ScopeID: project, UserID: f.users["decider"].ID, ExpectedRole: &deciderRole})
	if p := f.page(t, "decider", project, 50, ""); len(p.Items) != 28 {
		t.Fatal("workspace baseline lost on write-role removal", p)
	}
	if err := f.accept("decider", decisions[1].ID, "removed-write-role"); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("removed role retained acceptance", err)
	}
	// P02: all restricted Project roles read, while write authority stays distinct.
	restricted, restrictedChannel := f.project(t, "restricted-decisions", "restricted")
	restrictedSource := f.thread(t, restrictedChannel, "restricted-project", "project")
	draft := f.propose(t, restrictedSource, "restricted-draft", "Restricted Project question", false)
	for _, role := range []string{"reader", "contributor", "decider", "owner"} {
		if p := f.page(t, role, restricted, 25, ""); len(p.Items) != 1 {
			t.Fatal(role, p)
		}
	}
	for _, role := range []string{"reader", "contributor"} {
		if err := f.accept(role, draft.ID, "restricted-deny-"+role); !errors.Is(err, authz.ErrForbidden) {
			t.Fatal(role, err)
		}
	}
	if err := f.accept("decider", draft.ID, "restricted-decider"); err != nil {
		t.Fatal(err)
	}
	adminDraft := f.propose(t, restrictedSource, "restricted-admin", "Admin acceptance", false)
	if err := f.accept("owner", adminDraft.ID, "restricted-admin"); err != nil {
		t.Fatal(err)
	}
	// P06: unreadable source Thread does not conceal the readable Decision.
	privateThread := f.thread(t, restrictedChannel, "private-thread", "restricted")
	privateDraft := f.propose(t, privateThread, "private-draft", "Public Decision question distinct from source", false)
	if _, err := f.core.GetNexusView(f.ctx, f.users["decider"], entityref.Ref{Type: "thread", ID: privateThread}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("private source visible", err)
	}
	if err := f.accept("decider", privateDraft.ID, "private-evidence-denied"); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("decider bypassed evidence", err)
	}
	privateChannel := f.configure(t, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: restricted, Name: "SOURCE_SECRET_CHANNEL", Visibility: "restricted", MemberUserIDs: []string{owner.ID, f.users["decider"].ID}}).Object.ID
	originThread := f.thread(t, privateChannel, "private-origin", "restricted")
	originDraft := f.propose(t, originThread, "origin-draft", "Origin-isolated question", false)
	// Preserve a Thread grant while restricting origin Channel access to isolate
	// the origin check independently from the Thread membership check.
	f.exec(t, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id) VALUES($1,$2,$3)`, owner.WorkspaceID, originThread, f.users["decider"].ID)
	f.configure(t, goldenpath.ConfigurationInput{Kind: "channel.member.remove", ScopeID: privateChannel, UserID: f.users["decider"].ID, ExpectedMember: true})
	f.exec(t, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id) VALUES($1,$2,$3)`, owner.WorkspaceID, originThread, f.users["decider"].ID)
	if err := f.accept("decider", originDraft.ID, "origin-evidence-denied"); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("origin Channel bypass", err)
	}
	for _, id := range []string{privateDraft.ID, originDraft.ID} {
		view, err := f.core.GetNexusView(f.ctx, f.users["decider"], entityref.Ref{Type: "decision", ID: id})
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(view)
		if strings.Contains(string(encoded), "SOURCE_SECRET_") || strings.Contains(string(encoded), privateThread) || strings.Contains(string(encoded), originThread) {
			t.Fatal("detail source leaked", string(encoded))
		}
	}
	w = f.get(t, h, "decider", restricted, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), privateDraft.ID) || !strings.Contains(w.Body.String(), originDraft.ID) {
		t.Fatal("source permission filtered Decision", w.Code, w.Body.String())
	}
	assertDecisionDiscoveryNoSource(t, w.Body.String())
	// Admin is also subject to evidence, even though Project visibility permits reading.
	f.exec(t, `DELETE FROM radishnexus.thread_memberships WHERE workspace_id=$1 AND thread_id=$2 AND user_id=$3`, owner.WorkspaceID, privateThread, owner.ID)
	if err := f.accept("owner", privateDraft.ID, "admin-private-evidence-denied"); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("admin bypassed evidence", err)
	}
	// P05: archived Projects stay readable, but new acceptance remains a conflict.
	f.exec(t, `UPDATE radishnexus.projects SET status='archived' WHERE id=$1`, restricted)
	if p := f.page(t, "reader", restricted, 25, ""); len(p.Items) != 4 {
		t.Fatal("archived read", p)
	}
	if _, err := f.core.GetNexusView(f.ctx, reader, entityref.Ref{Type: "decision", ID: privateDraft.ID}); err != nil {
		t.Fatal("archived detail", err)
	}
	context, err := f.cfg.ReadConfiguration(f.ctx, reader, "project", restricted)
	if err != nil || context.Status != "archived" {
		t.Fatal("archived context", context, err)
	}
	if err := f.accept("owner", originDraft.ID, "archived-accept-denied"); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("archived accept", err)
	}
	f.exec(t, `UPDATE radishnexus.projects SET status='active' WHERE id=$1`, restricted)
	// P03/P07/D04: a remembered cursor and ownership grant no restricted access.
	restrictedFirst := f.page(t, "reader", restricted, 1, "")
	f.configure(t, goldenpath.ConfigurationInput{Kind: "project.member.remove", ScopeID: restricted, UserID: reader.ID, ExpectedRole: &viewer})
	if _, err := f.discovery.ListProjectDecisions(f.ctx, reader, restricted, goldenpath.DiscoveryPageInput{Limit: 1, AfterID: restrictedFirst.NextID}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("cursor survived revocation", err)
	}
	if _, err := f.core.GetNexusView(f.ctx, reader, entityref.Ref{Type: "decision", ID: draft.ID}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("revoked detail", err)
	}
	for _, id := range []string{restricted, "prj_missing"} {
		w := f.get(t, h, "reader", id, "")
		if w.Code != 404 || strings.Contains(w.Body.String(), draft.ID) || strings.Contains(w.Body.String(), "Restricted Project question") {
			t.Fatal("not-found disclosure", w.Code, w.Body.String())
		}
	}
	// There is no public admin-removal command; direct membership mutation here
	// is only an owner-is-not-an-override permission condition, never a write path.
	f.exec(t, `DELETE FROM radishnexus.project_memberships WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3`, owner.WorkspaceID, restricted, owner.ID)
	if w := f.get(t, h, "owner", restricted, ""); w.Code != 404 {
		t.Fatal("Workspace owner bypass", w.Code, w.Body.String())
	}
	if _, err := f.core.GetNexusView(f.ctx, owner, entityref.Ref{Type: "decision", ID: draft.ID}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("Workspace owner detail bypass", err)
	}
	f.exec(t, `INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role) VALUES($1,$2,$3,'admin')`, owner.WorkspaceID, restricted, owner.ID)
	f.configure(t, goldenpath.ConfigurationInput{Kind: "project.member.set", ScopeID: restricted, UserID: reader.ID, Role: "viewer"})
	if p := f.page(t, "reader", restricted, 25, ""); len(p.Items) != 4 {
		t.Fatal("restored authority did not reread", p)
	}
	foreign := reader
	foreign.WorkspaceID = foreignOwner.WorkspaceID
	if _, err := f.discovery.ListProjectDecisions(f.ctx, foreign, project, goldenpath.DiscoveryPageInput{Limit: 25}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("cross Workspace Project", err)
	}
	// P04: membership loss is 404, whereas invalid identity is 401.
	f.exec(t, `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE workspace_id=$1 AND user_id=$2`, owner.WorkspaceID, reader.ID)
	if w := f.get(t, h, "reader", project, ""); w.Code != 404 {
		t.Fatal("suspended member", w.Code, w.Body.String())
	}
	f.exec(t, `UPDATE radishnexus.workspace_memberships SET status='active' WHERE workspace_id=$1 AND user_id=$2`, owner.WorkspaceID, reader.ID)
	f.exec(t, `DELETE FROM radishnexus.project_memberships WHERE workspace_id=$1 AND user_id=$2`, owner.WorkspaceID, reader.ID)
	f.exec(t, `DELETE FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, owner.WorkspaceID, reader.ID)
	if w := f.get(t, h, "reader", project, ""); w.Code != 404 {
		t.Fatal("removed member", w.Code, w.Body.String())
	}
	f.exec(t, `INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id) VALUES($1,$2)`, owner.WorkspaceID, reader.ID)
	if p := f.page(t, "reader", project, 50, ""); len(p.Items) != 28 {
		t.Fatal("public baseline after role removal", p)
	}
	for _, status := range []string{"disabled", "active"} {
		f.exec(t, `UPDATE radishnexus.user_accounts SET status=$1 WHERE user_id=$2`, status, reader.ID)
		want := 200
		if status == "disabled" {
			want = 401
		}
		if w := f.get(t, h, "reader", project, ""); w.Code != want {
			t.Fatal("account status", status, w.Code, w.Body.String())
		}
	}
	if w := f.get(t, h, "", project, ""); w.Code != 401 {
		t.Fatal("missing Session", w.Code)
	}
	// Session identity and expiry are immutable. Advance only the authentication
	// service's clock past this formally issued Session's actual expiry, retaining
	// the same PostgreSQL Store and HTTP path instead of mutating its record.
	expiresAt := f.sessions["reader"].Account.ExpiresAt
	if expiresAt.IsZero() {
		t.Fatal("formally issued Session lacks expiry")
	}
	expiredFixture := *f
	expiredFixture.auth = authn.NewService(authpostgres.New(f.pool), nil, nil, fixedClock{now: expiresAt.Add(time.Second)})
	if w := f.get(t, expiredFixture.handler(t), "reader", project, ""); w.Code != 401 {
		t.Fatal("expired Session", w.Code)
	}
	if w := f.get(t, h, "reader", project, ""); w.Code != 200 {
		t.Fatal("expiry probe mutated the issued Session", w.Code, w.Body.String())
	}
	if err := f.auth.RevokeSession(f.ctx, f.sessions["contributor"].Token, f.sessions["contributor"].CSRFToken); err != nil {
		t.Fatal(err)
	}
	if w := f.get(t, h, "contributor", project, ""); w.Code != 401 {
		t.Fatal("revoked Session", w.Code)
	}
	// D06: reserved statuses are corruption probes, never supported transitions.
	// The corrupt record is outside page one: no global pre-scan is permitted.
	corrupt := decisions[26]
	for _, status := range []string{"rejected", "superseded"} {
		// Start from a formally accepted record to retain all reserved-state fields.
		// superseded additionally requires an active replacement relation. This
		// explicit anomaly fixture respects that database invariant; it does not
		// exercise or claim a supported public supersession command.
		if status == "superseded" {
			f.exec(t, `INSERT INTO radishnexus.entity_links
				(id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id)
				VALUES('lnk_decision_reserved_probe',$1,'decision',$2,'supersedes','decision',$3,'asserted','user','user',$4)`, owner.WorkspaceID, decisions[0].ID, corrupt.ID, owner.ID)
		}
		f.exec(t, `UPDATE radishnexus.decisions SET status=$1,rejection_reason='anomaly probe' WHERE id=$2`, status, corrupt.ID)
		if w := f.get(t, h, "owner", project, "?limit=1"); w.Code != 200 {
			t.Fatal("unreached corruption rejected first page", w.Code, w.Body.String())
		}
		if w := f.get(t, h, "owner", project, "?limit=50"); w.Code != 500 || strings.Contains(w.Body.String(), "\"items\"") || strings.Contains(w.Body.String(), corrupt.ID) {
			t.Fatal("invalid status partially exposed", status, w.Code, w.Body.String())
		}
		f.exec(t, `UPDATE radishnexus.decisions SET status='accepted',rejection_reason=NULL WHERE id=$1`, corrupt.ID)
		if status == "superseded" {
			f.exec(t, `UPDATE radishnexus.entity_links SET state='removed',removed_by_kind='user',removed_by_id=$1,
				removed_at=clock_timestamp(),updated_at=clock_timestamp(),removal_reason='reserved-state probe complete'
				WHERE id='lnk_decision_reserved_probe'`, owner.ID)
		}
	}
	f.exec(t, `UPDATE radishnexus.decisions SET question='' WHERE id=$1`, ids[0])
	if w := f.get(t, h, "owner", project, "?limit=50"); w.Code != 500 || strings.Contains(w.Body.String(), "\"items\"") {
		t.Fatal("invalid question partially exposed", w.Code, w.Body.String())
	}
	f.exec(t, `UPDATE radishnexus.decisions SET question=$1 WHERE id=$2`, question, ids[0])
	// R04: compare full serialized row sets, catching mutation as well as inserts.
	before := f.businessSnapshot(t)
	for range 3 {
		if w := f.get(t, h, "owner", project, "?limit=1"); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		after := ""
		for {
			page := f.page(t, "owner", project, 25, after)
			if page.NextID == "" {
				break
			}
			after = page.NextID
		}
	}
	if after := f.businessSnapshot(t); !reflect.DeepEqual(before, after) {
		t.Fatal("discovery mutated business facts", before, after)
	}
}

func (f *projectDecisionFixture) businessSnapshot(t *testing.T) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	for _, table := range []string{"decisions", "entity_links", "domain_events", "outbox_deliveries", "activity_items", "identity_audit", "workspace_configuration_audit", "workspace_configuration_receipts", "collaboration_command_receipts"} {
		var encoded string
		query := `SELECT COALESCE(jsonb_agg(row_data ORDER BY row_data::text),'[]'::jsonb)::text FROM (SELECT to_jsonb(t) AS row_data FROM radishnexus.` + pgx.Identifier{table}.Sanitize() + ` t) records`
		if err := f.pool.QueryRow(f.ctx, query).Scan(&encoded); err != nil {
			t.Fatal(err)
		}
		snapshot[table] = encoded
	}
	return snapshot
}

func TestProjectDecisionDiscoveryScaleAndConstantQueries(t *testing.T) {
	f := newProjectDecisionFixture(t)
	projects := []string{}
	targetIDs := []string{}
	const targetCount = 27
	const otherCount = 600
	for index, count := range []int{targetCount, otherCount, otherCount} {
		project, channel := f.project(t, fmt.Sprintf("scale-decisions-%d", index), "workspace")
		projects = append(projects, project)
		thread := f.thread(t, channel, fmt.Sprintf("scale-%d", index), "project")
		for i := 0; i < count; i++ {
			d := f.propose(t, thread, fmt.Sprintf("scale-%d-%04d", index, i), "Same question in every Project", i%2 == 0)
			if index == 0 {
				targetIDs = append(targetIDs, d.ID)
			}
		}
	}
	sort.Strings(targetIDs)
	var total, target, accepted int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*),count(*) FILTER (WHERE governing_project_id=$1),count(*) FILTER (WHERE status='accepted') FROM radishnexus.decisions`, projects[0]).Scan(&total, &target, &accepted); err != nil {
		t.Fatal(err)
	}
	if total != targetCount+2*otherCount || target != targetCount || accepted == 0 || accepted == total {
		t.Fatal("representative Decision dataset drift", total, target, accepted)
	}
	f.exec(t, `ANALYZE radishnexus.decisions`)
	counter := &relationQueryCounter{}
	config := f.pool.Config()
	config.ConnConfig.Tracer = counter
	measured, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	if err = measured.Ping(f.ctx); err != nil {
		t.Fatal(err)
	}
	service := goldenpath.NewDiscoveryService(goldenpostgres.New(measured))
	var baseline int64
	for _, probe := range []struct {
		label string
		limit int
		after string
		want  int
	}{
		{"first-small", 1, "", 1},
		{"first-default", 25, "", 25},
		{"tail-default", 25, targetIDs[24], 2},
		{"first-max", 50, "", 27},
		{"empty-tail", 50, targetIDs[26], 0},
	} {
		var plan []byte
		if err := f.pool.QueryRow(f.ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT id,question,status FROM radishnexus.decisions WHERE workspace_id=$1 AND governing_project_id=$2 AND id>$3 ORDER BY id LIMIT $4`, f.users["owner"].WorkspaceID, projects[0], probe.after, probe.limit+1).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		if !json.Valid(plan) {
			t.Fatal("invalid EXPLAIN JSON", string(plan))
		}
		t.Logf("Decision EXPLAIN %s: total=%d target=%d non-target=%d Projects=%d limit=%d after=%q plan=%s", probe.label, targetCount+2*otherCount, targetCount, 2*otherCount, len(projects), probe.limit, probe.after, plan)
		// Five fixed admission statements (timeout, schema gate, Workspace gate, account and membership).
		counter.queries.Store(0)
		start := time.Now()
		page, err := service.ListProjectDecisions(f.ctx, f.users["reader"], projects[0], goldenpath.DiscoveryPageInput{Limit: probe.limit, AfterID: probe.after})
		elapsed := time.Since(start)
		calls := counter.queries.Load()
		if err != nil || len(page.Items) != probe.want || calls > 11 {
			t.Fatal(probe.label, page, err, calls)
		}
		if baseline == 0 {
			baseline = calls
		} else if calls != baseline {
			t.Fatal("SQL calls changed with items/page size", probe.label, calls, baseline)
		}
		for _, item := range page.Items {
			if item.Ref.ID <= probe.after || !containsDecisionID(targetIDs, item.Ref.ID) {
				t.Fatal("scale scope/boundary mismatch", item)
			}
		}
		t.Logf("Decision measured %s: %d/%d target objects, %d results, %d SQL calls, %s", probe.label, targetCount, targetCount+2*otherCount, len(page.Items), calls, elapsed)
	}
}
func containsDecisionID(ids []string, id string) bool {
	index := sort.SearchStrings(ids, id)
	return index < len(ids) && ids[index] == id
}
