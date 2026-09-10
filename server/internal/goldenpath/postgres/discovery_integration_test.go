//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

func assertDiscoverySlice(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.users(id,display_name) VALUES ('usr_discovery_reader','Reader'),('usr_discovery_owner','Owner'),('usr_discovery_member','Member');
		INSERT INTO radishnexus.user_accounts(user_id,status,created_at) VALUES ('usr_discovery_reader','active',now());
		INSERT INTO radishnexus.workspaces(id,name) VALUES ('wrk_discovery','Discovery'),('wrk_discovery_other','Other');
		INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,role) VALUES
		('wrk_discovery','usr_discovery_reader','member'),('wrk_discovery','usr_discovery_owner','owner'),('wrk_discovery','usr_discovery_member','member');
		INSERT INTO radishnexus.teams(id,workspace_id,name) VALUES ('tem_discovery','wrk_discovery','Team'),('tem_discovery_other','wrk_discovery_other','Other');
		INSERT INTO radishnexus.projects(id,workspace_id,key,name,owner_team_id,visibility,status,created_by_kind) VALUES
		('prj_discovery_a','wrk_discovery','A','Public project','tem_discovery','workspace','active','system'),
		('prj_discovery_b','wrk_discovery','B','Private project','tem_discovery','restricted','active','system'),
		('prj_discovery_c','wrk_discovery','C','Archived project','tem_discovery','workspace','archived','system'),
		('prj_discovery_other','wrk_discovery_other','OTHER','Other project','tem_discovery_other','workspace','active','system');
		INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role) VALUES
		('wrk_discovery','prj_discovery_a','usr_discovery_owner','admin'),('wrk_discovery','prj_discovery_b','usr_discovery_member','viewer');
		INSERT INTO radishnexus.channels(id,workspace_id,governing_project_id,name,visibility,status,created_by_kind) VALUES
		('chn_discovery_a','wrk_discovery','prj_discovery_a','Public channel','project','active','system'),
		('chn_discovery_b','wrk_discovery','prj_discovery_a','Private channel','restricted','active','system'),
		('chn_discovery_c','wrk_discovery','prj_discovery_a','Archived channel','project','archived','system'),
		('chn_discovery_hidden_project','wrk_discovery','prj_discovery_b','Private project channel','project','active','system');
		INSERT INTO radishnexus.channel_memberships(workspace_id,channel_id,user_id) VALUES ('wrk_discovery','chn_discovery_b','usr_discovery_member');
	`)
	if err != nil {
		t.Fatal(err)
	}
	store := goldenpostgres.New(pool)
	service := goldenpath.NewDiscoveryService(store)
	reader := authz.Principal{Kind: authz.PrincipalUser, ID: "usr_discovery_reader", WorkspaceID: "wrk_discovery"}
	owner := reader
	owner.ID = "usr_discovery_owner"
	member := reader
	member.ID = "usr_discovery_member"
	ids := func(page goldenpath.DiscoveryPage) []string {
		result := []string{}
		for _, item := range page.Items {
			result = append(result, item.Ref.ID)
		}
		return result
	}
	for _, principal := range []authz.Principal{reader, owner} {
		page, err := service.ListProjects(ctx, principal, goldenpath.DiscoveryPageInput{Limit: 1})
		if err != nil || !reflect.DeepEqual(ids(page), []string{"prj_discovery_a"}) || page.NextID != "prj_discovery_a" {
			t.Fatal(page, err)
		}
		page, err = service.ListProjects(ctx, principal, goldenpath.DiscoveryPageInput{Limit: 1, AfterID: page.NextID})
		if err != nil || !reflect.DeepEqual(ids(page), []string{"prj_discovery_c"}) || page.NextID != "" || page.Items[0].Status != "archived" {
			t.Fatal("hidden Project affected page", page, err)
		}
		page, err = service.ListProjectChannels(ctx, principal, "prj_discovery_a", goldenpath.DiscoveryPageInput{Limit: 1})
		if err != nil || !reflect.DeepEqual(ids(page), []string{"chn_discovery_a"}) || page.NextID != "chn_discovery_a" {
			t.Fatal(page, err)
		}
		page, err = service.ListProjectChannels(ctx, principal, "prj_discovery_a", goldenpath.DiscoveryPageInput{Limit: 1, AfterID: page.NextID})
		if err != nil || !reflect.DeepEqual(ids(page), []string{"chn_discovery_c"}) || page.NextID != "" || page.Items[0].Status != "archived" {
			t.Fatal("hidden Channel affected page", page, err)
		}
		for _, item := range page.Items {
			if err := store.AuthorizeChannelRead(ctx, principal, item.Ref.ID); err != nil {
				t.Fatal("list/read policy mismatch", err)
			}
		}
		if err := store.AuthorizeChannelRead(ctx, principal, "chn_discovery_b"); !errors.Is(err, authz.ErrNotFound) {
			t.Fatal("admin/owner pierced restricted Channel", err)
		}
		for _, project := range []string{"prj_discovery_b", "prj_discovery_other", "prj_missing"} {
			if _, err := service.ListProjectChannels(ctx, principal, project, goldenpath.DiscoveryPageInput{Limit: 25}); !errors.Is(err, authz.ErrNotFound) {
				t.Fatal("hidden/foreign Project discoverable", err)
			}
		}
	}
	page, err := service.ListProjectChannels(ctx, member, "prj_discovery_a", goldenpath.DiscoveryPageInput{Limit: 25})
	if err != nil || !reflect.DeepEqual(ids(page), []string{"chn_discovery_a", "chn_discovery_b", "chn_discovery_c"}) {
		t.Fatal(page, err)
	}
	page, err = service.ListProjectChannels(ctx, member, "prj_discovery_b", goldenpath.DiscoveryPageInput{Limit: 25})
	if err != nil || !reflect.DeepEqual(ids(page), []string{"chn_discovery_hidden_project"}) {
		t.Fatal(page, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM radishnexus.channel_memberships WHERE workspace_id='wrk_discovery' AND user_id='usr_discovery_member'; DELETE FROM radishnexus.project_memberships WHERE project_id='prj_discovery_b' AND user_id='usr_discovery_member'`); err != nil {
		t.Fatal(err)
	}
	page, err = service.ListProjectChannels(ctx, member, "prj_discovery_a", goldenpath.DiscoveryPageInput{Limit: 1, AfterID: "chn_discovery_a"})
	if err != nil || !reflect.DeepEqual(ids(page), []string{"chn_discovery_c"}) {
		t.Fatal("cursor bypassed revoked membership", page, err)
	}
	if _, err := service.ListProjectChannels(ctx, member, "prj_discovery_b", goldenpath.DiscoveryPageInput{Limit: 25}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("Project revocation ignored", err)
	}
	page, err = service.ListProjectChannels(ctx, reader, "prj_discovery_c", goldenpath.DiscoveryPageInput{Limit: 25})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("empty archived Project unreadable", page, err)
	}
	foreign := reader
	foreign.WorkspaceID = "wrk_discovery_other"
	if _, err := service.ListProjects(ctx, foreign, goldenpath.DiscoveryPageInput{Limit: 25}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("foreign Workspace discoverable", err)
	}
	assertDiscoveryHTTP(t, ctx, pool, service)
	assertDiscoveryScale(t, ctx, pool, owner)
}

