package httptransport

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/markdown"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type documentApp struct {
	DocumentApplication
	calls int
	input goldenpath.DocumentInput
	kind  string
	err   error
}

func (a *documentApp) WriteDocument(_ context.Context, _ goldenpath.Invocation, kind string, in goldenpath.DocumentInput) (goldenpath.DocumentResult, error) {
	a.calls++
	a.input = in
	a.kind = kind
	return goldenpath.DocumentResult{Ref: entityref.Ref{Type: "document", ID: "doc_created"}, AppliedRevision: 1, Created: kind == "document.create"}, a.err
}
func TestDocumentHTTPWriteBoundary(t *testing.T) {
	session, _ := NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := NewTrustedProxyPolicy("10.0.0.0/8")
	body := `{"client_operation_id":"create","title":"设计","body_markdown":"正文","format_version":"nexus-markdown-v1"}`
	for _, test := range []struct {
		name, body string
		csrf       bool
		err        error
		want       int
	}{
		{"create", body, true, nil, 201}, {"csrf", body, false, nil, 403},
		{"unknown", strings.TrimSuffix(body, "}") + `,"extra":1}`, true, nil, 400},
		{"duplicate", strings.TrimSuffix(body, "}") + `,"title":"other"}`, true, nil, 400},
		{"invalid UTF8", strings.Replace(body, `"正文"`, "\""+string([]byte{0xff})+"\"", 1), true, nil, 400},
		{"unpaired high", strings.Replace(body, `"正文"`, `"\ud800"`, 1), true, nil, 400},
		{"unpaired low", strings.Replace(body, `"正文"`, `"\udc00"`, 1), true, nil, 400},
		{"emoji escape", strings.Replace(body, `"正文"`, `"\ud83d\ude00"`, 1), true, nil, 201},
		{"literal escape", strings.Replace(body, `"正文"`, `"\\ud800"`, 1), true, nil, 201},
		{"null", strings.Replace(body, `"正文"`, `null`, 1), true, nil, 400},
		{"stale", body, true, &goldenpath.RevisionConflict{CurrentRevision: 9}, 409},
		{"parse", body, true, &markdown.Diagnostic{Code: "html_forbidden", Offset: 3}, 400},
		{"revoked", body, true, authz.ErrNotFound, 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := &documentApp{err: test.err}
			sessions := validMessagingSessions()
			h := WithRequestID(NewDocumentHandler(sessions, a, session, proxy))
			r := messagingWriteRequest("POST", "/api/v1/workspaces/wrk_main/tickets/tkt_origin/documents", test.body)
			if !test.csrf {
				r.Header.Del(CSRFHeaderName)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("cacheable")
			}
			if w.Code == 201 {
				if a.input.TargetID != "tkt_origin" || a.kind != "document.create" || sessions.verifyCalls != 1 {
					t.Fatal("wrong boundary")
				}
				for _, secret := range []string{"body_markdown", "client_operation", "payload_sha", "event_id", "正文"} {
					if strings.Contains(w.Body.String(), secret) {
						t.Fatal("write response leaked input", secret)
					}
				}
			}
			if test.name == "stale" && !strings.Contains(w.Body.String(), `"current_revision":9`) {
				t.Fatal("missing revision")
			}
			if test.name == "parse" && !strings.Contains(w.Body.String(), `"offset":3`) {
				t.Fatal("missing diagnostic")
			}
		})
	}
}
func TestDocumentHistoryCursorScope(t *testing.T) {
	scope := discoveryCursor{Version: 1, WorkspaceID: "wrk_main", Kind: "document-revision", ProjectID: "doc_one", AfterID: "rev_7"}
	raw, _ := json.Marshal(scope)
	cursor := base64Cursor(t, string(raw))
	scope.AfterID = ""
	in, err := parseDiscoveryQuery("after="+cursor, scope)
	if err != nil || in.AfterID != "rev_7" {
		t.Fatal(in, err)
	}
	scope.ProjectID = "doc_two"
	if _, err = parseDiscoveryQuery("after="+cursor, scope); err == nil {
		t.Fatal("cross document cursor accepted")
	}
}
func TestTicketDocumentRelationDTO(t *testing.T) {
	relations := []goldenpath.RelationProjection{{State: goldenpath.ProjectionVisible, Direction: "outgoing", RelationType: "implements", Target: entityref.Ref{Type: "decision", ID: "dec_source"}, Title: "来源"}, {State: goldenpath.ProjectionVisible, Direction: "outgoing", RelationType: "relates-to", Target: entityref.Ref{Type: "document", ID: "doc_design"}, Title: "设计"}}
	source, err := collaborationSourceRelations("ticket", relations)
	if err != nil || len(source) != 1 {
		t.Fatal(source, err)
	}
	if _, err = publicCollaborationRelations(relations); err != nil {
		t.Fatal(err)
	}
}
