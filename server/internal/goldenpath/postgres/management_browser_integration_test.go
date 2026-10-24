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

// The review uses actual production handlers, PostgreSQL and built Web assets.
// It is opt-in, with no permanent credentials, trust changes or shared profile.
func TestMembershipHTTPSBrowser(t *testing.T) {
	statePath := os.Getenv("RADISHNEXUS_MEMBERSHIP_BROWSER_STATE")
	if statePath == "" {
		t.Skip("explicit browser review required")
	}
	webRoot := os.Getenv("RADISHNEXUS_WEB_ROOT")
	if webRoot == "" {
		t.Fatal("built Web required")
	}
	f := newMembershipFixture(t)
	server, _ := contextHTTPS(t, f.contextFixture, webRoot)
	state := map[string]any{"origin": server.URL, "workspace": f.workspace, "project": f.project, "channel": f.channel, "thread": f.thread, "owner_id": f.principal.ID, "member_id": f.members[0].Account.User.ID, "other_id": f.members[1].Account.User.ID, "owner_email": "context.owner@example.test", "member_email": "membership-0@example.test", "done_file": statePath + ".done"}
	raw, e := json.Marshal(state)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(statePath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	fmt.Println("membership browser fixture ready:", statePath)
	deadline := time.Now().Add(40 * time.Minute)
	for {
		raw, e = os.ReadFile(statePath + ".done")
		if e == nil {
			var evidence struct {
				Passed bool     `json:"passed"`
				Checks []string `json:"checks"`
			}
			if e = json.Unmarshal(raw, &evidence); e != nil || !evidence.Passed || len(evidence.Checks) < 6 {
				t.Fatal("missing browser evidence", e)
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
	var revision int64
	var status string
	// The fixture setup context has a shorter budget than an interactive review.
	// Use a fresh, bounded context for the final persisted-state assertions.
	verifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e = f.pool.QueryRow(verifyCtx, `SELECT lifecycle_revision,status FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, f.workspace, f.members[0].Account.User.ID).Scan(&revision, &status); e != nil || revision < 5 || status != "active" {
		t.Fatal("browser did not complete pause/resume/exit/readmission", revision, status, e)
	}
	var grants int
	if e = f.pool.QueryRow(verifyCtx, `SELECT count(*) FROM radishnexus.project_memberships WHERE workspace_id=$1 AND user_id=$2`, f.workspace, f.members[0].Account.User.ID).Scan(&grants); e != nil || grants != 0 {
		t.Fatal("readmission restored old project rights", grants, e)
	}
}
