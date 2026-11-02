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
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func TestDecisionSupersessionHTTPSBrowser(t *testing.T) {
	path := os.Getenv("RADISHNEXUS_SUPERSESSION_BROWSER_STATE")
	if path == "" {
		t.Skip("explicit browser review required")
	}
	root := os.Getenv("RADISHNEXUS_WEB_ROOT")
	if root == "" {
		t.Fatal("frozen Web build required")
	}
	f := contextDatabase(t)
	f.seed(t)
	management := goldenpath.NewManagementService(f.store, goldenpath.CryptoIDGenerator{})
	for i, proposal := range []goldenpath.ManagementProposal{{ScopeKind: "project", ScopeID: f.project, Action: "role", TargetUserID: f.member.ID, Role: "decider"}, {ScopeKind: "thread", ScopeID: f.thread, Action: "add", TargetUserID: f.member.ID}} {
		plan, e := management.Preview(f.ctx, f.principal, f.owner.Token, proposal)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = management.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, goldenpath.ManagementInput{ManagementProposal: proposal, ClientOperationID: fmt.Sprintf("12345678-1234-4234-8234-%012d", i+101), PlanDigest: plan.Digest, Confirmed: true}); e != nil {
			t.Fatal(e)
		}
	}
	counter := 0
	create := func(question string, accepted bool) string {
		counter++
		p, e := f.core.CreateDecisionFromThread(f.ctx, contextInvocation(f.principal), goldenpath.CreateDecisionInput{ThreadID: f.thread, ClientOperationID: fmt.Sprintf("browser-proposal-%d", counter), Question: question})
		if e != nil {
			t.Fatal(e)
		}
		if accepted {
			if _, e = f.core.AcceptDecision(f.ctx, contextInvocation(f.principal), goldenpath.AcceptDecisionInput{DecisionID: p.Decision.ID, ClientOperationID: fmt.Sprintf("browser-accept-%d", counter), Outcome: "采用已核对的方案：" + question, Rationale: "保留原讨论与执行来源，人工确认此结论。"}); e != nil {
				t.Fatal(e)
			}
		}
		return p.Decision.ID
	}
	pairs := []map[string]string{}
	for i := 0; i < 6; i++ {
		old := f.decision
		if i == 5 {
			old = f.proposed
		} else if i != 0 {
			old = create(fmt.Sprintf("浏览器旧结论 %d", i), true)
		}
		replacement := create(fmt.Sprintf("浏览器新结论 %d", i), true)
		view, e := f.core.GetNexusView(f.ctx, f.principal, entityref.Ref{Type: "decision", ID: old})
		if e != nil {
			t.Fatal(e)
		}
		pairs = append(pairs, map[string]string{"old": old, "replacement": replacement, "old_title": view.Current.Title, "replacement_title": fmt.Sprintf("浏览器新结论 %d", i)})
	}
	rival := create("浏览器竞争替代结论", true)
	continuation := create("浏览器链延伸结论", true)
	for i := 0; i < 12; i++ {
		create(fmt.Sprintf("保留待确认条目 %02d", i), false)
	}
	server, ca := contextHTTPS(t, f, root)
	spki := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	state := map[string]any{"origin": server.URL, "ca_file": ca, "spki": base64.StdEncoding.EncodeToString(spki[:]), "workspace": f.workspace, "project": f.project, "thread": f.thread, "member_id": f.member.ID, "member_email": "context.member@example.test", "pairs": pairs, "rival": rival, "continuation": continuation, "ticket": f.ticket, "document": f.document, "database": f.pool.Config().ConnConfig.Database, "database_port": f.pool.Config().ConnConfig.Port, "done_file": path + ".done"}
	raw, e := json.Marshal(state)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	fmt.Println("supersession browser fixture ready:", path)
	deadline := time.Now().Add(30 * time.Minute)
	for {
		data, err := os.ReadFile(path + ".done")
		if err == nil {
			var result struct {
				Passed bool     `json:"passed"`
				Checks []string `json:"checks"`
			}
			if json.Unmarshal(data, &result) != nil || !result.Passed || len(result.Checks) < 12 {
				t.Fatal("browser evidence missing or failed")
			}
			break
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("browser review timeout")
		}
		time.Sleep(250 * time.Millisecond)
	}
	for i, pair := range pairs {
		var status string
		var revision int
		if e = f.pool.QueryRow(f.ctx, `SELECT status,decision_revision FROM radishnexus.decisions WHERE id=$1`, pair["old"]).Scan(&status, &revision); e != nil {
			t.Fatal(e)
		}
		wantStatus, wantRevision := "superseded", 3
		if i == 3 {
			wantStatus, wantRevision = "accepted", 2
		}
		if status != wantStatus || revision != wantRevision {
			t.Fatal("browser old result", i, status, revision)
		}
		if e = f.pool.QueryRow(f.ctx, `SELECT status,decision_revision FROM radishnexus.decisions WHERE id=$1`, pair["replacement"]).Scan(&status, &revision); e != nil {
			t.Fatal(e)
		}
		wantStatus, wantRevision = "accepted", 3
		if i == 0 {
			wantStatus, wantRevision = "superseded", 4
		}
		if i == 2 || i == 3 {
			wantRevision = 2
		}
		if status != wantStatus || revision != wantRevision {
			t.Fatal("browser replacement result", i, status, revision)
		}
	}
	var events, activity, revision int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*),(SELECT count(*) FROM radishnexus.activity_items WHERE activity_type='decision.superseded') FROM radishnexus.domain_events WHERE event_type='decision.superseded'`).Scan(&events, &activity); e != nil || events != 6 || activity != 12 {
		t.Fatal("browser effect count", events, activity, e)
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT status_revision FROM radishnexus.tickets WHERE id=$1`, f.ticket).Scan(&revision); e != nil || revision < 6 {
		t.Fatal("original execution not continued", revision, e)
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT current_revision FROM radishnexus.documents WHERE id=$1`, f.document).Scan(&revision); e != nil || revision < 4 {
		t.Fatal("original document not saved", revision, e)
	}
}