func assertDiscoveryHTTP(t *testing.T, ctx context.Context, pool *pgxpool.Pool, service *goldenpath.DiscoveryService) {
	t.Helper()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	token := deploymentHTTPToken(91)
	digest := sha256.Sum256([]byte(token))
	csrf := sha256.Sum256([]byte(deploymentHTTPToken(92)))
	if _, err := pool.Exec(ctx, `INSERT INTO radishnexus.user_sessions(id,user_id,token_digest,csrf_token_digest,created_at,expires_at) VALUES ('ses_discovery','usr_discovery_reader',$1,$2,$3,$4)`, digest[:], csrf[:], now, now.Add(authn.SessionLifetime)); err != nil {
		t.Fatal(err)
	}
	auth := authn.NewService(authpostgres.New(pool), nil, nil, fixedClock{now: now})
	session, err := httptransport.NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := httptransport.NewTrustedProxyPolicy("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	handler := httptransport.WithRequestID(httptransport.NewDiscoveryHandler(auth, service, session, proxy))
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "https://nexus.example.test"+path, nil)
		r.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	response := get("/api/v1/workspaces/wrk_discovery/projects?limit=1")
	var payload struct {
		Data struct {
			NextCursor *string `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || payload.Data.NextCursor == nil || strings.Contains(response.Body.String(), "Private") {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, sql := range []string{`UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE workspace_id='wrk_discovery' AND user_id='usr_discovery_reader'`, `DELETE FROM radishnexus.workspace_memberships WHERE workspace_id='wrk_discovery' AND user_id='usr_discovery_reader'`} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
		response = get("/api/v1/workspaces/wrk_discovery/projects?after=" + *payload.Data.NextCursor)
		if response.Code != 404 || strings.Contains(response.Body.String(), "project:") {
			t.Fatal("membership revocation ignored", response.Code, response.Body.String())
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE radishnexus.user_sessions SET revoked_at=$1 WHERE id='ses_discovery'`, now); err != nil {
		t.Fatal(err)
	}
	if response = get("/api/v1/workspaces/wrk_discovery/projects"); response.Code != 401 {
		t.Fatal("Session revocation ignored", response.Code, response.Body.String())
	}
}

func assertDiscoveryScale(t *testing.T, ctx context.Context, pool *pgxpool.Pool, principal authz.Principal) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.projects(id,workspace_id,key,name,owner_team_id,visibility,status,created_by_kind)
		SELECT 'prj_discovery_scale_'||lpad(i::text,3,'0'),'wrk_discovery','SCALE_'||i,'Project '||i,'tem_discovery',CASE WHEN i%2=0 THEN 'workspace' ELSE 'restricted' END,'active','system' FROM generate_series(1,100) i;
		INSERT INTO radishnexus.channels(id,workspace_id,governing_project_id,name,visibility,status,created_by_kind)
		SELECT 'chn_discovery_scale_'||lpad(i::text,3,'0'),'wrk_discovery','prj_discovery_a','Channel '||i,CASE WHEN i%2=0 THEN 'project' ELSE 'restricted' END,'active','system' FROM generate_series(1,100) i;
	`); err != nil {
		t.Fatal(err)
	}
	counter := &relationQueryCounter{}
	config := pool.Config()
	config.ConnConfig.Tracer = counter
	measured, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	if err := measured.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	service := goldenpath.NewDiscoveryService(goldenpostgres.New(measured))
	for _, kind := range []string{"project", "channel"} {
		counter.queries.Store(0)
		start := time.Now()
		var page goldenpath.DiscoveryPage
		if kind == "project" {
			page, err = service.ListProjects(ctx, principal, goldenpath.DiscoveryPageInput{AfterID: "prj_discovery_scale_000", Limit: 25})
		} else {
			page, err = service.ListProjectChannels(ctx, principal, "prj_discovery_a", goldenpath.DiscoveryPageInput{AfterID: "chn_discovery_scale_000", Limit: 25})
		}
		elapsed := time.Since(start)
		if err != nil || len(page.Items) != 25 || !strings.HasSuffix(page.NextID, "050") || counter.queries.Load() > 6 {
			t.Fatal("unbounded or incorrectly filtered discovery", kind, page, err, counter.queries.Load())
		}
		t.Logf("%s discovery scale: 100 candidates, 25 visible results, %d SQL calls, %s", kind, counter.queries.Load(), elapsed)
	}
}
