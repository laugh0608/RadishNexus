package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const MaxLoginBodyBytes int64 = 4 * 1024

type SessionService interface {
	Login(context.Context, authn.LoginInput) (authn.Session, error)
	ResolveSession(context.Context, string) (authn.SessionAccount, error)
	RevokeSession(context.Context, string, string) error
}

type AuthHandler struct {
	service SessionService
	session BrowserSessionPolicy
	proxy   TrustedProxyPolicy
	guard   *LoginGuard
	clock   func() time.Time
}

type loginRequest struct {
	LoginName string `json:"login_name"`
	Password  string `json:"password"`
}

type sessionResponse struct {
	User       sessionUser        `json:"user"`
	Workspaces []sessionWorkspace `json:"workspaces"`
	ExpiresAt  time.Time          `json:"expires_at"`
}

type sessionUser struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type sessionWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

func NewAuthHandler(
	service SessionService,
	session BrowserSessionPolicy,
	proxy TrustedProxyPolicy,
	guard *LoginGuard,
) http.Handler {
	handler := &AuthHandler{
		service: service,
		session: session,
		proxy:   proxy,
		guard:   guard,
		clock:   time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/sessions", handler.login)
	mux.HandleFunc("GET /api/v1/auth/session", handler.resolveSession)
	mux.HandleFunc("DELETE /api/v1/auth/session", handler.logout)
	mux.HandleFunc("/api/v1/auth/sessions", handler.methodNotAllowed)
	mux.HandleFunc("/api/v1/auth/session", handler.methodNotAllowed)
	mux.HandleFunc("/api/v1/auth/", handler.notFound)
	mux.HandleFunc("/api/v1/auth", handler.notFound)
	return noStore(mux)
}

func (handler *AuthHandler) login(response http.ResponseWriter, request *http.Request) {
	clientIP, err := handler.proxy.ClientIP(request)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	if err := handler.session.ValidateHost(request); err != nil {
		handler.writeError(response, request, err)
		return
	}
	if err := handler.session.ValidateOrigin(request); err != nil {
		handler.writeError(response, request, err)
		return
	}

	var input loginRequest
	if err := decodeJSON(response, request, &input, MaxLoginBodyBytes); err != nil {
		handler.writeError(response, request, err)
		return
	}
	release, retryAfter, err := handler.guard.Begin(clientIP, handler.clock())
	if err != nil {
		seconds := int64((retryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		response.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		handler.writeError(response, request, err)
		return
	}
	defer release()

	session, err := handler.service.Login(request.Context(), authn.LoginInput{
		LoginName: input.LoginName,
		Password:  input.Password,
	})
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	cookies, err := SessionCookies(session.Token, session.CSRFToken, session.Account.ExpiresAt)
	if err != nil {
		handler.writeError(response, request, fmt.Errorf("create browser session cookies: %w", err))
		return
	}
	for _, cookie := range cookies {
		http.SetCookie(response, cookie)
	}
	if err := writeSessionResponse(response, http.StatusCreated, session.Account); err != nil {
		log.Printf("write login response request_id=%s: %v", RequestID(request.Context()), err)
	}
}

func (handler *AuthHandler) resolveSession(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		handler.methodNotAllowed(response, request)
		return
	}
	if _, err := handler.proxy.ClientIP(request); err != nil {
		handler.writeError(response, request, err)
		return
	}
	if err := handler.session.ValidateHost(request); err != nil {
		handler.writeError(response, request, err)
		return
	}
	token, err := handler.session.SessionToken(request)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	account, err := handler.service.ResolveSession(request.Context(), token)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	if err := writeSessionResponse(response, http.StatusOK, account); err != nil {
		log.Printf("write session response request_id=%s: %v", RequestID(request.Context()), err)
	}
}

func (handler *AuthHandler) methodNotAllowed(response http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/api/v1/auth/sessions":
		response.Header().Set("Allow", http.MethodPost)
	case "/api/v1/auth/session":
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodDelete)
	}
	handler.writeError(response, request, ErrMethodNotAllowed)
}

func (handler *AuthHandler) notFound(response http.ResponseWriter, request *http.Request) {
	handler.writeError(response, request, authz.ErrNotFound)
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(response, request)
	})
}

func (handler *AuthHandler) logout(response http.ResponseWriter, request *http.Request) {
	if _, err := handler.proxy.ClientIP(request); err != nil {
		handler.writeError(response, request, err)
		return
	}
	if err := handler.session.ValidateHost(request); err != nil {
		handler.writeError(response, request, err)
		return
	}
	token, err := handler.session.SessionToken(request)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	csrfToken, err := handler.session.ValidateCSRF(request)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	if err := handler.service.RevokeSession(request.Context(), token, csrfToken); err != nil {
		handler.writeError(response, request, err)
		return
	}
	for _, cookie := range ExpiredSessionCookies() {
		http.SetCookie(response, cookie)
	}
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (handler *AuthHandler) writeError(response http.ResponseWriter, request *http.Request, err error) {
	if MapApplicationError(err).StatusCode == http.StatusInternalServerError {
		log.Printf("public authentication request failed request_id=%s: %v", RequestID(request.Context()), err)
	}
	if writeErr := WriteError(response, RequestID(request.Context()), err); writeErr != nil {
		log.Printf("write public authentication error: %v", writeErr)
	}
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any, limit int64) error {
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || len(parameters) > 1 {
		return ErrUnsupportedMediaType
	}
	if charset, exists := parameters["charset"]; exists && !strings.EqualFold(charset, "utf-8") {
		return ErrUnsupportedMediaType
	}
	if request.ContentLength > limit {
		return ErrPayloadTooLarge
	}
	request.Body = http.MaxBytesReader(response, request.Body, limit)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return ErrPayloadTooLarge
		}
		return fmt.Errorf("%w: malformed JSON body", authz.ErrInvalid)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return ErrPayloadTooLarge
		}
		return fmt.Errorf("%w: request body must contain one JSON object", authz.ErrInvalid)
	}
	return nil
}

func writeSessionResponse(response http.ResponseWriter, status int, account authn.SessionAccount) error {
	workspaces := make([]sessionWorkspace, len(account.Workspaces))
	for index, workspace := range account.Workspaces {
		workspaces[index] = sessionWorkspace{ID: workspace.ID, Name: workspace.Name, Role: workspace.Role}
	}
	body, err := json.Marshal(sessionResponse{
		User: sessionUser{
			ID:          account.User.ID,
			DisplayName: account.User.DisplayName,
		},
		Workspaces: workspaces,
		ExpiresAt:  account.ExpiresAt.UTC(),
	})
	if err != nil {
		return fmt.Errorf("marshal session response: %w", err)
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	if _, err := response.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("write session response: %w", err)
	}
	return nil
}
