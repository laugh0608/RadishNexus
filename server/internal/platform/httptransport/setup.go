package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type SetupApplication interface {
	Status(context.Context) (string, error)
	Complete(context.Context, string, authn.BootstrapInput) error
}
type SetupReadiness interface{ CheckReady(context.Context) error }

func NewSetupHandler(app SetupApplication, readiness SetupReadiness, session BrowserSessionPolicy, proxy TrustedProxyPolicy, guard *LoginGuard) http.Handler {
	return privateNoStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/setup" {
			writeIdentityError(w, r, authz.ErrNotFound)
			return
		}
		if r.Method != "GET" && r.Method != "POST" {
			w.Header().Set("Allow", "GET, POST")
			writeIdentityError(w, r, ErrMethodNotAllowed)
			return
		}
		ip, err := proxy.ClientIP(r)
		if err == nil {
			err = session.ValidateHost(r)
		}
		if err == nil && r.Method == "POST" {
			err = session.ValidateOrigin(r)
		}
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		if r.URL.RawQuery != "" {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		// A partially upgraded or drifted instance must never look like an empty one.
		readyCtx, readyCancel := context.WithTimeout(ctx, 2*time.Second)
		err = readiness.CheckReady(readyCtx)
		readyCancel()
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		if r.Method == "GET" {
			status, err := app.Status(ctx)
			if err != nil {
				writeIdentityError(w, r, err)
				return
			}
			if status != "required" && status != "unavailable" && status != "complete" {
				writeIdentityError(w, r, errors.New("invalid setup status"))
				return
			}
			writeIdentityJSON(w, r, 200, struct {
				Status string `json:"status"`
			}{status})
			return
		}
		release, retry, err := guard.Begin(ip, time.Now())
		if err != nil {
			w.Header().Set("Retry-After", strconv.FormatInt(max(1, int64((retry+time.Second-1)/time.Second)), 10))
			writeIdentityError(w, r, err)
			return
		}
		defer release()
		// Retain the existing strict bounded object decoder, including duplicate-key
		// rejection. No setup code or credential is logged or returned.
		fields, err := decodeStrictObject(w, r, []string{"setup_code", "email", "display_name", "password", "workspace_name"}, 4*1024)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		var code string
		input := authn.BootstrapInput{}
		for key, target := range map[string]*string{"setup_code": &code, "email": &input.Email, "display_name": &input.DisplayName, "password": &input.Password, "workspace_name": &input.WorkspaceName} {
			if string(fields[key]) == "null" || json.Unmarshal(fields[key], target) != nil {
				writeIdentityError(w, r, authz.ErrInvalid)
				return
			}
		}
		if err := app.Complete(ctx, code, input); err != nil {
			writeIdentityError(w, r, err)
			return
		}
		writeIdentityJSON(w, r, 201, struct {
			Status string `json:"status"`
		}{"complete"})
	}))
}
