//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestTicketAssignmentHTTPSBrowser(t *testing.T) {
	statePath := os.Getenv("RADISHNEXUS_ASSIGNMENT_BROWSER_STATE")
	if statePath == "" {
		t.Skip("explicit browser review required")
	}
	webRoot := os.Getenv("RADISHNEXUS_WEB_ROOT")
	if webRoot == "" {
		t.Fatal("built Web required")
	}
	f := contextDatabase(t)
	f.seed(t)
	// Only identity and grant fixtures use SQL; collaboration facts use formal commands.
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("usr_candidate_%02d", i)
		contextSQL(t, f, `INSERT INTO radishnexus.users(id,display_name) VALUES($1,$2)`, id, fmt.Sprintf("合成候选成员 %02d · 长名称测试", i))
		contextSQL(t, f, `INSERT INTO radishnexus.user_accounts(user_id,status,created_at) VALUES($1,'active',clock_timestamp())`, id)
		contextSQL(t, f, `INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES($1,$2,'active','member')`, f.workspace, id)
		contextSQL(t, f, `INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role) VALUES($1,$2,$3,'contributor')`, f.workspace, f.project, id)
	}
	server, _ := contextHTTPS(t, f, webRoot)
	state := map[string]any{"origin": server.URL, "workspace": f.workspace, "project": f.project, "channel": f.channel, "decision": f.decision, "ticket": f.ticket, "owner_id": f.principal.ID, "member_id": f.member.ID, "owner_email": "context.owner@example.test", "member_email": "context.member@example.test", "database": f.pool.Config().ConnConfig.Database, "done_file": statePath + ".done"}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(statePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	fmt.Println("assignment browser fixture ready:", statePath)
	deadline := time.Now().Add(40 * time.Minute)
	for {
		raw, err = os.ReadFile(statePath + ".done")
		if err == nil {
			var result struct {
				Passed bool     `json:"passed"`
				Checks []string `json:"checks"`
			}
			if json.Unmarshal(raw, &result) != nil || !result.Passed || len(result.Checks) < 6 {
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
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.domain_events WHERE event_type='ticket.assignee-changed'`).Scan(&count); err != nil || count < 4 {
		t.Fatal("browser did not change responsibility", count, err)
	}
}
