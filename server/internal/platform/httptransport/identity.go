package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const oidcCookieName = "__Host-radishnexus-oidc"

type IdentityService interface {
	Account(context.Context, string) (authn.AccountDetails, error)
	CreateInvitation(context.Context, string, string) (authn.Invitation, error)
	AcceptInvitation(context.Context, authn.AcceptInvitationInput) (authn.Session, error)
	Unlink(context.Context, string) error
}

type OIDCService interface {
	Start(context.Context, authn.OIDCStartInput) (authn.OIDCStart, error)
	Callback(context.Context, string, string, string, bool) (authn.Session, bool, error)
}

type CSRFService interface {
	VerifyCSRF(context.Context, string, string) error
}

type IdentityHandler struct {
	identity IdentityService
	auth     CSRFService
	oidc     OIDCService
	session  BrowserSessionPolicy
	proxy    TrustedProxyPolicy
	guard    *LoginGuard
}

func NewIdentityHandler(identity IdentityService, auth CSRFService, oidc OIDCService, session BrowserSessionPolicy, proxy TrustedProxyPolicy, guard *LoginGuard) http.Handler {
	handler := &IdentityHandler{identity: identity, auth: auth, oidc: oidc, session: session, proxy: proxy, guard: guard}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/auth/methods", handler.methods)
	mux.HandleFunc("GET /api/v1/auth/account", handler.account)
	mux.HandleFunc("DELETE /api/v1/auth/external-identity", handler.unlink)
	mux.HandleFunc("POST /api/v1/auth/oidc/start", handler.start)
	mux.HandleFunc("GET /api/v1/auth/oidc/callback", handler.callback)
	mux.HandleFunc("POST /api/v1/auth/invitations/accept", handler.acceptInvitation)
	mux.HandleFunc("POST /api/v1/workspaces/{workspace_id}/invitations", handler.createInvitation)
	mux.HandleFunc("GET /auth/complete", handler.complete)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeIdentityError(w, r, authz.ErrNotFound) })
	return noStore(mux)
}

func (handler *IdentityHandler) validate(request *http.Request, mutation bool) error {
	if _, err := handler.proxy.ClientIP(request); err != nil {
		return err
	}
	if err := handler.session.ValidateHost(request); err != nil {
		return err
	}
	if mutation {
		return handler.session.ValidateOrigin(request)
	}
	return nil
}

func (handler *IdentityHandler) authenticatedMutation(request *http.Request) (string, error) {
	if err := handler.validate(request, true); err != nil {
		return "", err
	}
	token, err := handler.session.SessionToken(request)
	if err != nil {
		return "", err
	}
	csrf, err := handler.session.ValidateCSRF(request)
	if err != nil {
		return "", err
	}
	if err := handler.auth.VerifyCSRF(request.Context(), token, csrf); err != nil {
		return "", err
	}
	return token, nil
}

func (handler *IdentityHandler) limited(response http.ResponseWriter, request *http.Request) (func(), error) {
	ip, err := handler.proxy.ClientIP(request)
	if err != nil {
		return nil, err
	}
	release, retry, err := handler.guard.Begin(ip, time.Now())
	if err != nil {
		response.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Seconds())+1)))
	}
	return release, err
}

func (handler *IdentityHandler) methods(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeIdentityError(w, r, ErrMethodNotAllowed)
		return
	}
	if err := handler.validate(r, false); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeIdentityJSON(w, r, http.StatusOK, struct {
		Local        bool `json:"local"`
		Radish       bool `json:"radish"`
		Registration bool `json:"registration"`
	}{Local: true, Radish: handler.oidc != nil, Registration: false})
}

