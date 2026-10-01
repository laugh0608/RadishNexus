package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type JenkinsRecorder interface {
	RecordCompletedJenkinsRun(context.Context, goldenpath.VerifiedJenkinsDelivery, goldenpath.RecordCompletedCIRunInput) (goldenpath.CIRunReceipt, error)
}
type JenkinsDeliveryLog struct {
	Time       time.Time `json:"time"`
	RequestID  string    `json:"request_id"`
	Code       string    `json:"code"`
	Status     int       `json:"status"`
	DurationMS int64     `json:"duration_ms"`
	SourceID   string    `json:"source_id,omitempty"`
	DeliveryID string    `json:"delivery_id,omitempty"`
	CIRunID    string    `json:"ci_run_id,omitempty"`
}

// Jenkins logging is a safe field allowlist, never an application error dump.
func LogJenkinsDelivery(entry JenkinsDeliveryLog) {
	body, _ := json.Marshal(entry)
	log.Printf("jenkins_delivery %s", body)
}

// WithJenkinsDeliveries dispatches before ServeMux path cleaning, so signed
// noncanonical paths fail instead of redirecting or becoming another resource.
func WithJenkinsDeliveries(next, handler http.Handler) http.Handler {
	if handler == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := strings.TrimSuffix(jenkins.PathPrefix, "/")
		if strings.HasPrefix(r.URL.Path, prefix) || strings.HasPrefix(path.Clean(r.URL.Path), prefix) {
			handler.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func NewJenkinsDeliveryHandler(sources []jenkins.Source, service JenkinsRecorder, session BrowserSessionPolicy, proxy TrustedProxyPolicy, now func() time.Time, record func(JenkinsDeliveryLog)) http.Handler {
	configured := make(map[string]jenkins.Source, len(sources))
	for _, s := range sources {
		configured[s.ID] = s
	}
	slots := make(chan struct{}, 4)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := now()
		elapsed := time.Now()
		entry := JenkinsDeliveryLog{Time: start, RequestID: RequestID(r.Context())}
		defer func() { entry.DurationMS = time.Since(elapsed).Milliseconds(); record(entry) }()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fail := func(status int, code string) {
			entry.Status = status
			entry.Code = code
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(ErrorResponse{Error: ErrorObject{Code: code, Message: strings.ReplaceAll(code, "_", " "), RequestID: entry.RequestID}})
		}
		mappedFail := func(err error) { m := MapApplicationError(err); fail(m.StatusCode, m.Code) }
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			mappedFail(ErrMethodNotAllowed)
			return
		}
		if err := session.ValidateHost(r); err != nil {
			mappedFail(err)
			return
		}
		if _, err := proxy.ClientIP(r); err != nil {
			mappedFail(err)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, jenkins.PathPrefix), "/")
		if len(parts) != 2 || parts[1] != "deliveries" || r.URL.Path != jenkins.Path(parts[0]) || r.URL.EscapedPath() != r.URL.Path || r.URL.RawQuery != "" || r.URL.ForceQuery {
			fail(400, "invalid_request")
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "1")
			fail(429, "rate_limited")
			return
		}
		content := r.Header.Values("Content-Type")
		if len(content) != 1 || len(r.Header.Values("Content-Encoding")) != 0 {
			mappedFail(ErrUnsupportedMediaType)
			return
		}
		media, params, err := mime.ParseMediaType(content[0])
		if err != nil || media != "application/json" || len(params) > 1 || (len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8")) {
			mappedFail(ErrUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, jenkins.MaxBody))
		if err != nil {
			var max *http.MaxBytesError
			if errors.As(err, &max) {
				mappedFail(ErrPayloadTooLarge)
			} else {
				fail(400, "invalid_request")
			}
			return
		}
		header := func(name string) string {
			v := r.Header.Values(name)
			if len(v) != 1 {
				return ""
			}
			return v[0]
		}
		key, delivery, stamp, sig := header("X-Nexus-Key-ID"), header("X-Nexus-Delivery-ID"), header("X-Nexus-Timestamp"), header("X-Nexus-Signature")
		source, ok := configured[parts[0]]
		// Check time after reading the body: slow uploads must not extend replay validity.
		checkedAt := now()
		if !ok || !source.Verify(key, delivery, stamp, sig, body, checkedAt) {
			fail(401, "unauthenticated")
			return
		}
		entry.SourceID = source.ID
		entry.DeliveryID = delivery
		payload, err := jenkins.ParsePayload(body, checkedAt)
		if err != nil {
			if errors.Is(err, jenkins.ErrRunNotCompleted) || errors.Is(err, jenkins.ErrUnsupportedResult) {
				fail(422, err.Error())
			} else {
				fail(400, "invalid_request")
			}
			return
		}
		verified, input, err := source.Map(payload, delivery)
		if err != nil {
			if errors.Is(err, jenkins.ErrBinding) {
				fail(403, "source_binding_mismatch")
			} else {
				fail(400, "invalid_request")
			}
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		receipt, err := service.RecordCompletedJenkinsRun(ctx, verified, input)
		if err != nil {
			switch {
			case errors.Is(err, authz.ErrNotFound), errors.Is(err, authz.ErrForbidden):
				fail(404, "not_found")
			case errors.Is(err, authz.ErrConflict):
				fail(409, "conflict")
			case errors.Is(err, authz.ErrInvalid):
				fail(400, "invalid_request")
			default:
				fail(503, "temporarily_unavailable")
			}
			return
		}
		if !validScopedID(receipt.CIRun.ID, "cir_") {
			fail(503, "temporarily_unavailable")
			return
		}
		entry.Status = http.StatusCreated
		entry.Code = "recorded"
		entry.CIRunID = receipt.CIRun.ID
		if receipt.Duplicate {
			entry.Status = http.StatusOK
			entry.Code = "duplicate"
		}
		w.WriteHeader(entry.Status)
		_ = json.NewEncoder(w).Encode(struct {
			Data struct {
				CIRunID   string `json:"ci_run_id"`
				Duplicate bool   `json:"duplicate"`
			} `json:"data"`
		}{Data: struct {
			CIRunID   string `json:"ci_run_id"`
			Duplicate bool   `json:"duplicate"`
		}{receipt.CIRun.ID, receipt.Duplicate}})
	})
}
