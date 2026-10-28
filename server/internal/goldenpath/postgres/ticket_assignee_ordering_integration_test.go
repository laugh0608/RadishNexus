//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

// Barriers live only in disposable test databases. Each ordering observes the
// second transaction waiting on a real PostgreSQL lock before releasing the first.
func TestTicketAssignmentMutationOrdering(t *testing.T) {
	for _, kind := range []string{"done", "suspend", "role", "account"} {
		for _, assignmentFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/assignment-first-%v", kind, assignmentFirst), func(t *testing.T) {
				f := contextDatabase(t)
				f.seed(t)
				inv := contextInvocation(f.principal)
				statuses := goldenpath.NewTicketStatusService(f.store, goldenpath.CryptoIDGenerator{})
				if _, err := statuses.ChangeTicketStatus(f.ctx, inv, goldenpath.TicketStatusInput{TicketID: f.ticket, ClientOperationID: "ordering-reopen", BaseStatusRevision: 5, ToStatus: "open", Confirmed: true}); err != nil {
					t.Fatal(err)
				}
				assignments := goldenpath.NewTicketAssignmentService(f.store, goldenpath.CryptoIDGenerator{})
				input := assignmentInput(t, f.ctx, assignments, inv, f.ticket, f.member.ID, "ordering-assignment")
				m := &membershipFixture{contextFixture: f, management: goldenpath.NewManagementService(f.store, goldenpath.CryptoIDGenerator{})}
				var management goldenpath.ManagementInput
				if kind == "suspend" || kind == "role" {
					p := proposal("workspace", f.workspace, "suspend", f.member.ID, "")
					if kind == "role" {
						p = proposal("project", f.project, "role", f.member.ID, "viewer")
					}
					var err error
					management, err = m.prepare(f.owner, p)
					if err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithTimeout(f.ctx, 12*time.Second)
				defer cancel()
				t.Cleanup(cancel)
				assign := func() error { _, err := assignments.SetTicketAssignee(ctx, inv, input); return err }
				change := func() error {
					switch kind {
					case "done":
						_, err := statuses.ChangeTicketStatus(ctx, inv, goldenpath.TicketStatusInput{TicketID: f.ticket, ClientOperationID: "ordering-done", BaseStatusRevision: 6, ToStatus: "done", Confirmed: true})
						return err
					case "suspend", "role":
						_, err := m.management.Apply(ctx, inv, f.owner.Token, management)
						return err
					default:
						_, err := f.pool.Exec(ctx, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, f.member.ID)
						return err
					}
				}
				assigned, changed := make(chan error, 1), make(chan error, 1)
				waitCompeting := func(pattern string) {
					t.Helper()
					waitCtx, stop := context.WithTimeout(ctx, 3*time.Second)
					defer stop()
					// All patterns are local test constants, never supplied by callers.
					waitProjectionLock(t, waitCtx, f.pool, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '`+pattern+`')`)
				}
				if kind == "account" && !assignmentFirst {
					tx, err := f.pool.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(context.Background())
					if _, err = tx.Exec(ctx, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, f.member.ID); err != nil {
						t.Fatal(err)
					}
					go func() { assigned <- assign() }()
					waitCompeting("SELECT status FROM radishnexus.user_accounts%")
					changed <- tx.Commit(ctx)
				} else {
					table := "domain_events"
					if !assignmentFirst && (kind == "suspend" || kind == "role") {
						table = "workspace_configuration_audit"
					}
					release := contextGate(t, f, table)
					if assignmentFirst {
						go func() { assigned <- assign() }()
					} else {
						go func() { changed <- change() }()
					}
					contextWaitGate(t, f)
					if assignmentFirst {
						go func() { changed <- change() }()
					} else {
						go func() { assigned <- assign() }()
					}
					pattern := "%19450001%"
					if kind == "done" {
						pattern = "SELECT%FROM radishnexus.tickets%FOR UPDATE%"
					}
					if kind == "account" {
						pattern = "UPDATE radishnexus.user_accounts%"
					}
					waitCompeting(pattern)
					release()
				}
				if err := <-changed; err != nil {
					t.Fatal("competing mutation", err)
				}
				err := <-assigned
				if assignmentFirst && err != nil || !assignmentFirst && !errors.Is(err, authz.ErrConflict) {
					t.Fatal("wrong serialized assignment result", err)
				}
				value, err := assignments.ReadTicketAssignment(ctx, f.principal, f.ticket)
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if assignmentFirst {
					want = 1
					if value.Assignee == nil || value.Assignee.ID != f.member.ID || value.Assignee.Eligible != (kind == "done") {
						t.Fatal("responsibility or eligibility", value)
					}
				} else if value.Assignee != nil {
					t.Fatal("rejected write left assignee", value)
				}
				if value.AssignmentRevision != want+1 {
					t.Fatal("assignment revision", value)
				}
				if kind == "done" && (value.CanAssign || value.StatusRevision != 7) {
					t.Fatal("terminal state", value)
				}
				var events, receipts, outbox, activities int
				err = f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM radishnexus.domain_events WHERE event_type='ticket.assignee-changed'),(SELECT count(*) FROM radishnexus.collaboration_command_receipts WHERE command_kind='ticket.assignee.set'),(SELECT count(*) FROM radishnexus.outbox_deliveries o JOIN radishnexus.domain_events e USING(event_id) WHERE e.event_type='ticket.assignee-changed'),(SELECT count(*) FROM radishnexus.activity_items a JOIN radishnexus.domain_events e ON e.event_id=a.event_id WHERE e.event_type='ticket.assignee-changed')`).Scan(&events, &receipts, &outbox, &activities)
				if err != nil || events != want || receipts != want || outbox != want || activities != want {
					t.Fatal("non-atomic serialized outcome", events, receipts, outbox, activities, err)
				}
				if assignmentFirst {
					result, err := assignments.SetTicketAssignee(ctx, inv, input)
					if err != nil || result.AssignmentRevision != 2 {
						t.Fatal("historical retry after mutation", result, err)
					}
				}
				if _, err = f.service.Export(ctx, f.principal, f.owner.Token, f.project); err != nil {
					t.Fatal("serialized history no longer portable", err)
				}
			})
		}
	}
}
