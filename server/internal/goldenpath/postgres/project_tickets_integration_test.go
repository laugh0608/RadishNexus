//go:build integration

package postgres_test

import (
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

func TestProjectTicketDiscoveryPermissionsPaginationAndScale(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	store := goldenpostgres.New(pool)
	service := goldenpath.NewDiscoveryService(store)
	reader := principal("usr_reader")
	list := func(limit int, after string) goldenpath.DiscoveryPage {
		t.Helper()
		p, e := service.ListProjectTickets(ctx, reader, "prj_auth", goldenpath.DiscoveryPageInput{Limit: limit, AfterID: after})
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	if p := list(25, ""); len(p.Items) != 0 || p.NextID != "" {
		t.Fatal(p)
	}
	first, thread := componentTicket(t, ctx, core, "chn_restricted", "discovery-first")
	view, e := core.GetNexusView(ctx, reader, entityref.Ref{Type: "ticket", ID: first.ID})
	if e != nil {
		t.Fatal(e)
	}
	decision := view.Relations[0].Target.ID
	if _, e := core.GetNexusView(ctx, reader, entityref.Ref{Type: "thread", ID: thread}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("private evidence readable", e)
	}
	create := func(source, key string) string {
		t.Helper()
		r, e := core.CreateTicketFromDecision(ctx, tcInvocation("usr_decider"), goldenpath.CreateTicketInput{DecisionID: source, ClientOperationID: key, Title: "Work " + key})
		if e != nil {
			t.Fatal(e)
		}
		return r.Ticket.ID
	}
	ids := []string{first.ID}
	for i := 1; i < 25; i++ {
		ids = append(ids, create(decision, fmt.Sprintf("page-%03d", i)))
	}
	if p := list(25, ""); len(p.Items) != 25 || p.NextID != "" {
		t.Fatal("exact page", p)
	}
	ids = append(ids, create(decision, "page-025"))
	sort.Strings(ids)
	for _, limit := range []int{1, 25, 50} {
		got := []string{}
		after := ""
		for {
			p := list(limit, after)
			for _, item := range p.Items {
				if item.Status != "open" || item.Ref.Type != "ticket" {
					t.Fatal(item)
				}
				got = append(got, item.Ref.ID)
				if _, e := core.GetNexusView(ctx, reader, item.Ref); e != nil {
					t.Fatal("list/detail drift", e)
				}
			}
			if p.NextID == "" {
				break
			}
			after = p.NextID
		}
		if !reflect.DeepEqual(ids, got) {
			t.Fatal("pagination mismatch", limit, got, ids)
		}
	}
	boundary := ids[10] + "x" // A syntactically valid boundary that is not an object.
	for _, item := range list(50, boundary).Items {
		if item.Ref.ID <= boundary {
			t.Fatal("inclusive boundary", item)
		}
	}
	p := list(25, "")
	newID := create(decision, "between-pages")
	next := list(25, p.NextID)
	for _, item := range next.Items {
		if item.Ref.ID <= p.NextID {
			t.Fatal("page repeated", item)
		}
	}
	if newID > p.NextID && (len(next.Items) != 2) {
		t.Fatal("new later object missing", next)
	}
	if len(list(50, "").Items) != 27 {
		t.Fatal("refresh missed insertion")
	}

	// Governing Project read access is independent of write role and evidence.
	for _, id := range []string{"usr_reader", "usr_contributor", "usr_decider", "usr_admin"} {
		if _, e := service.ListProjectTickets(ctx, principal(id), "prj_auth", goldenpath.DiscoveryPageInput{Limit: 25}); e != nil {
			t.Fatal(id, e)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`DELETE FROM radishnexus.project_memberships WHERE workspace_id='wrk_main' AND project_id='prj_auth' AND user_id='usr_reader'`)
	if len(list(50, "").Items) != 27 {
		t.Fatal("public read baseline lost")
	}
	exec(`UPDATE radishnexus.projects SET visibility='restricted' WHERE id='prj_auth'`)
	if _, e := service.ListProjectTickets(ctx, reader, "prj_auth", goldenpath.DiscoveryPageInput{Limit: 25, AfterID: p.NextID}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("revoked cursor granted access", e)
	}
	// Owner is not an override for an unjoined restricted Project.
	exec(`DELETE FROM radishnexus.project_memberships WHERE workspace_id='wrk_main' AND project_id='prj_auth' AND user_id='usr_admin'`)
	if _, e := service.ListProjectTickets(ctx, principal("usr_admin"), "prj_auth", goldenpath.DiscoveryPageInput{Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("owner bypass", e)
	}
	exec(`INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role) VALUES('wrk_main','prj_auth','usr_reader','viewer')`)
	if len(list(50, "").Items) != 27 {
		t.Fatal("restored read missing")
	}
	exec(`UPDATE radishnexus.projects SET status='archived' WHERE id='prj_auth'`)
	if len(list(50, "").Items) != 27 {
		t.Fatal("archived read missing")
	}
	exec(`UPDATE radishnexus.projects SET status='active' WHERE id='prj_auth'`)
	for _, project := range []string{"prj_other", "prj_missing"} {
		if _, e := service.ListProjectTickets(ctx, reader, project, goldenpath.DiscoveryPageInput{Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
			t.Fatal(project, e)
		}
	}
	exec(`UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE workspace_id='wrk_main' AND user_id='usr_reader'`)
	if _, e := service.ListProjectTickets(ctx, reader, "prj_auth", goldenpath.DiscoveryPageInput{Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("suspended read", e)
	}
	exec(`UPDATE radishnexus.workspace_memberships SET status='active' WHERE workspace_id='wrk_main' AND user_id='usr_reader'`)

	// Real Session-backed HTTP, with current membership/Session revalidation.
	token := deploymentHTTPToken(73)
	digest := sha256.Sum256([]byte(token))
	csrf := sha256.Sum256([]byte(deploymentHTTPToken(74)))
	now := time.Now()
	exec(`INSERT INTO radishnexus.user_sessions(id,user_id,token_digest,csrf_token_digest,created_at,expires_at) VALUES('ses_ticket_discovery','usr_reader',$1,$2,$3,$4)`, digest[:], csrf[:], now, now.Add(authn.SessionLifetime))
	auth := authn.NewService(authpostgres.New(pool), nil, nil, fixedClock{now: now})
	policy, _ := httptransport.NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := httptransport.NewTrustedProxyPolicy("127.0.0.1/32")
	handler := httptransport.WithRequestID(httptransport.NewDiscoveryHandler(auth, service, policy, proxy))
	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://nexus.example.test/api/v1/workspaces/wrk_main/projects/prj_auth/tickets", nil)
		r.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := get()
	if w.Code != 200 || strings.Contains(w.Body.String(), thread) || strings.Contains(w.Body.String(), decision) {
		t.Fatal(w.Code, w.Body.String())
	}
	// Deliberate drift is a failure probe, never a supported status transition.
	exec(`UPDATE radishnexus.tickets SET title='' WHERE id=$1`, ids[0])
	if w := get(); w.Code != 500 || strings.Contains(w.Body.String(), "Work") {
		t.Fatal("drift partially exposed", w.Code, w.Body.String())
	}
	exec(`UPDATE radishnexus.tickets SET title='Work restored' WHERE id=$1`, ids[0])
	exec(`DELETE FROM radishnexus.project_memberships WHERE workspace_id='wrk_main' AND user_id='usr_reader'; DELETE FROM radishnexus.workspace_memberships WHERE workspace_id='wrk_main' AND user_id='usr_reader'`)
	if w := get(); w.Code != 404 {
		t.Fatal("removed member", w.Code)
	}
	exec(`INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id) VALUES('wrk_main','usr_reader')`)
	exec(`INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role) VALUES('wrk_main','prj_auth','usr_reader','viewer')`)
	exec(`UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id='usr_reader'`)
	if w := get(); w.Code != 401 {
		t.Fatal("disabled account", w.Code)
	}
	exec(`UPDATE radishnexus.user_accounts SET status='active' WHERE user_id='usr_reader'`)
	// Session expiry is independent of Project authority.
	exec(`UPDATE radishnexus.user_sessions SET revoked_at=$1 WHERE id='ses_ticket_discovery'`, now)
	if w := get(); w.Code != 401 {
		t.Fatal("revoked Session", w.Code)
	}

	// Representative mixed-project data: create facts using the normal command.
	exec(`INSERT INTO radishnexus.projects(id,workspace_id,key,name,owner_team_id,visibility,status,created_by_kind) VALUES('prj_scale','wrk_main','SCALE','Scale','tem_main','workspace','active','system');
	INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role) VALUES('wrk_main','prj_scale','usr_decider','decider');
	INSERT INTO radishnexus.channels(id,workspace_id,governing_project_id,name,visibility,status,created_by_kind) VALUES('chn_scale','wrk_main','prj_scale','Scale','project','active','system')`)
	other, _ := componentTicket(t, ctx, core, "chn_scale", "other-project")
	otherView, e := core.GetNexusView(ctx, principal("usr_decider"), entityref.Ref{Type: "ticket", ID: other.ID})
	if e != nil {
		t.Fatal(e)
	}
	for i := 27; i < 200; i++ {
		create(decision, fmt.Sprintf("scale-main-%04d", i))
	}
	for i := 1; i < 1000; i++ {
		create(otherView.Relations[0].Target.ID, fmt.Sprintf("scale-other-%04d", i))
	}
	exec(`ANALYZE radishnexus.tickets`)
	var plan []byte
	if e := pool.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT id,title,status FROM radishnexus.tickets WHERE workspace_id=$1 AND governing_project_id=$2 AND id>$3 ORDER BY id LIMIT 26`, "wrk_main", "prj_auth", "").Scan(&plan); e != nil {
		t.Fatal(e)
	}
	var formatted any
	if e := json.Unmarshal(plan, &formatted); e != nil {
		t.Fatal(e)
	}
	t.Logf("Ticket query plan (1200 Tickets, 2 Projects): %s", plan)
	counter := &relationQueryCounter{}
	config := pool.Config()
	config.ConnConfig.Tracer = counter
	measured, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer measured.Close()
	if e := measured.Ping(ctx); e != nil {
		t.Fatal(e)
	}
	counter.queries.Store(0)
	start := time.Now()
	page, e := goldenpath.NewDiscoveryService(goldenpostgres.New(measured)).ListProjectTickets(ctx, reader, "prj_auth", goldenpath.DiscoveryPageInput{Limit: 25})
	if e != nil || len(page.Items) != 25 || counter.queries.Load() > 6 {
		t.Fatal(page, e, counter.queries.Load())
	}
	t.Logf("Ticket discovery scale: 1200 Tickets, 200 in target Project, 25 results, %d SQL calls, %s", counter.queries.Load(), time.Since(start))
}
