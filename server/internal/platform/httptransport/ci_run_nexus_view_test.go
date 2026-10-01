package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func ciRunTestView() goldenpath.NexusView {
	v := testDeploymentNexusView(entityref.Ref{Type: "ci-run", ID: "cir_build"}, true)
	v.Current.Environment = nil
	v.Current.CIRun = nil
	component := goldenpath.SubjectProjection{State: goldenpath.ProjectionVisible, Ref: entityref.Ref{Type: "component", ID: "cmp_api"}, Title: "API"}
	v.Current.Component = &component
	v.Relations = nil
	v.Timeline[0].ActivityType = "ci-run.recorded"
	v.Timeline[0].Actor = goldenpath.ActorRef{Kind: "plugin"}
	v.Timeline[0].Subjects = []goldenpath.SubjectProjection{component}
	return v
}
func ciRunTestHandler(t *testing.T, sessions *fakeMessagingSessions, reader *fakeNexusViewReader) http.Handler {
	t.Helper()
	session, err := NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewTrustedProxyPolicy("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	return WithRequestID(NewCIRunNexusViewHandler(sessions, reader, session, proxy))
}
func ciRunTestRequest() *http.Request {
	r := httptest.NewRequest("GET", "https://nexus.example.test/api/v1/workspaces/wrk_main/ci-runs/cir_build/nexus-view", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: transportToken(1)})
	return r
}
func TestCIRunTransportSafeCurrentPrincipalAndNullableTime(t *testing.T) {
	for _, withStart := range []bool{true, false} {
		sessions := validMessagingSessions()
		reader := &fakeNexusViewReader{view: ciRunTestView()}
		if !withStart {
			reader.view.Current.StartedAt = nil
		}
		w := httptest.NewRecorder()
		ciRunTestHandler(t, sessions, reader).ServeHTTP(w, ciRunTestRequest())
		if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("Vary") != "Cookie" {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if sessions.resolveWorkspace != "wrk_main" || sessions.resolveToken != transportToken(1) || sessions.verifyCalls != 0 || reader.principal.ID != "usr_reader" || reader.principal.WorkspaceID != "wrk_main" || reader.target != reader.view.Current.Ref {
			t.Fatal("principal or target mismatch")
		}
		body := w.Body.String()
		for _, required := range []string{`"kind":"plugin"`, `"relations":[]`, `"activity_type":"ci-run.recorded"`, `"title":"API"`, `"updated_at"`} {
			if !strings.Contains(body, required) {
				t.Fatalf("missing %s: %s", required, body)
			}
		}
		for _, forbidden := range []string{"source_id", "external_run", "receipt", "digest", "secret", "projection_version", "safe_facts"} {
			if strings.Contains(body, forbidden) {
				t.Fatal("leaked", forbidden)
			}
		}
		if !withStart && !strings.Contains(body, `"started_at":null`) {
			t.Fatal(body)
		}
	}
}
func TestCIRunTransportRejectsDrift(t *testing.T) {
	cases := map[string]func(*goldenpath.NexusView){
		"wrong object":         func(v *goldenpath.NexusView) { v.Current.Ref.ID = "cir_other" },
		"title leak":           func(v *goldenpath.NexusView) { v.Current.Title = "source-secret" },
		"actor leak":           func(v *goldenpath.NexusView) { v.Timeline[0].Actor.ID = "source-secret" },
		"running":              func(v *goldenpath.NexusView) { v.Current.Status = "running" },
		"missing finish":       func(v *goldenpath.NexusView) { v.Current.CompletedAt = nil },
		"reversed time":        func(v *goldenpath.NexusView) { v.Current.StartedAt = v.Current.RecordedAt },
		"wrong event time":     func(v *goldenpath.NexusView) { v.Timeline[0].OccurredAt = *v.Current.RecordedAt },
		"wrong subject":        func(v *goldenpath.NexusView) { v.Timeline[0].Subjects[0].Title = "source-secret" },
		"restricted component": func(v *goldenpath.NexusView) { v.Current.Component.State = goldenpath.ProjectionRestricted },
		"unexpected relations": func(v *goldenpath.NexusView) { v.Relations = []goldenpath.RelationProjection{{}} },
		"missing timeline":     func(v *goldenpath.NexusView) { v.Timeline = nil },
		"extra safe fact":      func(v *goldenpath.NexusView) { v.Timeline[0].SafeFacts["source"] = "source-secret" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			v := ciRunTestView()
			mutate(&v)
			w := httptest.NewRecorder()
			ciRunTestHandler(t, validMessagingSessions(), &fakeNexusViewReader{view: v}).ServeHTTP(w, ciRunTestRequest())
			if w.Code != 500 || strings.Contains(w.Body.String(), "source-secret") {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestCIRunTransportAuthMethodsAndScope(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*http.Request, *fakeMessagingSessions, *fakeNexusViewReader)
		want   int
	}{
		{"no session", func(r *http.Request, _ *fakeMessagingSessions, _ *fakeNexusViewReader) { r.Header.Del("Cookie") }, 401},
		{"revoked membership", func(_ *http.Request, s *fakeMessagingSessions, _ *fakeNexusViewReader) {
			s.resolveErr = authz.ErrForbidden
		}, 404},
		{"unknown or unreadable", func(_ *http.Request, _ *fakeMessagingSessions, v *fakeNexusViewReader) { v.err = authz.ErrNotFound }, 404},
		{"bad workspace", func(r *http.Request, _ *fakeMessagingSessions, _ *fakeNexusViewReader) {
			r.URL.Path = strings.Replace(r.URL.Path, "wrk_main", "invalid", 1)
		}, 400},
		{"bad id", func(r *http.Request, _ *fakeMessagingSessions, _ *fakeNexusViewReader) {
			r.URL.Path = strings.Replace(r.URL.Path, "cir_build", "dpl_build", 1)
		}, 400},
		{"HEAD", func(r *http.Request, _ *fakeMessagingSessions, _ *fakeNexusViewReader) { r.Method = "HEAD" }, 405},
		{"POST", func(r *http.Request, _ *fakeMessagingSessions, _ *fakeNexusViewReader) { r.Method = "POST" }, 405},
		{"bad host", func(r *http.Request, _ *fakeMessagingSessions, _ *fakeNexusViewReader) { r.Host = "attacker.example" }, 400},
		{"plain http", func(r *http.Request, _ *fakeMessagingSessions, _ *fakeNexusViewReader) {
			r.TLS = nil
			r.URL.Scheme = "http"
		}, 400},
		{"unknown path", func(r *http.Request, _ *fakeMessagingSessions, _ *fakeNexusViewReader) { r.URL.Path += "/unknown" }, 404},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := validMessagingSessions()
			v := &fakeNexusViewReader{view: ciRunTestView()}
			r := ciRunTestRequest()
			c.mutate(r, s, v)
			w := httptest.NewRecorder()
			ciRunTestHandler(t, s, v).ServeHTTP(w, r)
			if w.Code != c.want || w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if c.want == 405 && w.Header().Get("Allow") != "GET" {
				t.Fatal("missing Allow")
			}
		})
	}
}
