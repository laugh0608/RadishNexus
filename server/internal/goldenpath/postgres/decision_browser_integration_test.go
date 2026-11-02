//go:build integration

package postgres_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
)

func TestDecisionRejectionHTTPSBrowser(t *testing.T) {
	statePath := os.Getenv("RADISHNEXUS_DECISION_BROWSER_STATE")
	if statePath == "" {
		t.Skip("explicit browser review required")
	}
	webRoot := os.Getenv("RADISHNEXUS_WEB_ROOT")
	if webRoot == "" {
		t.Fatal("built Web required")
	}
	f := contextDatabase(t)
	f.seed(t)
	management := goldenpath.NewManagementService(f.store, goldenpath.CryptoIDGenerator{})
	for i, proposal := range []goldenpath.ManagementProposal{{ScopeKind: "project", ScopeID: f.project, Action: "role", TargetUserID: f.member.ID, Role: "decider"}, {ScopeKind: "thread", ScopeID: f.thread, Action: "add", TargetUserID: f.member.ID}} {
		plan, e := management.Preview(f.ctx, f.principal, f.owner.Token, proposal)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = management.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, goldenpath.ManagementInput{ManagementProposal: proposal, ClientOperationID: fmt.Sprintf("12345678-1234-4234-8234-%012d", i+1), PlanDigest: plan.Digest, Confirmed: true}); e != nil {
			t.Fatal(e)
		}
	}
	ids := []string{f.proposed}
	for i := 0; i < 3; i++ {
		p, e := f.core.CreateDecisionFromThread(f.ctx, contextInvocation(f.principal), goldenpath.CreateDecisionInput{ThreadID: f.thread, ClientOperationID: fmt.Sprintf("browser-decision-%d", i), Question: fmt.Sprintf("拒绝交互验收 %d：保留理由与来源？", i)})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, p.Decision.ID)
	}
	server, ca := contextHTTPS(t, f, webRoot)
	spki := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	state := map[string]any{"origin": server.URL, "ca_file": ca, "spki": base64.StdEncoding.EncodeToString(spki[:]), "workspace": f.workspace, "project": f.project, "thread": f.thread, "decisions": ids, "accepted": f.decision, "member_id": f.member.ID, "owner_id": f.principal.ID, "member_email": "context.member@example.test", "owner_email": "context.owner@example.test", "database": f.pool.Config().ConnConfig.Database, "done_file": statePath + ".done"}
	raw, e := json.Marshal(state)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(statePath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	fmt.Println("decision browser fixture ready:", statePath)
	deadline := time.Now().Add(40 * time.Minute)
	for time.Now().Before(deadline) {
		raw, e = os.ReadFile(statePath + ".done")
		if e == nil {
			var result struct {
				Passed bool     `json:"passed"`
				Checks []string `json:"checks"`
			}
			if json.Unmarshal(raw, &result) != nil || !result.Passed || len(result.Checks) < 8 {
				t.Fatal("missing browser evidence")
			}
			return
		}
		if !os.IsNotExist(e) {
			t.Fatal(e)
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("browser review timeout")
}
