//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/laugh0608/RadishNexus/server/db"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
	"github.com/laugh0608/RadishNexus/server/internal/platform/realtime"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type contextNotifier struct{ hub *realtime.Hub }

func (n contextNotifier) NotifyMessageCreated(v goldenpath.MessageCreatedNotification) {
	n.hub.NotifyMessageCreated(realtime.MessageNotification{WorkspaceID: v.WorkspaceID, ChannelID: v.ChannelID, MessageID: v.MessageID})
}
func contextHTTPS(t *testing.T, f *contextFixture, webRoot string) (*httptest.Server, string) {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	origin := "https://" + server.Listener.Addr().String()
	policy, e := httptransport.NewBrowserSessionPolicy(origin)
	if e != nil {
		t.Fatal(e)
	}
	proxy, e := httptransport.NewTrustedProxyPolicy("127.0.0.0/8")
	if e != nil {
		t.Fatal(e)
	}
	guard := httptransport.NewLoginGuard(5, time.Minute, 64, 2)
	identity := httptransport.NewIdentityHandler(f.identity, f.auth, nil, policy, proxy, guard)
	auth := httptransport.NewAuthHandler(f.auth, policy, proxy, guard, identity)
	config, e := realtime.DefaultConfig()
	if e != nil {
		t.Fatal(e)
	}
	hub, e := realtime.NewHub(config)
	if e != nil {
		t.Fatal(e)
	}
	core := goldenpath.NewService(f.store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{}, goldenpath.WithMessageCreatedNotifier(contextNotifier{hub}))
	mux := http.NewServeMux()
	setup, e := authn.NewSetupService(f.auth, authpostgres.New(f.pool), "")
	if e != nil {
		t.Fatal(e)
	}
	ready, e := db.NewReadinessChecker(f.pool)
	if e != nil {
		t.Fatal(e)
	}
	mux.Handle("/api/v1/setup", httptransport.NewSetupHandler(setup, ready, policy, proxy, guard))
	mux.Handle("/api/v1/auth", auth)
	mux.Handle("/api/v1/auth/", auth)
	mux.Handle("/api/v1/workspaces/{workspace_id}/invitations", identity)
	mux.Handle("/auth/complete", identity)
	discovery := httptransport.NewDiscoveryHandler(f.auth, goldenpath.NewDiscoveryService(f.store), policy, proxy)
	configuration := httptransport.NewConfigurationHandler(f.auth, f.configuration, policy, proxy)
	httptransport.RegisterConfigurationRoutes(mux, discovery, configuration)
	mux.Handle("/api/v1/workspaces/{workspace_id}/projects/", discovery)
	httptransport.RegisterContextRoutes(mux, httptransport.NewContextHandler(f.auth, f.service, policy, proxy))
	httptransport.RegisterManagementRoutes(mux, httptransport.NewManagementHandler(f.auth, goldenpath.NewManagementService(f.store, goldenpath.CryptoIDGenerator{}), policy, proxy))
	httptransport.RegisterOwnerTransferRoutes(mux, httptransport.NewOwnerTransferHandler(f.auth, goldenpath.NewOwnerTransferService(f.store, goldenpath.CryptoIDGenerator{}), policy, proxy))
	messages := httptransport.NewChannelMessagesHandler(f.auth, core, policy, proxy)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages", messages)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages/", messages)
	mux.Handle("/api/v1/workspaces/{workspace_id}/channels/{channel_id}/events", httptransport.NewChannelEventsHandler(f.auth, core, hub, policy, proxy))
	collaboration := httptransport.NewCollaborationHandler(f.auth, core, policy, proxy)
	for _, kind := range []string{"threads", "decisions", "tickets"} {
		mux.Handle("/api/v1/workspaces/{workspace_id}/"+kind+"/", collaboration)
	}
	httptransport.RegisterDocumentRoutes(mux, httptransport.NewDocumentHandler(f.auth, f.documents, policy, proxy))
	httptransport.RegisterTicketComponentRoutes(mux, httptransport.NewTicketComponentHandler(f.auth, goldenpath.NewTicketComponentService(f.store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{}), policy, proxy))
	httptransport.RegisterTicketStatusRoutes(mux, httptransport.NewTicketStatusHandler(f.auth, goldenpath.NewTicketStatusService(f.store, goldenpath.CryptoIDGenerator{}), policy, proxy))
	mux.Handle("/api/v1/workspaces", httptransport.NewDeploymentNexusViewHandler(f.auth, core, policy, proxy))
	if webRoot != "" {
		web, e := httptransport.NewWebAppHandler(webRoot)
		if e != nil {
			t.Fatal(e)
		}
		mux.Handle("/", web)
	}
	server.Config.Handler = httptransport.WithRequestID(mux)
	server.StartTLS()
	t.Cleanup(func() { hub.Shutdown(); server.CloseClientConnections(); server.Close() })
	ca := filepath.Join(t.TempDir(), "fixture-ca.pem")
	if e = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); e != nil {
		t.Fatal(e)
	}
	return server, ca
}
func contextCLI(t *testing.T, args ...string) []byte {
	t.Helper()
	binary := os.Getenv("RADISHNEXUS_CONTEXT_CLI")
	if binary == "" {
		t.Skip("RADISHNEXUS_CONTEXT_CLI required for real CLI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	output, e := command.CombinedOutput()
	if e != nil {
		t.Fatalf("context CLI failed: %v: %s", e, output)
	}
	return output
}
func contextCredentialFile(t *testing.T, email string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "login.json")
	if e := os.WriteFile(name, cp.Encode(map[string]string{"email": email, "password": contextTestPassword}), 0600); e != nil {
		t.Fatal(e)
	}
	return name
}
func contextHTTPRead(t *testing.T, s *httptest.Server, session authn.Session, path string, want int) []byte {
	t.Helper()
	request, e := http.NewRequest("GET", s.URL+path, nil)
	if e != nil {
		t.Fatal(e)
	}
	request.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: session.Token})
	response, e := s.Client().Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	if response.StatusCode != want {
		t.Fatalf("HTTP read status %d, expected %d: %s", response.StatusCode, want, raw)
	}
	if response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatal("context HTTP response cacheable")
	}
	return raw
}
func TestContextCLIHTTPSAndBrowser(t *testing.T) {
	if os.Getenv("RADISHNEXUS_CONTEXT_CLI") == "" {
		t.Skip("RADISHNEXUS_CONTEXT_CLI required")
	}
	source := contextDatabase(t)
	source.seed(t)
	target := contextDatabase(t)
	outsiderSession, outsider := target.invite(t, "context.outsider@example.test")
	webRoot := os.Getenv("RADISHNEXUS_WEB_ROOT")
	sourceHTTP, sourceCA := contextHTTPS(t, source, webRoot)
	targetHTTP, targetCA := contextHTTPS(t, target, webRoot)
	output := t.TempDir()
	bundlePath := filepath.Join(output, "project.nexus")
	planPath := filepath.Join(output, "plan.json")
	sourceLogin := contextCredentialFile(t, "context.owner@example.test")
	targetLogin := contextCredentialFile(t, "context.owner@example.test")
	contextCLI(t, "export", "--origin", sourceHTTP.URL, "--ca", sourceCA, "--credentials-file", sourceLogin, "--workspace", source.workspace, "--project", source.project, "--output", bundlePath)
	contextCLI(t, "inspect", "--input", bundlePath)
	contextCLI(t, "prepare", "--origin", targetHTTP.URL, "--ca", targetCA, "--credentials-file", targetLogin, "--workspace", target.workspace, "--input", bundlePath, "--operation", contextOperation, "--output", planPath)
	raw, e := os.ReadFile(planPath)
	if e != nil {
		t.Fatal(e)
	}
	var plan cp.Plan
	if e = cp.Decode(raw, &plan); e != nil {
		t.Fatal(e)
	}
	contextCLI(t, "apply", "--origin", targetHTTP.URL, "--ca", targetCA, "--credentials-file", targetLogin, "--workspace", target.workspace, "--input", bundlePath, "--plan", planPath, "--confirm-digest", plan.PlanDigest)
	resultRaw := contextCLI(t, "result", "--origin", targetHTTP.URL, "--ca", targetCA, "--credentials-file", targetLogin, "--workspace", target.workspace, "--operation", contextOperation)
	var result cp.Result
	if e = cp.Decode(resultRaw, &result); e != nil || result.Status != "applied" {
		t.Fatal("CLI result did not confirm apply", e)
	}
	base := "/api/v1/workspaces/" + target.workspace
	ids := map[string]string{}
	for kind, id := range map[string]string{"project": source.project, "channel": source.channel, "thread": source.thread, "decision": source.decision, "ticket": source.ticket, "document": source.document, "proposed": source.proposed} {
		mappingKind := kind
		if kind == "proposed" {
			mappingKind = "decision"
		}
		ids[kind] = contextMapping(t, plan, mappingKind, id)
	}
	for _, kind := range []string{"thread", "decision", "ticket", "document"} {
		raw := contextHTTPRead(t, targetHTTP, target.owner, base+"/"+kind+"s/"+ids[kind]+"/nexus-view", 200)
		if kind != "thread" && !strings.Contains(string(raw), `"imported":true`) {
			t.Fatal("public timeline omitted import source", kind)
		}
		contextHTTPRead(t, targetHTTP, outsiderSession, base+"/"+kind+"s/"+ids[kind]+"/nexus-view", 404)
	}
	for _, kind := range []string{"project", "channel", "thread", "decision", "ticket", "document"} {
		raw := contextHTTPRead(t, targetHTTP, target.owner, base+"/context-source/"+kind+"/"+ids[kind], 200)
		if !strings.Contains(string(raw), `"imported":true`) {
			t.Fatal("source projection absent")
		}
		contextHTTPRead(t, targetHTTP, outsiderSession, base+"/context-source/"+kind+"/"+ids[kind], 404)
	}
	var activeSessions int
	if e = target.pool.QueryRow(target.ctx, `SELECT count(*) FROM radishnexus.user_sessions WHERE user_id=$1 AND revoked_at IS NULL`, target.principal.ID).Scan(&activeSessions); e != nil || activeSessions != 1 {
		t.Fatal("CLI left active sessions", e)
	}
	if statePath := os.Getenv("RADISHNEXUS_CONTEXT_BROWSER_STATE"); statePath != "" {
		if webRoot == "" {
			t.Fatal("browser fixture requires built Web")
		}
		state := map[string]any{"source_origin": sourceHTTP.URL, "target_origin": targetHTTP.URL, "source_workspace": source.workspace, "workspace": target.workspace, "owner_email": "context.owner@example.test", "outsider_email": "context.outsider@example.test", "outsider_id": outsider.ID, "ids": ids, "done_file": statePath + ".done"}
		if e = os.WriteFile(statePath, cp.Encode(state), 0600); e != nil {
			t.Fatal(e)
		}
		fmt.Println("context browser fixture ready:", statePath)
		deadline := time.Now().Add(25 * time.Minute)
		for {
			raw, e := os.ReadFile(statePath + ".done")
			if e == nil {
				var evidence struct {
					Passed bool     `json:"passed"`
					Checks []string `json:"checks"`
				}
				if e = json.Unmarshal(raw, &evidence); e != nil || !evidence.Passed || len(evidence.Checks) == 0 {
					t.Fatal("browser evidence missing or failed")
				}
				break
			}
			if !os.IsNotExist(e) {
				t.Fatal(e)
			}
			if time.Now().After(deadline) {
				t.Fatal("browser review timeout")
			}
			time.Sleep(250 * time.Millisecond)
		}
		var ticketRevision, documentRevision int
		if e = target.pool.QueryRow(context.Background(), `SELECT status_revision FROM radishnexus.tickets WHERE id=$1`, ids["ticket"]).Scan(&ticketRevision); e != nil || ticketRevision < 6 {
			t.Fatal("browser did not continue Ticket", e)
		}
		if e = target.pool.QueryRow(context.Background(), `SELECT current_revision FROM radishnexus.documents WHERE id=$1`, ids["document"]).Scan(&documentRevision); e != nil || documentRevision < 4 {
			t.Fatal("browser did not save Document", e)
		}
	}
}
