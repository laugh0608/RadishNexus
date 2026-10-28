//go:build integration

package postgres_test

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestTicketPriorityFilterMatrixAndPagination(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	store := goldenpostgres.New(pool)
	discovery := goldenpath.NewDiscoveryService(store)
	priorities := goldenpath.NewTicketPriorityService(store, goldenpath.CryptoIDGenerator{})
	assignments := goldenpath.NewTicketAssignmentService(store, goldenpath.CryptoIDGenerator{})
	statuses := goldenpath.NewTicketStatusService(store, goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_contributor")
	type row struct{ id, status, priority, assignee string }
	rows := []row{}
	for i := 0; i < 30; i++ {
		ticket, _ := componentTicket(t, ctx, core, "chn_restricted", fmt.Sprintf("filters-%02d", i))
		r := row{id: ticket.ID, status: []string{"open", "in-progress", "done", "canceled"}[i%4], priority: []string{"unset", "low", "normal", "high", "urgent"}[i%5], assignee: []string{"", "usr_contributor", "usr_decider"}[i%3]}
		if r.priority != "unset" {
			if _, err := priorities.SetTicketPriority(ctx, inv, priorityInput(t, ctx, priorities, inv, r.id, r.priority, "set")); err != nil {
				t.Fatal(err)
			}
		}
		if r.assignee != "" {
			if _, err := assignments.SetTicketAssignee(ctx, inv, assignmentInput(t, ctx, assignments, inv, r.id, r.assignee, "assign")); err != nil {
				t.Fatal(err)
			}
		}
		if r.status != "open" {
			if _, err := statuses.ChangeTicketStatus(ctx, inv, goldenpath.TicketStatusInput{TicketID: r.id, ClientOperationID: "status", BaseStatusRevision: 1, ToStatus: r.status, Confirmed: true}); err != nil {
				t.Fatal(err)
			}
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
	for _, state := range []string{"", "active", "open", "in-progress", "done", "canceled"} {
		for _, assignee := range []string{"", "me", "unassigned"} {
			for _, priority := range []string{"", "unset", "low", "normal", "high", "urgent"} {
				filter := goldenpath.TicketFilters{Status: state, Assignee: assignee, Priority: priority}
				want := []string{}
				for _, r := range rows {
					if state != "" && state != r.status && !(state == "active" && (r.status == "open" || r.status == "in-progress")) {
						continue
					}
					if assignee == "me" && r.assignee != inv.Principal.ID || assignee == "unassigned" && r.assignee != "" || priority != "" && priority != r.priority {
						continue
					}
					want = append(want, r.id)
				}
				for _, limit := range []int{1, 25, 50} {
					got := []string{}
					after := ""
					for step := 0; step <= len(rows); step++ {
						page, err := discovery.ListProjectTickets(ctx, inv.Principal, "prj_auth", goldenpath.DiscoveryPageInput{Limit: limit, AfterID: after, TicketFilters: filter})
						if err != nil || page.Items == nil {
							t.Fatal(filter, err)
						}
						for _, item := range page.Items {
							got = append(got, item.Ref.ID)
						}
						if page.NextID == "" {
							break
						}
						if page.NextID <= after || len(page.Items) != limit {
							t.Fatal("invalid page boundary", page)
						}
						after = page.NextID
						if step == len(rows) {
							t.Fatal("paging failed to terminate")
						}
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatal("filter page mismatch", filter, limit, got, want)
					}
				}
			}
		}
	}
	// Nonexistent boundaries are valid, but permission is always rechecked.
	page, err := discovery.ListProjectTickets(ctx, inv.Principal, "prj_auth", goldenpath.DiscoveryPageInput{Limit: 25, AfterID: "tkt_zzzzzz", TicketFilters: goldenpath.TicketFilters{Status: "active"}})
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.NextID != "" {
		t.Fatal(page, err)
	}
	for _, limit := range []int{25, 26} { // Demonstrate the boundary using a legal page size and explicit expected tail.
		first, err := discovery.ListProjectTickets(ctx, inv.Principal, "prj_auth", goldenpath.DiscoveryPageInput{Limit: limit})
		if err != nil || len(first.Items) != limit || first.NextID == "" {
			t.Fatal(first, err)
		}
		tail, err := discovery.ListProjectTickets(ctx, inv.Principal, "prj_auth", goldenpath.DiscoveryPageInput{Limit: limit, AfterID: first.NextID})
		if err != nil || len(tail.Items) != 30-limit || tail.NextID != "" {
			t.Fatal(tail, err)
		}
	}
	// A row before an already consumed boundary can newly match; refresh is required.
	first := rows[0]
	if first.status == "done" || first.status == "canceled" {
		if _, err = statuses.ChangeTicketStatus(ctx, inv, goldenpath.TicketStatusInput{TicketID: first.id, ClientOperationID: "filter-reopen", BaseStatusRevision: 2, ToStatus: "open", Confirmed: true}); err != nil {
			t.Fatal(err)
		}
	}
	target := "high"
	if first.priority == target {
		target = "low"
	}
	if _, err = priorities.SetTicketPriority(ctx, inv, priorityInput(t, ctx, priorities, inv, first.id, target, "new-match")); err != nil {
		t.Fatal(err)
	}
	q := goldenpath.DiscoveryPageInput{Limit: 50, AfterID: rows[1].id, TicketFilters: goldenpath.TicketFilters{Priority: target}}
	page, err = discovery.ListProjectTickets(ctx, inv.Principal, "prj_auth", q)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range page.Items {
		if v.Ref.ID == first.id {
			t.Fatal("keyset moved backwards")
		}
	}
	q.AfterID = ""
	page, err = discovery.ListProjectTickets(ctx, inv.Principal, "prj_auth", q)
	if err != nil || len(page.Items) == 0 || page.Items[0].Ref.ID != first.id {
		t.Fatal("refresh missed newly matching row", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.projects SET status='archived' WHERE id='prj_auth'`); err != nil {
		t.Fatal(err)
	}
	if _, err = discovery.ListProjectTickets(ctx, inv.Principal, "prj_auth", q); err != nil {
		t.Fatal("archived read", err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM radishnexus.project_memberships WHERE project_id='prj_auth' AND user_id='usr_contributor'`); err != nil {
		t.Fatal(err)
	}
	if _, err = discovery.ListProjectTickets(ctx, inv.Principal, "prj_auth", q); err != nil {
		t.Fatal("role removal erased public read baseline", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.projects SET visibility='restricted' WHERE id='prj_auth'`); err != nil {
		t.Fatal(err)
	}
	if _, err = discovery.ListProjectTickets(ctx, inv.Principal, "prj_auth", q); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("cursor retained access", err)
	}
}
