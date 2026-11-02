package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
)

func TestDecisionSupersessionStrictTransportAndResult(t *testing.T) {
	path := "/api/v1/workspaces/wrk_main/decisions/dec_old/supersession"
	a := &fakeCollaborationApplication{supersessionResult: goldenpath.SupersedeDecisionResult{DecisionID: "dec_old", ReplacementID: "dec_new", LinkID: "lnk_pair", SupersessionReason: "Reason", FromRevision: 2, ToRevision: 3, ReplacementFromRevision: 2, ReplacementToRevision: 3, OccurredAt: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)}}
	h := testCollaborationHandler(t, validMessagingSessions(), a)
	valid := `{"client_operation_id":"web:replace","replacement_decision_id":"dec_new","base_decision_revision":2,"base_replacement_revision":2,"supersession_reason":" Reason ","confirmed":true}`
	for _, body := range []string{`{}`, `null`, valid + ` {}`, strings.Replace(valid, `"base_decision_revision":2`, `"base_decision_revision":null`, 1), strings.Replace(valid, `"base_decision_revision":2`, `"base_decision_revision":2.5`, 1), strings.Replace(valid, `"confirmed":true`, `"confirmed":false`, 1), strings.Replace(valid, `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1), strings.Replace(valid, `" Reason "`, `"\ud800"`, 1), strings.Replace(valid, `" Reason "`, `"\u0085"`, 1), strings.Replace(valid, `" Reason "`, `"bad\u0000"`, 1), strings.Replace(valid, `" Reason "`, `"`+strings.Repeat("x", 32769)+`"`, 1)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, messagingWriteRequest(http.MethodPost, path, body))
		if w.Code != 400 || a.supersessionCalls != 0 {
			t.Fatal("malformed request reached command", w.Code)
		}
	}
	for _, modify := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Origin") }, func(r *http.Request) { r.Header.Del(CSRFHeaderName) }} {
		r := messagingWriteRequest(http.MethodPost, path, valid)
		modify(r)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 || a.supersessionCalls != 0 {
			t.Fatal("write authority", w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, messagingWriteRequest(http.MethodPost, path, valid))
	if w.Code != 200 || a.supersessionCalls != 1 || a.invocation.Principal.ID != "usr_reader" {
		t.Fatal("valid supersession", w.Code, w.Body.String())
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body) != 1 || body["data"] == nil {
		t.Fatal("response envelope")
	}
	if strings.Contains(w.Body.String(), "payload_sha256") || strings.Contains(w.Body.String(), "client_operation_id") || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("response metadata leak")
	}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, messagingRequest(method, path, ""))
		if w.Code != 405 || w.Header().Get("Allow") != "POST" {
			t.Fatal("method envelope", method, w.Code)
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingWriteRequest(http.MethodPost, path+"?x=1", valid))
	if w.Code != 400 {
		t.Fatal("query allowed")
	}
	a.supersessionResult.ReplacementID = "dec_wrong"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingWriteRequest(http.MethodPost, path, valid))
	if w.Code != 500 {
		t.Fatal("mismatched result accepted")
	}
}