func (handler *IdentityHandler) account(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeIdentityError(w, r, ErrMethodNotAllowed)
		return
	}
	if err := handler.validate(r, false); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	token, err := handler.session.SessionToken(r)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	account, err := handler.identity.Account(r.Context(), token)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeIdentityJSON(w, r, http.StatusOK, struct {
		User   sessionUser `json:"user"`
		Local  bool        `json:"local"`
		Radish bool        `json:"radish_linked"`
		Recent bool        `json:"recent_authentication"`
	}{User: sessionUser{ID: account.User.ID, DisplayName: account.User.DisplayName}, Local: account.HasPassword, Radish: account.RadishLinked, Recent: account.RecentlyAuthenticated})
}

func (handler *IdentityHandler) createInvitation(w http.ResponseWriter, r *http.Request) {
	token, err := handler.authenticatedMutation(r)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	var body struct{}
	if err := decodeJSON(w, r, &body, MaxLoginBodyBytes); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	release, err := handler.limited(w, r)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	defer release()
	invitation, err := handler.identity.CreateInvitation(r.Context(), token, r.PathValue("workspace_id"))
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeIdentityJSON(w, r, http.StatusCreated, struct {
		ID        string    `json:"id"`
		Token     string    `json:"invitation_token"`
		ExpiresAt time.Time `json:"expires_at"`
	}{invitation.ID, invitation.Token, invitation.ExpiresAt})
}

func (handler *IdentityHandler) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	if err := handler.validate(r, true); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	var body struct {
		Invitation  string `json:"invitation_token"`
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := decodeJSON(w, r, &body, MaxLoginBodyBytes); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	input := authn.AcceptInvitationInput{InvitationToken: body.Invitation, Email: body.Email, Password: body.Password, DisplayName: body.DisplayName}
	if _, err := r.Cookie(SessionCookieName); err == nil {
		input.SessionToken, err = handler.authenticatedMutation(r)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
	}
	release, err := handler.limited(w, r)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	defer release()
	session, err := handler.identity.AcceptInvitation(r.Context(), input)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	if input.SessionToken == "" {
		if err := setIdentitySession(w, session); err != nil {
			writeIdentityError(w, r, err)
			return
		}
	}
	if err := writeSessionResponse(w, http.StatusCreated, session.Account); err != nil {
		log.Printf("invitation response failed request_id=%s", RequestID(r.Context()))
	}
}

func (handler *IdentityHandler) unlink(w http.ResponseWriter, r *http.Request) {
	token, err := handler.authenticatedMutation(r)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	if err := handler.identity.Unlink(r.Context(), token); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	for _, cookie := range ExpiredSessionCookies() {
		http.SetCookie(w, cookie)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *IdentityHandler) start(w http.ResponseWriter, r *http.Request) {
	if err := handler.validate(r, true); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	if handler.oidc == nil {
		writeIdentityError(w, r, authn.ErrOIDCUnavailable)
		return
	}
	var body struct {
		Mode        string `json:"mode"`
		Invitation  string `json:"invitation_token"`
		DisplayName string `json:"display_name"`
	}
	if err := decodeJSON(w, r, &body, MaxLoginBodyBytes); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	input := authn.OIDCStartInput{Mode: body.Mode, InvitationToken: body.Invitation, DisplayName: body.DisplayName}
	if input.Mode == "link" {
		token, err := handler.authenticatedMutation(r)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		input.SessionToken = token
	} else if _, err := r.Cookie(SessionCookieName); err == nil {
		// Switching identities must start from an explicit logout, not silently
		// overwrite an authenticated browser's session.
		writeIdentityError(w, r, authn.ErrIdentityConflict)
		return
	}
	release, err := handler.limited(w, r)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	defer release()
	start, err := handler.oidc.Start(r.Context(), input)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oidcCookieName, Value: start.BrowserToken, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: start.ExpiresAt})
	writeIdentityJSON(w, r, http.StatusOK, struct {
		URL string `json:"authorization_url"`
	}{start.AuthorizationURL})
}

