//go:build integration

package postgres_test

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/calendardate"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

func TestTicketDueDateWritesReplayAndDiscovery(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	ticket, _ := componentTicket(t, ctx, core, "chn_restricted", "due-date")
	store := goldenpostgres.New(pool)
	service := goldenpath.NewTicketDueDateService(store, goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_contributor")
	reader := tcInvocation("usr_reader").Principal
	read := func(who authz.Principal, date *string, revision int, writable bool) {
		t.Helper()
		got, err := service.ReadTicketDueDate(ctx, who, ticket.ID)
		if err != nil || !calendardate.Equal(got.DueDate, date) || got.DueDateRevision != revision || got.CanSetDueDate != writable {
			t.Fatal(got, err)
		}
	}
	read(inv.Principal, nil, 1, true)
	read(reader, nil, 1, false)
	day := "2032-02-29"
	first := goldenpath.TicketDueDateInput{TicketID: ticket.ID, ClientOperationID: "due-first", BaseDueDateRevision: 1, BaseStatusRevision: 1, DueDate: &day, Confirmed: true}
	submit := func(input goldenpath.TicketDueDateInput, errWant error) {
		t.Helper()
		got, err := service.SetTicketDueDate(ctx, inv, input)
		if !errors.Is(err, errWant) || (errWant == nil && (!got.Applied || got.DueDateRevision != input.BaseDueDateRevision+1 || !calendardate.Equal(got.DueDate, input.DueDate))) {
			t.Fatal(input, got, err)
		}
	}

	if _, err := pool.Exec(ctx, `CREATE FUNCTION radishnexus.fail_due_date_activity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected Activity failure'; END $$;
		CREATE TRIGGER fail_due_date_activity BEFORE INSERT ON radishnexus.activity_items FOR EACH ROW WHEN (NEW.activity_type='ticket.due-date-changed') EXECUTE FUNCTION radishnexus.fail_due_date_activity();`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetTicketDueDate(ctx, inv, first); err == nil {
		t.Fatal("injected failure committed")
	}
	read(inv.Principal, nil, 1, true)
	if _, err := pool.Exec(ctx, `DROP TRIGGER fail_due_date_activity ON radishnexus.activity_items; DROP FUNCTION radishnexus.fail_due_date_activity();`); err != nil {
		t.Fatal(err)
	}
	// Retrying the same input after rollback must be able to apply.
	submit(first, nil)
	read(reader, &day, 2, false)
	assignments := goldenpath.NewTicketAssignmentService(store, goldenpath.CryptoIDGenerator{})
	if _, err := assignments.SetTicketAssignee(ctx, inv, assignmentInput(t, ctx, assignments, inv, ticket.ID, inv.Principal.ID, "due-assignee")); err != nil {
		t.Fatal(err)
	}
	discovery := goldenpath.NewDiscoveryService(store)
	page := goldenpath.DiscoveryPageInput{Limit: 25, IncludeDueDate: true}
	project, err := discovery.ListProjectTickets(ctx, inv.Principal, ticket.GoverningProjectID, page)
	if err != nil || len(project.Items) != 1 || !calendardate.Equal(project.Items[0].DueDate, &day) {
		t.Fatal(project, err)
	}
	mine, err := discovery.ListMyTickets(ctx, inv.Principal, page)
	if err != nil || len(mine.Items) != 1 || !calendardate.Equal(mine.Items[0].DueDate, &day) {
		t.Fatal(mine, err)
	}

	clear := first
	clear.ClientOperationID, clear.BaseDueDateRevision, clear.DueDate = "due-clear", 2, nil
	submit(clear, nil)
	stale := first
	stale.ClientOperationID = "due-stale"
	submit(stale, authz.ErrConflict)
	status := goldenpath.NewTicketStatusService(store, goldenpath.CryptoIDGenerator{})
	setStatus := func(revision int, value string) {
		t.Helper()
		_, e := status.ChangeTicketStatus(ctx, inv, goldenpath.TicketStatusInput{TicketID: ticket.ID, ClientOperationID: "due-status-" + value, BaseStatusRevision: revision, ToStatus: value, Confirmed: true})
		if e != nil {
			t.Fatal(e)
		}
	}
	change := first
	change.ClientOperationID, change.BaseDueDateRevision = "due-again", 3
	setStatus(1, "done")
	read(inv.Principal, nil, 3, false)
	terminal := change
	terminal.BaseStatusRevision = 2
	submit(terminal, authz.ErrConflict)
	submit(first, nil)
	setStatus(2, "open")
	submit(change, authz.ErrConflict)
	change.BaseStatusRevision = 3
	submit(change, nil)
	read(inv.Principal, &day, 4, true)

	ref := entityref.Ref{Type: "ticket", ID: ticket.ID}
	before, err := core.GetNexusView(ctx, inv.Principal, ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.RebuildActivityProjection(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := core.GetNexusView(ctx, inv.Principal, ref)
	if err != nil || !reflect.DeepEqual(before.Timeline, after.Timeline) {
		t.Fatal("Activity changed after rebuild", err)
	}
	var receipts, events, outbox, activity int
	if err = pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM radishnexus.collaboration_command_receipts WHERE command_kind='ticket.due-date.set'),
		(SELECT count(*) FROM radishnexus.domain_events WHERE event_type='ticket.due-date-changed'),
		(SELECT count(*) FROM radishnexus.outbox_deliveries o JOIN radishnexus.domain_events e USING(event_id) WHERE e.event_type='ticket.due-date-changed'),
		(SELECT count(*) FROM radishnexus.activity_items WHERE activity_type='ticket.due-date-changed' AND projection_version=16)`).Scan(&receipts, &events, &outbox, &activity); err != nil || receipts != 3 || events != 3 || outbox != 3 || activity != 3 {
		t.Fatal(receipts, events, outbox, activity, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.project_memberships SET role='viewer' WHERE user_id='usr_contributor' AND project_id='prj_auth'`); err != nil {
		t.Fatal(err)
	}
	submit(first, authz.ErrForbidden)
}

func TestTicketDueDateV7RoundTripAndNativeContinuation(t *testing.T) {
	source := contextDatabase(t)
	source.seed(t)
	inv := contextInvocation(source.principal)
	status := goldenpath.NewTicketStatusService(source.store, goldenpath.CryptoIDGenerator{})
	if _, err := status.ChangeTicketStatus(source.ctx, inv, goldenpath.TicketStatusInput{TicketID: source.ticket, ClientOperationID: "portable-due-open", BaseStatusRevision: 5, ToStatus: "open", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	service := goldenpath.NewTicketDueDateService(source.store, goldenpath.CryptoIDGenerator{})
	day := "0001-01-01"
	first := goldenpath.TicketDueDateInput{TicketID: source.ticket, ClientOperationID: "portable-due", BaseDueDateRevision: 1, BaseStatusRevision: 6, DueDate: &day, Confirmed: true}
	if _, err := service.SetTicketDueDate(source.ctx, inv, first); err != nil {
		t.Fatal(err)
	}
	bundle := source.bundle(t)
	if bundle.Manifest.Profile != "project-collaboration-v9" {
		t.Fatal(bundle.Manifest.Profile)
	}
	target := contextDatabase(t)
	plan := target.plan(t, bundle)
	applied, err := target.service.Apply(target.ctx, target.principal, target.owner.Token, bundle, plan, plan.PlanDigest)
	if err != nil {
		t.Fatal(err)
	}
	id := contextMapping(t, plan, "ticket", source.ticket)
	service = goldenpath.NewTicketDueDateService(target.store, goldenpath.CryptoIDGenerator{})
	got, err := service.ReadTicketDueDate(target.ctx, target.principal, id)
	if err != nil || got.DueDateRevision != 2 || !calendardate.Equal(got.DueDate, &day) {
		t.Fatal(got, err)
	}
	native := goldenpath.TicketDueDateInput{TicketID: id, ClientOperationID: "imported-due-clear", BaseDueDateRevision: 2, BaseStatusRevision: got.StatusRevision, Confirmed: true}
	if _, err = service.SetTicketDueDate(target.ctx, contextInvocation(target.principal), native); err != nil {
		t.Fatal(err)
	}
	second, err := target.service.Export(target.ctx, target.principal, target.owner.Token, applied.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	third := contextDatabase(t)
	plan2 := third.plan(t, second)
	if _, err = third.service.Apply(third.ctx, third.principal, third.owner.Token, second, plan2, plan2.PlanDigest); err != nil {
		t.Fatal(err)
	}
	ref := entityref.Ref{Type: "ticket", ID: contextMapping(t, plan2, "ticket", id)}
	got, err = third.store.ReadTicketDueDate(third.ctx, third.principal, ref.ID)
	if err != nil || got.DueDate != nil || got.DueDateRevision != 3 {
		t.Fatal(got, err)
	}
	view, err := third.core.GetNexusView(third.ctx, third.principal, ref)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for _, entry := range view.Timeline {
		if entry.ActivityType == goldenpath.TicketDueDateChanged {
			count++
			if entry.SafeFacts["imported"] != "true" || (entry.SafeFacts["from"] != day && entry.SafeFacts["to"] != day) {
				t.Fatal(entry)
			}
		}
	}
	if count != 2 {
		t.Fatal(count)
	}
}

func TestTicketDueDateConcurrentChanges(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	ticket, _ := componentTicket(t, ctx, core, "chn_restricted", "due-date-race")
	service := goldenpath.NewTicketDueDateService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_contributor")
	dayOne, dayTwo := "2032-02-29", "2032-03-01"
	base := goldenpath.TicketDueDateInput{
		TicketID: ticket.ID, ClientOperationID: "due-race-one", BaseDueDateRevision: 1,
		BaseStatusRevision: 1, DueDate: &dayOne, Confirmed: true,
	}
	other := base
	other.ClientOperationID, other.DueDate = "due-race-two", &dayTwo
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, in := range []goldenpath.TicketDueDateInput{base, other} {
		go func() {
			<-start
			_, err := service.SetTicketDueDate(ctx, inv, in)
			results <- err
		}()
	}
	close(start)
	first, second := <-results, <-results
	if !((first == nil && errors.Is(second, authz.ErrConflict)) ||
		(second == nil && errors.Is(first, authz.ErrConflict))) {
		t.Fatal(first, second)
	}
	date, err := service.ReadTicketDueDate(ctx, inv.Principal, ticket.ID)
	if err != nil || date.DueDateRevision != 2 || date.DueDate == nil {
		t.Fatal(date, err)
	}
}

func TestTicketDueDateSessionHTTPS(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	inv := contextInvocation(f.principal)
	status := goldenpath.NewTicketStatusService(f.store, goldenpath.CryptoIDGenerator{})
	if _, err := status.ChangeTicketStatus(f.ctx, inv, goldenpath.TicketStatusInput{TicketID: f.ticket, ClientOperationID: "https-due-open", BaseStatusRevision: 5, ToStatus: "open", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	assignment := goldenpath.NewTicketAssignmentService(f.store, goldenpath.CryptoIDGenerator{})
	if _, err := assignment.SetTicketAssignee(f.ctx, inv, assignmentInput(t, f.ctx, assignment, inv, f.ticket, f.principal.ID, "https-due-assignee")); err != nil {
		t.Fatal(err)
	}

	server, _ := contextHTTPS(t, f, "")
	workspace := "/api/v1/workspaces/" + f.workspace
	ticket := workspace + "/tickets/" + f.ticket
	const date = "2011-12-30" // Samoa skipped this day locally. The civil date still exists.
	if _, err := f.pool.Exec(f.ctx, `ALTER DATABASE "`+f.pool.Config().ConnConfig.Database+`" SET TimeZone='Pacific/Apia'`); err != nil {
		t.Fatal(err)
	}
	f.pool.Reset() // New sessions use the database's timezone setting.

	submit := func(body, want string) {
		t.Helper()
		req, err := http.NewRequest("POST", server.URL+ticket+"/due-date", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", server.URL)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(httptransport.CSRFHeaderName, f.owner.CSRFToken)
		req.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: f.owner.Token})
		req.AddCookie(&http.Cookie{Name: httptransport.CSRFCookieName, Value: f.owner.CSRFToken})
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != 200 || !strings.Contains(string(raw), want) {
			t.Fatal(response.StatusCode, string(raw), err)
		}
	}
	submit(`{"client_operation_id":"https-due-set","base_due_date_revision":1,"base_status_revision":6,"due_date":"`+date+`","confirmed":true}`, `"applied_due_date":"`+date+`"`)
	for _, path := range []string{
		ticket + "/due-date",
		workspace + "/projects/" + f.project + "/tickets?include=due_date",
		workspace + "/my-tickets?include=due_date",
	} {
		body := string(contextHTTPRead(t, server, f.owner, path, 200))
		if !strings.Contains(body, `"due_date":"`+date+`"`) {
			t.Fatal(path, body)
		}
	}
	submit(`{"client_operation_id":"https-due-clear","base_due_date_revision":2,"base_status_revision":6,"due_date":null,"confirmed":true}`, `"applied_due_date":null`)
	body := string(contextHTTPRead(t, server, f.owner, ticket+"/nexus-view?include=due_date", 200))
	if !strings.Contains(body, `"from":"`+date+`","to":null`) || !strings.Contains(body, `"from":null,"to":"`+date+`"`) {
		t.Fatal(body)
	}
	if body = string(contextHTTPRead(t, server, f.owner, ticket+"/nexus-view", 200)); strings.Contains(body, `"ticket.due-date-changed"`) {
		t.Fatal(body)
	}
}
