//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
)

func TestTicketTimelineHTTPS(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	srv, _ := contextHTTPS(t, f, "")
	base := "/api/v1/workspaces/" + f.workspace
	path := func(id string) string { return base + "/tickets/" + id + "/nexus-view" }
	read := func(path string, want int) []byte { return contextHTTPRead(t, srv, f.owner, path, want) }
	member, err := f.auth.Login(f.ctx, authn.LoginInput{Email: "context.member@example.test", Password: contextTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	channel := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: f.project, Name: "History source", Visibility: "project", MemberUserIDs: []string{}}).Object.ID
	message := deliveryMessage(t, f, channel, "", "history-source")
	ticket := deliveryMessageTicket(t, f, channel, message, "history-work")
	decisionTicket := deliveryDecisionTicket(t, f, f.decision, "history-decision")
	invoke := contextInvocation(f.principal)
	due := "2030-02-03"
	for _, id := range []string{ticket, decisionTicket} {
		_, err := goldenpath.NewTicketDueDateService(f.store, goldenpath.CryptoIDGenerator{}).SetTicketDueDate(f.ctx, invoke, goldenpath.TicketDueDateInput{TicketID: id, BaseDueDateRevision: 1, BaseStatusRevision: 1, ClientOperationID: "due-" + id, DueDate: &due, Confirmed: true})
		if err != nil {
			t.Fatal(err)
		}
		_, err = goldenpath.NewTicketClassificationService(f.store, goldenpath.CryptoIDGenerator{}).Set(f.ctx, invoke, goldenpath.ClassificationInput{TicketID: id, Classification: cp.Classification{Type: "task", LabelIDs: []string{}}, BaseRevision: 1, ClientOperationID: "classification-" + id, Confirmed: true})
		if err != nil {
			t.Fatal(err)
		}
		deliveryExecution(t, f, goldenpath.TicketDiscussionLink, goldenpath.TicketExecutionInput{TicketID: id, ThreadID: f.thread, ClientOperationID: "discussion-" + id})
	}
	// Force a tie in this projection-only scenario, so all page boundaries
	// exercise the event ID tiebreaker in both new and legacy reads.
	contextSQL(t, f, `UPDATE radishnexus.activity_items SET occurred_at='2030-01-01T00:00:00Z' WHERE workspace_id=$1 AND target_type='ticket' AND target_id=$2`, f.workspace, decisionTicket)
	for _, id := range []string{ticket, decisionTicket} {
		for _, inc := range []string{"", "due_date", "classification", "execution_context", "due_date,execution_context", "due_date,classification", "execution_context,classification", "due_date,execution_context,classification"} {
			q := url.Values{}
			if inc != "" {
				q.Set("include", inc)
			}
			status := 200
			if id == ticket && !strings.Contains(inc, "execution_context") {
				status = 409
			}
			raw := read(path(id)+"?"+q.Encode(), status)
			for _, summary := range []bool{false, true} {
				if summary {
					q.Set("relations", "summary")
				}
				q.Set("timeline", "separate")
				brief := read(path(id)+"?"+q.Encode(), status)
				if status == 200 && (bytes.Contains(brief, []byte(`"timeline":`)) || !bytes.Contains(brief, []byte(`"capabilities"`))) {
					t.Fatal("invalid separate detail", string(brief))
				}
				q.Del("timeline")
			}
			q.Del("relations")
			q.Set("limit", "1")
			if status == 409 {
				read(path(id)+"/timeline?"+q.Encode(), 409)
				continue
			}
			var legacy struct {
				Data struct{ Timeline []json.RawMessage }
			}
			if err := json.Unmarshal(raw, &legacy); err != nil || len(legacy.Data.Timeline) == 0 {
				t.Fatal("legacy baseline", err)
			}
			for i := len(legacy.Data.Timeline) - 1; i >= 0; i-- {
				row := parseNexusRelationsResponse(t, read(path(id)+"/timeline?"+q.Encode(), 200)).Data
				if len(row.Items) != 1 || !bytes.Equal(row.Items[0], legacy.Data.Timeline[i]) || (row.Cursor != nil) != (i > 0) {
					t.Fatalf("include %q at %d: got %s want %s", inc, i, row.Items, legacy.Data.Timeline[i])
				}
				if row.Cursor != nil {
					q.Set("after", *row.Cursor)
				}
			}
		}
	}
	endpoint := path(ticket) + "/timeline?include=execution_context&limit=1"
	memberRead := func(suffix string, want int) []byte { return contextHTTPRead(t, srv, member, endpoint+suffix, want) }
	visible := memberRead("", 200)
	ownerPage := parseNexusRelationsResponse(t, read(endpoint, 200)).Data
	if len(ownerPage.Items) != 1 || ownerPage.Cursor == nil || !bytes.Contains(ownerPage.Items[0], []byte(f.thread)) {
		t.Fatal("expected newest discussion")
	}
	// Hidden discussion takes no slot and emits no next cursor for this selection.
	if bytes.Contains(visible, []byte(f.thread)) || parseNexusRelationsResponse(t, visible).Data.Cursor != nil {
		t.Fatal("hidden boundary leaked")
	}
	contextSQL(t, f, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,management_role) VALUES($1,$2,$3,'member')`, f.workspace, f.thread, f.member.ID)
	boundary := parseNexusRelationsResponse(t, memberRead("", 200)).Data.Cursor
	if boundary == nil {
		t.Fatal("member missing boundary")
	}
	after := "&after=" + url.QueryEscape(*boundary)
	read(endpoint+after, 400)
	contextHTTPRead(t, srv, member, path(decisionTicket)+"/timeline?include=execution_context"+after, 400)
	contextHTTPRead(t, srv, member, path(ticket)+"/timeline?include=due_date,execution_context"+after, 400)
	// A newer visible priority change gives this reader a second, independent
	// boundary. Revoking the older discussion must not invalidate this one.
	priorities := goldenpath.NewTicketPriorityService(f.store, goldenpath.CryptoIDGenerator{})
	changePriority := func(revision int, value string) {
		t.Helper()
		_, err := priorities.SetTicketPriority(f.ctx, invoke, goldenpath.TicketPriorityInput{
			TicketID: ticket, ClientOperationID: "continuation-" + value, BasePriorityRevision: revision,
			BaseStatusRevision: 1, Priority: value, Confirmed: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	changePriority(1, "high")
	validBoundary := parseNexusRelationsResponse(t, memberRead("", 200)).Data.Cursor
	if validBoundary == nil {
		t.Fatal("missing priority boundary")
	}
	changePriority(2, "low")
	contextSQL(t, f, `DELETE FROM radishnexus.channel_memberships WHERE workspace_id=$1 AND channel_id=$2 AND user_id=$3`, f.workspace, f.channel, f.member.ID)
	memberRead(after, 409)
	continued := memberRead("&after="+url.QueryEscape(*validBoundary), 200)
	if !bytes.Equal(visible, continued) || !bytes.Contains(memberRead("", 200), []byte(`"to":"low"`)) {
		t.Fatal("non-boundary revocation or concurrent insert changed continuation")
	}
	// Loss of the creation Message has different disclosure semantics.
	contextSQL(t, f, `UPDATE radishnexus.channels SET visibility='restricted' WHERE workspace_id=$1 AND id=$2`, f.workspace, channel)
	restricted := memberRead("&after="+url.QueryEscape(*validBoundary), 200)
	if !bytes.Contains(restricted, []byte(`"subjects":[{"visibility":"restricted"}]`)) || bytes.Contains(restricted, []byte(message)) {
		t.Fatal("restricted creation source leaked", string(restricted))
	}
	contextSQL(t, f, `DELETE FROM radishnexus.project_memberships WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3`, f.workspace, f.project, f.member.ID)
	memberRead(after, 404)
	for _, q := range []string{"limit=0", "limit=51", "after=", "limit=1&limit=2", "include=", "include=classification,due_date", "include=due_date&include=due_date", "timeline=separate", "after=bad"} {
		read(path(ticket)+"/timeline?"+q, 400)
	}
	read(base+"/threads/"+f.thread+"/nexus-view?timeline=separate", 400)
	read(path(ticket)+"?timeline=bad", 400)
	contextHTTPRead(t, srv, authn.Session{}, endpoint, 401)
	executionWriteHTTP(t, srv, f.owner, "POST", path(ticket)+"/timeline", "{}", 405)
}

type ticketTimelineTracer struct {
	relationQueryCounter
	sql            string
	args           []any
	historyQueries int
}

func (tr *ticketTimelineTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, q pgx.TraceQueryStartData) context.Context {
	if strings.Contains(q.SQL, "FROM radishnexus.activity_items") {
		tr.historyQueries++
	}
	if strings.HasPrefix(q.SQL, "SELECT a.event_id,a.activity_type,") {
		tr.sql, tr.args = q.SQL, slices.Clone(q.Args)
	}
	return tr.relationQueryCounter.TraceQueryStart(ctx, conn, q)
}

func TestTicketTimelineQueryPlan(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	ticket := deliveryDecisionTicket(t, f, f.decision, "history-plan")
	neighbor := deliveryDecisionTicket(t, f, f.decision, "history-neighbor")
	tr := &ticketTimelineTracer{}
	cfg := f.pool.Config()
	cfg.ConnConfig.Tracer = tr
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	reader := goldenpostgres.New(pool)
	for _, summary := range []bool{true, false} {
		view, err := reader.GetTicketWithoutTimeline(f.ctx, f.member, ticket, summary)
		if err != nil || view.Timeline != nil || tr.historyQueries != 0 {
			t.Fatal("separate detail queried history", summary, err, tr.historyQueries)
		}
	}
	writer := goldenpath.NewTicketPriorityService(f.store, goldenpath.CryptoIDGenerator{})
	add := func(id string, start, end int) {
		for i := start; i < end; i++ {
			_, err := writer.SetTicketPriority(f.ctx, contextInvocation(f.principal), goldenpath.TicketPriorityInput{TicketID: id, ClientOperationID: fmt.Sprintf("%s-priority-%d", id, i), BasePriorityRevision: i + 1, BaseStatusRevision: 1, Priority: []string{"high", "low"}[i%2], Confirmed: true})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	measure := func(label string, limit int, after string) (int64, string) {
		t.Helper()
		contextSQL(t, f, "ANALYZE radishnexus.activity_items")
		tr.queries.Store(0)
		page, err := reader.ListTicketTimeline(f.ctx, f.member, ticket, goldenpath.TicketTimelineInput{TicketTimelineSelection: goldenpath.TicketTimelineSelection{Execution: true}, AfterEventID: after, Limit: limit})
		count := tr.queries.Load()
		if err != nil || len(page.Items) != limit || page.NextEventID == "" {
			t.Fatal(label, len(page.Items), err)
		}
		var plan string
		rows, err := f.pool.Query(f.ctx, "EXPLAIN (ANALYZE, BUFFERS) "+tr.sql, tr.args...)
		if err != nil {
			t.Fatal(err)
		}
		lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range lines {
			if strings.Contains(line, "activity_items") || strings.Contains(line, "Execution Time:") || strings.HasPrefix(line, "  Buffers:") {
				plan += "\n" + line
			}
		}
		t.Logf("%s limit=%d calls=%d%s", label, limit, count, plan)
		return count, page.NextEventID
	}
	add(ticket, 0, 8)
	small, _ := measure("8 visible", 1, "")
	add(ticket, 8, 100)
	measure("100 visible", 1, "")
	for i := range 30 {
		linked := deliveryExecution(t, f, goldenpath.TicketDiscussionLink, goldenpath.TicketExecutionInput{TicketID: ticket, ThreadID: f.thread, ClientOperationID: fmt.Sprintf("plan-link-%d", i)})
		deliveryExecution(t, f, goldenpath.TicketDiscussionUnlink, goldenpath.TicketExecutionInput{TicketID: ticket, LinkID: linked.LinkID, ClientOperationID: fmt.Sprintf("plan-unlink-%d", i)})
	}
	hidden, _ := measure("+60 hidden", 1, "")
	add(neighbor, 0, 800)
	busy, _ := measure("+800 unrelated", 1, "")
	wide, at := measure("wide", 50, "")
	continued, _ := measure("continuation", 1, at)
	if small != hidden || hidden != busy || wide != busy || continued != wide+1 {
		t.Fatalf("unexpected calls %d/%d/%d/%d/%d", small, hidden, busy, wide, continued)
	}
}
