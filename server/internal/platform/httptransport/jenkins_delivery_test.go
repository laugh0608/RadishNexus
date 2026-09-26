package httptransport

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const jenkinsBody = `{"version":1,"job_full_name":"example/build","build_number":42,"building":false,"in_progress":false,"result":"SUCCESS","started_at":null,"completed_at":"2026-09-26T02:01:00Z"}`

var jenkinsNow = time.Date(2026, 9, 26, 2, 2, 0, 0, time.UTC)

type jenkinsRecorderFunc func(context.Context, goldenpath.VerifiedJenkinsDelivery, goldenpath.RecordCompletedCIRunInput) (goldenpath.CIRunReceipt, error)

func (f jenkinsRecorderFunc) RecordCompletedJenkinsRun(c context.Context, d goldenpath.VerifiedJenkinsDelivery, i goldenpath.RecordCompletedCIRunInput) (goldenpath.CIRunReceipt, error) {
	return f(c, d, i)
}
func jenkinsSources(t *testing.T) []jenkins.Source {
	t.Helper()
	s, e := jenkins.LoadSources([]byte(`{"version":1,"sources":[{"source_id":"source_a","workspace_id":"wrk_main","component_id":"cmp_auth","job_full_name":"example/build","keys":[{"key_id":"key_a","secret_file":"/synthetic/key"},{"key_id":"key_b","secret_file":"/synthetic/new"}]}]}`), func(string, int64) ([]byte, error) { return []byte(strings.Repeat("ab", 32)), nil })
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func signJenkins(r *http.Request, body string) {
	sum := sha256.Sum256([]byte(body))
	key, _ := hex.DecodeString(strings.Repeat("ab", 32))
	mac := hmac.New(sha256.New, key)
	fmt.Fprint(mac, strings.Join([]string{"radishnexus-jenkins-v1", "POST", r.URL.Path, r.Header.Get("X-Nexus-Key-ID"), r.Header.Get("X-Nexus-Delivery-ID"), r.Header.Get("X-Nexus-Timestamp"), hex.EncodeToString(sum[:])}, "\n"))
	r.Header.Set("X-Nexus-Signature", "v1="+hex.EncodeToString(mac.Sum(nil)))
}
func jenkinsRequest(body string) *http.Request {
	r := httptest.NewRequest("POST", "https://nexus.example"+jenkins.Path("source_a"), strings.NewReader(body))
	r.TLS = &tls.ConnectionState{}
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Nexus-Key-ID", "key_a")
	r.Header.Set("X-Nexus-Delivery-ID", "build-42")
	r.Header.Set("X-Nexus-Timestamp", fmt.Sprint(jenkinsNow.Unix()))
	signJenkins(r, body)
	return r
}
func jenkinsHandler(t *testing.T, service JenkinsRecorder, record func(JenkinsDeliveryLog)) http.Handler {
	t.Helper()
	session, e := NewBrowserSessionPolicy("https://nexus.example")
	if e != nil {
		t.Fatal(e)
	}
	proxy, e := NewTrustedProxyPolicy("127.0.0.1/32")
	if e != nil {
		t.Fatal(e)
	}
	return WithRequestID(NewJenkinsDeliveryHandler(jenkinsSources(t), service, session, proxy, func() time.Time { return jenkinsNow }, record))
}
func TestJenkinsRejectsBeforeCoreAndRedacts(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*http.Request)
		body   string
		status int
	}{
		{name: "method", mutate: func(r *http.Request) { r.Method = "GET" }, status: 405},
		{name: "host", mutate: func(r *http.Request) { r.Host = "evil.example" }, status: 400},
		{name: "plaintext", mutate: func(r *http.Request) { r.TLS = nil }, status: 400},
		{name: "query", mutate: func(r *http.Request) { r.URL.RawQuery = "secret=1" }, status: 400},
		{name: "escaped", mutate: func(r *http.Request) { r.URL.RawPath = strings.Replace(r.URL.Path, "source_a", "%73ource_a", 1) }, status: 400},
		{name: "signature", mutate: func(r *http.Request) { r.Header.Set("X-Nexus-Signature", "sensitive") }, status: 401},
		{name: "session only", mutate: func(r *http.Request) {
			r.Header.Del("X-Nexus-Signature")
			r.Header.Set("Cookie", "nexus_session=sensitive")
		}, status: 401},
		{name: "unknown key", mutate: func(r *http.Request) { r.Header.Set("X-Nexus-Key-ID", "unknown") }, status: 401},
		{name: "expired", mutate: func(r *http.Request) {
			r.Header.Set("X-Nexus-Timestamp", fmt.Sprint(jenkinsNow.Unix()-301))
			signJenkins(r, jenkinsBody)
		}, status: 401},
		{name: "cross source", mutate: func(r *http.Request) { r.URL.Path = jenkins.Path("source_b") }, status: 401},
		{name: "compression", mutate: func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, status: 415},
		{name: "media", mutate: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, status: 415},
		{name: "oversize", body: jenkinsBody + strings.Repeat(" ", jenkins.MaxBody), status: 413},
		{name: "wrong job", body: strings.Replace(jenkinsBody, "example/build", "other/build", 1), status: 403},
		{name: "running", body: strings.Replace(jenkinsBody, `"building":false`, `"building":true`, 1), status: 422},
		{name: "unstable", body: strings.Replace(jenkinsBody, "SUCCESS", "UNSTABLE", 1), status: 422},
		{name: "duplicate field", body: strings.Replace(jenkinsBody, `"version":1`, `"version":1,"version":1`, 1), status: 400},
		{name: "identity mismatch", body: strings.Replace(jenkinsBody, ":42,", ":43,", 1), status: 400},
	}
	for _, header := range []string{"X-Nexus-Key-ID", "X-Nexus-Delivery-ID", "X-Nexus-Timestamp", "X-Nexus-Signature"} {
		cases = append(cases, struct {
			name   string
			mutate func(*http.Request)
			body   string
			status int
		}{name: header, mutate: func(r *http.Request) { r.Header.Add(header, r.Header.Get(header)) }, status: 401})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var logs []JenkinsDeliveryLog
			h := jenkinsHandler(t, jenkinsRecorderFunc(func(context.Context, goldenpath.VerifiedJenkinsDelivery, goldenpath.RecordCompletedCIRunInput) (goldenpath.CIRunReceipt, error) {
				calls++
				return goldenpath.CIRunReceipt{}, nil
			}), func(e JenkinsDeliveryLog) { logs = append(logs, e) })
			body := tc.body
			if body == "" {
				body = jenkinsBody
			}
			r := jenkinsRequest(body)
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || calls != 0 || len(logs) != 1 || w.Header().Get("Cache-Control") != "no-store" || !validRequestID(logs[0].RequestID) {
				t.Fatal(w.Code, w.Body.String(), calls, logs)
			}
			raw, _ := json.Marshal(logs)
			if strings.Contains(string(raw), "sensitive") || strings.Contains(w.Body.String(), "sensitive") || strings.Contains(string(raw), "example/build") {
				t.Fatal("log leak")
			}
			if tc.status == 401 && (logs[0].SourceID != "" || logs[0].DeliveryID != "") {
				t.Fatal("unverified fields logged")
			}
		})
	}
}
func TestJenkinsMapsCoreResultsAndRotatedKey(t *testing.T) {
	for _, tc := range []struct {
		err       error
		duplicate bool
		status    int
	}{{nil, false, 201}, {nil, true, 200}, {authz.ErrConflict, false, 409}, {authz.ErrNotFound, false, 404}, {errors.New("sensitive database detail"), false, 503}} {
		h := jenkinsHandler(t, jenkinsRecorderFunc(func(ctx context.Context, d goldenpath.VerifiedJenkinsDelivery, i goldenpath.RecordCompletedCIRunInput) (goldenpath.CIRunReceipt, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 5*time.Second {
				t.Fatal("deadline")
			}
			if d.WorkspaceID != "wrk_main" || d.SourceID != "source_a" || i.ComponentID != "cmp_auth" || i.Status != "succeeded" || i.StartedAt != nil {
				t.Fatal("mapping")
			}
			return goldenpath.CIRunReceipt{CIRun: goldenpath.CIRun{ID: "cir_test"}, Duplicate: tc.duplicate}, tc.err
		}), func(JenkinsDeliveryLog) {})
		r := jenkinsRequest(jenkinsBody)
		r.Header.Set("X-Nexus-Key-ID", "key_b")
		signJenkins(r, jenkinsBody)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "sensitive") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestJenkinsCapacityIsBoundedAndReleased(t *testing.T) {
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var group sync.WaitGroup
	h := jenkinsHandler(t, jenkinsRecorderFunc(func(context.Context, goldenpath.VerifiedJenkinsDelivery, goldenpath.RecordCompletedCIRunInput) (goldenpath.CIRunReceipt, error) {
		entered <- struct{}{}
		<-release
		return goldenpath.CIRunReceipt{CIRun: goldenpath.CIRun{ID: "cir_test"}}, nil
	}), func(JenkinsDeliveryLog) {})
	for range 4 {
		group.Add(1)
		go func() { defer group.Done(); h.ServeHTTP(httptest.NewRecorder(), jenkinsRequest(jenkinsBody)) }()
	}
	for range 4 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("entry timeout")
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, jenkinsRequest(jenkinsBody))
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatal(w.Code)
	}
	close(release)
	group.Wait()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, jenkinsRequest(jenkinsBody))
	if w.Code != 201 {
		t.Fatal(w.Code)
	}
}
func TestJenkinsSenderToRealTLSHandler(t *testing.T) {
	calls := 0
	service := jenkinsRecorderFunc(func(context.Context, goldenpath.VerifiedJenkinsDelivery, goldenpath.RecordCompletedCIRunInput) (goldenpath.CIRunReceipt, error) {
		calls++
		return goldenpath.CIRunReceipt{CIRun: goldenpath.CIRun{ID: "cir_tls"}, Duplicate: calls > 1}, nil
	})
	var handler http.Handler
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer server.Close()
	session, _ := NewBrowserSessionPolicy(server.URL)
	proxy, _ := NewTrustedProxyPolicy("127.0.0.1/32")
	handler = WithRequestID(NewJenkinsDeliveryHandler(jenkinsSources(t), service, session, proxy, time.Now, func(JenkinsDeliveryLog) {}))
	config, _ := jenkins.LoadSender([]byte(fmt.Sprintf(`{"version":1,"origin":%q,"source_id":"source_a","key_id":"key_a","secret_file":"/synthetic/key"}`, server.URL)), func(string, int64) ([]byte, error) { return []byte(strings.Repeat("ab", 32)), nil })
	for _, duplicate := range []bool{false, true} {
		result, e := jenkins.Send(context.Background(), config, []byte(jenkinsBody), server.Client())
		if e != nil || result.CIRunID != "cir_tls" || result.Duplicate != duplicate {
			t.Fatal(result, e)
		}
	}
	// An authenticated request with changed bytes cannot reuse the old signature.
	request := jenkinsRequest(jenkinsBody)
	request.URL.Scheme = "https"
	request.URL.Host = strings.TrimPrefix(server.URL, "https://")
	request.Host = request.URL.Host
	request.RequestURI = ""
	request.Body = io.NopCloser(bytes.NewBufferString(jenkinsBody + " "))
	request.ContentLength = int64(len(jenkinsBody) + 1)
	response, e := server.Client().Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	if response.StatusCode != 401 || calls != 2 {
		t.Fatal(response.StatusCode, calls)
	}
}

func TestJenkinsRejectsNoncanonicalPathsWithoutRedirect(t *testing.T) {
	handler := jenkinsHandler(t, jenkinsRecorderFunc(func(context.Context, goldenpath.VerifiedJenkinsDelivery, goldenpath.RecordCompletedCIRunInput) (goldenpath.CIRunReceipt, error) {
		t.Fatal("noncanonical request reached core")
		return goldenpath.CIRunReceipt{}, nil
	}), func(JenkinsDeliveryLog) {})
	mux := http.NewServeMux()
	mux.Handle("/", http.NotFoundHandler())
	wrapped := WithJenkinsDeliveries(mux, handler)
	for _, path := range []string{"//api/v1/integrations/jenkins/source_a/deliveries", "/prefix/../api/v1/integrations/jenkins/source_a/deliveries", "/api/v1/integrations/jenkins//source_a/deliveries", "/api/v1/integrations/jenkins/source_a/../source_b/deliveries"} {
		r := jenkinsRequest(jenkinsBody)
		r.URL.Path = path
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, r)
		if w.Code != 400 || w.Header().Get("Location") != "" {
			t.Fatal(path, w.Code)
		}
	}
}
