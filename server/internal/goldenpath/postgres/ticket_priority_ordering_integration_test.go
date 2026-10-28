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
func TestTicketPriorityMutationOrdering(t *testing.T) {
	for _, kind := range []string{"done", "suspend"} {
		for _, priorityFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/priority-first-%v", kind, priorityFirst), func(t *testing.T) {
				f := contextDatabase(t)
				f.seed(t)
				inv := contextInvocation(f.member)
				statuses := goldenpath.NewTicketStatusService(f.store, goldenpath.CryptoIDGenerator{})
				if _, err := statuses.ChangeTicketStatus(f.ctx, inv, goldenpath.TicketStatusInput{TicketID: f.ticket, ClientOperationID: "ordering-reopen", BaseStatusRevision: 5, ToStatus: "open", Confirmed: true}); err != nil {
					t.Fatal(err)
				}
				priorities := goldenpath.NewTicketPriorityService(f.store, goldenpath.CryptoIDGenerator{})
				input := priorityInput(t, f.ctx, priorities, inv, f.ticket, "high", "ordering-priority")
				m := &membershipFixture{contextFixture: f, management: goldenpath.NewManagementService(f.store, goldenpath.CryptoIDGenerator{})}
				var management goldenpath.ManagementInput
				if kind == "suspend" {
					p := proposal("workspace", f.workspace, "suspend", f.member.ID, "")

					var err error
					management, err = m.prepare(f.owner, p)
					if err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithTimeout(f.ctx, 12*time.Second)
				defer cancel()
				t.Cleanup(cancel)
				setPriority := func() error { _, err := priorities.SetTicketPriority(ctx, inv, input); return err }
				change := func() error {
					switch kind {
					case "done":
						_, err := statuses.ChangeTicketStatus(ctx, inv, goldenpath.TicketStatusInput{TicketID: f.ticket, ClientOperationID: "ordering-done", BaseStatusRevision: 6, ToStatus: "done", Confirmed: true})
						return err
					case "suspend":
						_, err := m.management.Apply(ctx, contextInvocation(f.principal), f.owner.Token, management)
						return err

					}
					return fmt.Errorf("unexpected mutation %s", kind)
				}
				prioritized, changed := make(chan error, 1), make(chan error, 1)
				waitCompeting := func(pattern string) {
					t.Helper()
					waitCtx, stop := context.WithTimeout(ctx, 3*time.Second)
					defer stop()
					// All patterns are local test constants, never supplied by callers.
					waitProjectionLock(t, waitCtx, f.pool, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '`+pattern+`')`)
				}
				{
					table := "domain_events"
					if !priorityFirst && kind == "suspend" {
						table = "workspace_configuration_audit"
					}
					release := contextGate(t, f, table)
					if priorityFirst {
						go func() { prioritized <- setPriority() }()
					} else {
						go func() { changed <- change() }()
					}
					contextWaitGate(t, f)
					if priorityFirst {
						go func() { changed <- change() }()
					} else {
						go func() { prioritized <- setPriority() }()
					}
					pattern := "%19450001%"
					if kind == "done" {
						pattern = "SELECT%FROM radishnexus.tickets%FOR UPDATE%"
					}

					waitCompeting(pattern)
					release()
				}
				if err := <-changed; err != nil {
					t.Fatal("competing mutation", err)
				}
				err := <-prioritized
				if priorityFirst && err != nil || !priorityFirst && ((kind == "done" && !errors.Is(err, authz.ErrConflict)) || (kind == "suspend" && !errors.Is(err, authz.ErrNotFound))) {
					t.Fatal("wrong serialized priority result", err)
				}
				value, err := priorities.ReadTicketPriority(ctx, f.principal, f.ticket)
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if priorityFirst {
					want = 1
					if value.Priority != "high" {
						t.Fatal(value)
					}
				} else if value.Priority != "unset" {
					t.Fatal("rejected write changed priority", value)
				}
				if value.PriorityRevision != want+1 {
					t.Fatal(value)
				}
				if kind == "done" && (value.CanSetPriority || value.StatusRevision != 7) {
					t.Fatal(value)
				}
				var events, receipts, outbox, activities int
				err = f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM radishnexus.domain_events WHERE event_type='ticket.priority-changed'),(SELECT count(*) FROM radishnexus.collaboration_command_receipts WHERE command_kind='ticket.priority.set'),(SELECT count(*) FROM radishnexus.outbox_deliveries o JOIN radishnexus.domain_events e USING(event_id) WHERE e.event_type='ticket.priority-changed'),(SELECT count(*) FROM radishnexus.activity_items a JOIN radishnexus.domain_events e ON e.event_id=a.event_id WHERE e.event_type='ticket.priority-changed')`).Scan(&events, &receipts, &outbox, &activities)
				if err != nil || events != want || receipts != want || outbox != want || activities != want {
					t.Fatal("non-atomic serialized outcome", events, receipts, outbox, activities, err)
				}
				if priorityFirst {
					result, retryErr := priorities.SetTicketPriority(ctx, inv, input)
					if kind == "done" {
						if retryErr != nil || result.PriorityRevision != 2 {
							t.Fatal(result, retryErr)
						}
					} else if !errors.Is(retryErr, authz.ErrNotFound) {
						t.Fatal("suspended actor reused receipt")
					}
				}
				if _, err = f.service.Export(ctx, f.principal, f.owner.Token, f.project); err != nil {
					t.Fatal("serialized history no longer portable", err)
				}
			})
		}
	}
}
