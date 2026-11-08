//go:build integration

package postgres_test

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

func TestMyTicketsCurrentAssignmentAndAccess(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO radishnexus.projects(id,workspace_id,key,name,owner_team_id,visibility,status,created_by_kind)
		VALUES('prj_private','wrk_main','PRIVATE','Restricted project','tem_main','restricted','active','system');
		INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role)
		VALUES('wrk_main','prj_private','usr_decider','decider'), ('wrk_main','prj_private','usr_contributor','contributor');
		INSERT INTO radishnexus.channels(id,workspace_id,governing_project_id,name,visibility,status,created_by_kind)
		VALUES('chn_private','wrk_main','prj_private','Restricted channel','project','active','system')`)
	open, _ := componentTicket(t, ctx, core, "chn_project", "my-open")
	restricted, source := componentTicket(t, ctx, core, "chn_private", "my-restricted")
	_, _ = componentTicket(t, ctx, core, "chn_project", "unassigned")
	closed, _ := componentTicket(t, ctx, core, "chn_project", "my-closed")
	store := goldenpostgres.New(pool)
	assignment := goldenpath.NewTicketAssignmentService(store, goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_decider")
	assign := func(ticket, target, operation string) {
		t.Helper()
		if _, err := assignment.SetTicketAssignee(ctx, inv, assignmentInput(t, ctx, assignment, inv, ticket, target, operation)); err != nil {
			t.Fatal(err)
		}
	}
	for _, ticket := range []string{open.ID, restricted.ID, closed.ID} {
		assign(ticket, "usr_contributor", "my-assign")
	}
	status := goldenpath.NewTicketStatusService(store, goldenpath.CryptoIDGenerator{})
	for _, change := range []struct{ ticket, to string }{{restricted.ID, "in-progress"}, {closed.ID, "canceled"}} {
		if _, err := status.ChangeTicketStatus(ctx, inv, goldenpath.TicketStatusInput{
			TicketID: change.ticket, ToStatus: change.to, BaseStatusRevision: 1, ClientOperationID: "my-status", Confirmed: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now()
	tokens := []string{deploymentHTTPToken(73), deploymentHTTPToken(74)}
	csrf := sha256.Sum256([]byte(deploymentHTTPToken(75)))
	for i, user := range []string{"usr_contributor", "usr_decider"} {
		digest := sha256.Sum256([]byte(tokens[i]))
		exec(`INSERT INTO radishnexus.user_sessions(id,user_id,token_digest,csrf_token_digest,created_at,expires_at)
			VALUES($1,$2,$3,$4,$5,$6)`, "ses_my_"+user, user, digest[:], csrf[:], now, now.Add(authn.SessionLifetime))
	}
	auth := authn.NewService(authpostgres.New(pool), nil, nil, fixedClock{now: now})
	policy, _ := httptransport.NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := httptransport.NewTrustedProxyPolicy("127.0.0.1/32")
	handler := httptransport.WithRequestID(httptransport.NewDiscoveryHandler(auth, goldenpath.NewDiscoveryService(store), policy, proxy))
	get := func(workspace, suffix, token string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "https://nexus.example.test/api/v1/workspaces/"+workspace+suffix, nil)
		req.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != want || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(w.Code, w.Header(), w.Body.String())
		}
		return w
	}
	list := func(path string, expected ...string) string {
		t.Helper()
		w := get("wrk_main", path, tokens[0], 200)
		var response struct {
			Data struct {
				Items []struct {
					Ref      struct{ ID string }
					Project  goldenpath.DiscoveryProject
					Status   string
					Priority string
				}
				NextCursor string `json:"next_cursor"`
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, item := range response.Data.Items {
			ids = append(ids, item.Ref.ID)
			wantProject := goldenpath.DiscoveryProject{ID: "prj_auth", Name: "Authentication"}
			wantStatus := "open"
			if item.Ref.ID == restricted.ID {
				wantProject = goldenpath.DiscoveryProject{ID: "prj_private", Name: "Restricted project"}
				wantStatus = "in-progress"
			}
			if item.Project != wantProject || item.Status != wantStatus || item.Priority != "unset" {
				t.Fatal(item)
			}
		}
		if !slices.Equal(ids, expected) || strings.Contains(w.Body.String(), source) || strings.Contains(w.Body.String(), "assignee") {
			t.Fatal(w.Body.String())
		}
		return response.Data.NextCursor
	}
	ids := []string{open.ID, restricted.ID}
	slices.Sort(ids)
	cursor := list("/my-tickets?limit=1", ids[0])
	if cursor == "" || list("/my-tickets?after="+cursor, ids[1]) != "" {
		t.Fatal("unexpected pagination boundary")
	}
	get("wrk_main", "/my-tickets?after="+cursor, tokens[1], 400)
	get("wrk_other", "/my-tickets?after="+cursor, tokens[0], 400)
	get("wrk_main", "/projects/prj_auth/tickets?after="+cursor, tokens[0], 400)
	get("wrk_main", "/my-tickets?assignee=unassigned", tokens[0], 400)
	if w := get("wrk_other", "/my-tickets", tokens[0], 200); !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal(w.Body.String())
	}

	// A still-assigned restricted ticket must vanish, even when reusing the cursor.
	exec(`DELETE FROM radishnexus.project_memberships WHERE workspace_id='wrk_main' AND project_id='prj_private' AND user_id='usr_contributor'`)
	if list("/my-tickets?limit=1", open.ID) != "" {
		t.Fatal("hidden rows affected pagination")
	}
	if open.ID > ids[0] {
		list("/my-tickets?after="+cursor, open.ID)
	} else {
		list("/my-tickets?after=" + cursor)
	}
	exec(`INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role)
		VALUES('wrk_main','prj_private','usr_contributor','viewer')`)
	list("/my-tickets", ids...)
	exec(`UPDATE radishnexus.projects SET status='archived' WHERE id='prj_private'`)
	list("/my-tickets", open.ID)
	assign(open.ID, "usr_decider", "my-reassign")
	list("/my-tickets")
}
