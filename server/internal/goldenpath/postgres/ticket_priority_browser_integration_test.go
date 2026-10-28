//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
)

func TestTicketPriorityHTTPSBrowser(t *testing.T) {
	statePath := os.Getenv("RADISHNEXUS_PRIORITY_BROWSER_STATE")
	if statePath == "" {
		t.Skip("explicit browser review required")
	}
	webRoot := os.Getenv("RADISHNEXUS_WEB_ROOT")
	if webRoot == "" {
		t.Fatal("built Web required")
	}
	f := contextDatabase(t)
	f.seed(t)
	inv := contextInvocation(f.principal)
	contextSQL(t, f, `INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_priority_other','第二合成工作区')`)
	contextSQL(t, f, `INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES('wrk_priority_other',$1,'active','owner')`, f.principal.ID)
	// Pagination facts use formal commands. The separately reviewed Ticket is
	// created from the accepted Decision through the browser itself.
	priorities := goldenpath.NewTicketPriorityService(f.store, goldenpath.CryptoIDGenerator{})
	assignments := goldenpath.NewTicketAssignmentService(f.store, goldenpath.CryptoIDGenerator{})
	for i := 0; i < 26; i++ {
		result, err := f.core.CreateTicketFromDecision(f.ctx, inv, goldenpath.CreateTicketInput{DecisionID: f.decision, ClientOperationID: fmt.Sprintf("priority-browser-page-%02d", i), Title: fmt.Sprintf("分页验收 %02d · 合成团队待处理工单", i)})
		if err != nil {
			t.Fatal(err)
		}
		ticket := result.Ticket.ID
		if _, err = priorities.SetTicketPriority(f.ctx, inv, priorityInput(t, f.ctx, priorities, inv, ticket, "high", "fixture-priority")); err != nil {
			t.Fatal(err)
		}
		if _, err = assignments.SetTicketAssignee(f.ctx, inv, assignmentInput(t, f.ctx, assignments, inv, ticket, f.principal.ID, "fixture-assignee")); err != nil {
			t.Fatal(err)
		}
	}
	server, _ := contextHTTPS(t, f, webRoot)
	state := map[string]any{"other_workspace": "wrk_priority_other", "origin": server.URL, "workspace": f.workspace, "project": f.project, "channel": f.channel, "decision": f.decision, "owner_id": f.principal.ID, "member_id": f.member.ID, "owner_email": "context.owner@example.test", "member_email": "context.member@example.test", "database": f.pool.Config().ConnConfig.Database, "done_file": statePath + ".done"}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(statePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	fmt.Println("priority browser fixture ready:", statePath)
	deadline := time.Now().Add(40 * time.Minute)
	var result struct {
		Passed   bool     `json:"passed"`
		Checks   []string `json:"checks"`
		TicketID string   `json:"ticket_id"`
	}
	for {
		raw, err = os.ReadFile(statePath + ".done")
		if err == nil {
			if json.Unmarshal(raw, &result) != nil || !result.Passed || len(result.Checks) < 8 {
				t.Fatal("missing browser evidence")
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var count, revision int
	if err = f.pool.QueryRow(ctx, `SELECT priority_revision,(SELECT count(*) FROM radishnexus.domain_events e WHERE e.primary_entity_id=t.id AND e.event_type='ticket.priority-changed') FROM radishnexus.tickets t WHERE t.workspace_id=$1 AND t.id=$2`, f.workspace, result.TicketID).Scan(&revision, &count); err != nil || count < 4 || revision != count+1 {
		t.Fatal("browser priority history inconsistent", revision, count, err)
	}
}
