//go:build browserfixture

package httptransport_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

type browserFixtureService struct {
	mu        sync.Mutex
	active    bool
	expiresAt time.Time
}

func (service *browserFixtureService) Login(_ context.Context, input authn.LoginInput) (authn.Session, error) {
	if input.LoginName != "admin" || input.Password != "browser fixture password" {
		return authn.Session{}, authn.ErrInvalidCredentials
	}
	service.mu.Lock()
	service.active = true
	service.expiresAt = time.Now().UTC().Add(time.Hour)
	account := browserFixtureAccount(service.expiresAt)
	service.mu.Unlock()
	return authn.Session{
		Token:     browserFixtureToken(1),
		CSRFToken: browserFixtureToken(2),
		Account:   account,
	}, nil
}

func (service *browserFixtureService) ResolveSession(_ context.Context, token string) (authn.SessionAccount, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if !service.active || token != browserFixtureToken(1) {
		return authn.SessionAccount{}, authn.ErrInvalidSession
	}
	return browserFixtureAccount(service.expiresAt), nil
}

func (service *browserFixtureService) RevokeSession(_ context.Context, token string, csrfToken string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if !service.active || token != browserFixtureToken(1) || csrfToken != browserFixtureToken(2) {
		return authn.ErrInvalidCSRFToken
	}
	service.active = false
	return nil
}

func TestHTTPSBrowserFixture(t *testing.T) {
	statePath := os.Getenv("RADISHNEXUS_BROWSER_FIXTURE_STATE")
	stopPath := os.Getenv("RADISHNEXUS_BROWSER_FIXTURE_STOP")
	if statePath == "" || stopPath == "" {
		t.Fatal("RADISHNEXUS_BROWSER_FIXTURE_STATE and RADISHNEXUS_BROWSER_FIXTURE_STOP are required")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	publicOrigin := "https://" + listener.Addr().String()
	sessionPolicy, err := httptransport.NewBrowserSessionPolicy(publicOrigin)
	if err != nil {
		t.Fatalf("NewBrowserSessionPolicy() error = %v", err)
	}
	proxyPolicy, err := httptransport.NewTrustedProxyPolicy("127.0.0.0/8")
	if err != nil {
		t.Fatalf("NewTrustedProxyPolicy() error = %v", err)
	}

	root := http.NewServeMux()
	root.Handle("/api/v1/auth/", httptransport.NewAuthHandler(
		&browserFixtureService{},
		sessionPolicy,
		proxyPolicy,
		httptransport.NewLoginGuard(5, time.Minute, 64, 2),
	))
	root.HandleFunc("GET /{$}", browserFixturePage)
	server := httptest.NewUnstartedServer(httptransport.WithRequestID(root))
	server.Listener = listener
	server.StartTLS()
	t.Cleanup(server.Close)

	evilServer := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(response, `<!doctype html><html><body>
<h1>Cross-origin fixture</h1>
<form method="post" action="%s/api/v1/auth/sessions">
<input name="login_name" value="admin">
<input name="password" value="browser fixture password">
<button type="submit">Submit cross-origin login</button>
</form></body></html>`, publicOrigin)
	}))
	t.Cleanup(evilServer.Close)

	state, err := json.Marshal(map[string]string{
		"public_origin": publicOrigin,
		"evil_origin":   evilServer.URL,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(statePath, append(state, '\n'), 0o600); err != nil {
		t.Fatalf("write fixture state: %v", err)
	}

	deadline := time.NewTimer(10 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("browser fixture timed out waiting for stop file")
		case <-ticker.C:
			if _, err := os.Stat(stopPath); err == nil {
				return
			} else if !os.IsNotExist(err) {
				t.Fatalf("inspect fixture stop file: %v", err)
			}
		}
	}
}

func browserFixturePage(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	_, _ = response.Write([]byte(`<!doctype html><html><body>
<h1>RadishNexus authentication browser fixture</h1>
<form id="login-form">
<label>Login <input id="login-name" value="admin"></label>
<label>Password <input id="password" type="password" value="browser fixture password"></label>
<button type="submit">Login</button>
</form>
<button id="resolve" type="button">Resolve session</button>
<button id="wrong-csrf" type="button">Logout with wrong CSRF</button>
<button id="logout" type="button">Logout</button>
<pre id="result">ready</pre>
<script>
const result = document.querySelector('#result');
const show = async response => {
  const body = await response.text();
  result.textContent = response.status + (body ? ' ' + body.trim() : '');
};
document.querySelector('#login-form').addEventListener('submit', async event => {
  event.preventDefault();
  await show(await fetch('/api/v1/auth/sessions', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({
      login_name: document.querySelector('#login-name').value,
      password: document.querySelector('#password').value,
    }),
  }));
});
document.querySelector('#resolve').addEventListener('click', async () => {
  await show(await fetch('/api/v1/auth/session'));
});
const csrf = () => document.cookie.split('; ').find(value => value.startsWith('__Host-radishnexus-csrf='))?.split('=')[1] || '';
document.querySelector('#wrong-csrf').addEventListener('click', async () => {
  await show(await fetch('/api/v1/auth/session', {method: 'DELETE', headers: {'X-CSRF-Token': 'wrong'}}));
});
document.querySelector('#logout').addEventListener('click', async () => {
  await show(await fetch('/api/v1/auth/session', {method: 'DELETE', headers: {'X-CSRF-Token': csrf()}}));
});
</script></body></html>`))
}

func browserFixtureToken(value byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{value}, 32))
}

func browserFixtureAccount(expiresAt time.Time) authn.SessionAccount {
	return authn.SessionAccount{
		User:       authn.User{ID: "usr_browser", DisplayName: "Browser Admin"},
		Workspaces: []authn.Workspace{{ID: "wrk_browser", Name: "Browser Workspace", Role: "owner"}},
		ExpiresAt:  expiresAt,
	}
}