func (handler *IdentityHandler) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		writeIdentityError(w, r, ErrMethodNotAllowed)
		return
	}
	if err := handler.validate(r, false); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	finish := func(code string) {
		http.SetCookie(w, &http.Cookie{Name: oidcCookieName, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		http.Redirect(w, r, "/auth/complete?result="+code, http.StatusSeeOther)
	}
	if handler.oidc == nil {
		finish("oidc_unavailable")
		return
	}
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	if queryErr != nil {
		finish("oidc_failed")
		return
	}
	if len(r.URL.RawQuery) > 16384 || len(query["state"]) != 1 || len(query["code"]) > 1 || len(query["error"]) > 1 || (query.Get("code") == "") == (query.Get("error") == "") {
		finish("oidc_failed")
		return
	}
	for key, values := range query {
		if len(values) != 1 || (key != "state" && key != "code" && key != "error" && key != "error_description" && key != "error_uri" && key != "iss" && key != "session_state") {
			finish("oidc_failed")
			return
		}
	}
	cookie, err := r.Cookie(oidcCookieName)
	if err != nil || !validOpaqueToken(cookie.Value) {
		finish("oidc_failed")
		return
	}
	release, err := handler.limited(w, r)
	if err != nil {
		finish("rate_limited")
		return
	}
	defer release()
	session, linked, err := handler.oidc.Callback(r.Context(), query.Get("state"), cookie.Value, query.Get("code"), query.Get("error") != "")
	if err != nil {
		mapping := MapApplicationError(err)
		log.Printf("Radish callback failed request_id=%s code=%s", RequestID(r.Context()), mapping.Code)
		switch {
		case errors.Is(err, authn.ErrInvitationInvalid):
			finish("invitation_invalid")
		case errors.Is(err, authn.ErrRecentAuthentication):
			finish("recent_authentication_required")
		case errors.Is(err, authn.ErrIdentityConflict):
			finish("identity_conflict")
		default:
			finish("oidc_failed")
		}
		return
	}
	if !linked {
		if err := setIdentitySession(w, session); err != nil {
			finish("oidc_failed")
			return
		}
	}
	finish("success")
}

func (handler *IdentityHandler) complete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeIdentityError(w, r, ErrMethodNotAllowed)
		return
	}
	if err := handler.validate(r, false); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	message := "登录已完成。"
	if r.URL.Query().Get("result") != "success" {
		message = "Radish 登录未完成，请返回重试。"
		switch r.URL.Query().Get("result") {
		case "invitation_invalid":
			message = "需要有效的成员邀请，或先用本地账户登录并绑定 Radish。"
		case "recent_authentication_required":
			message = "请重新登录 Nexus 后再绑定 Radish。"
		case "identity_conflict":
			message = "该登录方式已有关联账户，请使用原账户登录。"
		}
	}
	// The user-activated same-origin navigation sends Strict Session cookies;
	// neither the authorization code nor any provider text reaches this page.
	if _, err := w.Write([]byte(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>RadishNexus 登录</title><main><h1>` + message + `</h1><a href="/">进入 RadishNexus</a></main></html>`)); err != nil {
		log.Printf("authentication completion write failed request_id=%s", RequestID(r.Context()))
	}
}

func setIdentitySession(w http.ResponseWriter, session authn.Session) error {
	cookies, err := SessionCookies(session.Token, session.CSRFToken, session.Account.ExpiresAt)
	if err != nil {
		return err
	}
	for _, cookie := range cookies {
		http.SetCookie(w, cookie)
	}
	return nil
}

func writeIdentityJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(append(body, '\n')); err != nil {
		log.Printf("identity response write failed request_id=%s", RequestID(r.Context()))
	}
}

func writeIdentityError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, authn.ErrInvalidSession) {
		for _, cookie := range ExpiredSessionCookies() {
			http.SetCookie(w, cookie)
		}
	}
	if MapApplicationError(err).StatusCode >= 500 {
		log.Printf("identity request failed request_id=%s: %v", RequestID(r.Context()), err)
	}
	if err := WriteError(w, RequestID(r.Context()), err); err != nil {
		log.Printf("identity error response write failed request_id=%s", RequestID(r.Context()))
	}
}
