//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type prefixedSequenceIDs struct{ next int }

func (ids *prefixedSequenceIDs) NewID(prefix string) (string, error) {
	ids.next++
	return fmt.Sprintf("%stest_%02d", prefix, ids.next), nil
}

type fixedIDs struct{ values []string }

func (ids *fixedIDs) NewID(_ string) (string, error) {
	if len(ids.values) == 0 {
		return "", errors.New("fixed ID sequence exhausted")
	}
	value := ids.values[0]
	ids.values = ids.values[1:]
	return value, nil
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

func TestGoldenPathPermissionsAndAtomicity(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for integration tests")
	}
	ctx := context.Background()

	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgx.Connect() error = %v", err)
	}
	if err := db.Migrate(ctx, connection); err != nil {
		t.Fatalf("db.Migrate() first run error = %v", err)
	}
	if err := db.Migrate(ctx, connection); err != nil {
		t.Fatalf("db.Migrate() idempotent run error = %v", err)
	}
	if err := connection.Close(ctx); err != nil {
		t.Fatalf("migration connection Close() error = %v", err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	seedGoldenPath(t, ctx, pool)

	store := goldenpostgres.New(pool)
	clock := fixedClock{now: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)}
	messageNotifier := &committedMessageRecorder{}
	service := goldenpath.NewService(
		store,
		&prefixedSequenceIDs{},
		clock,
		goldenpath.WithMessageCreatedNotifier(messageNotifier),
	)
	contributor := principal("usr_contributor")
	decider := principal("usr_decider")
	reader := principal("usr_reader")
	admin := principal("usr_admin")

	_, err = service.CreateDecisionFromThread(ctx, invocation(reader, "cor_reader_denied"), goldenpath.CreateDecisionInput{
		ThreadID:          "thr_private",
		ClientOperationID: "test:reader:decision",
		Question:          "Should not discover this thread?",
	})
	if !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("reader CreateDecisionFromThread() error = %v, want not found", err)
	}

	decisionResult, err := service.CreateDecisionFromThread(ctx, invocation(contributor, "cor_propose"), goldenpath.CreateDecisionInput{
		ThreadID:          "thr_private",
		ClientOperationID: "test:decision:propose",
		Question:          "Should the login API add rate limiting?",
	})
	if err != nil {
		t.Fatalf("CreateDecisionFromThread() error = %v", err)
	}
	decision := decisionResult.Decision
	if !decisionResult.Created || decision.Status != "proposed" || decision.GoverningProjectID != "prj_auth" {
		t.Fatalf("created Decision = %#v", decisionResult)
	}
	assertCounts(t, ctx, pool, 1, 1, 1, 1)
	duplicateDecision, err := service.CreateDecisionFromThread(
		ctx,
		invocation(contributor, "cor_propose_retry"),
		goldenpath.CreateDecisionInput{
			ThreadID:          "thr_private",
			ClientOperationID: "test:decision:propose",
			Question:          "Should the login API add rate limiting?",
		},
	)
	if err != nil || duplicateDecision.Created || duplicateDecision.Decision.ID != decision.ID {
		t.Fatalf("duplicate Decision proposal = %#v, error = %v", duplicateDecision, err)
	}
	_, err = service.CreateDecisionFromThread(
		ctx,
		invocation(contributor, "cor_propose_changed_retry"),
		goldenpath.CreateDecisionInput{
			ThreadID:          "thr_private",
			ClientOperationID: "test:decision:propose",
			Question:          "Changed replay must conflict.",
		},
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("changed Decision proposal replay error = %v, want conflict", err)
	}
	assertCounts(t, ctx, pool, 1, 1, 1, 1)

	_, err = service.AcceptDecision(ctx, invocation(contributor, "cor_contributor_accept"), goldenpath.AcceptDecisionInput{
		DecisionID:        decision.ID,
		ClientOperationID: "test:contributor:accept",
		Outcome:           "Use a token bucket.",
		Rationale:         "Bound abuse while keeping bursts usable.",
	})
	if !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("contributor AcceptDecision() error = %v, want forbidden", err)
	}
	assertDecisionStatus(t, ctx, pool, decision.ID, "proposed")
	assertCounts(t, ctx, pool, 1, 1, 1, 1)

	_, err = service.AcceptDecision(ctx, invocation(admin, "cor_admin_accept"), goldenpath.AcceptDecisionInput{
		DecisionID:        decision.ID,
		ClientOperationID: "test:admin:accept",
		Outcome:           "Use a token bucket.",
		Rationale:         "An admin without evidence access must not confirm this.",
	})
	if !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("admin without evidence AcceptDecision() error = %v, want forbidden", err)
	}
	assertDecisionStatus(t, ctx, pool, decision.ID, "proposed")
	assertCounts(t, ctx, pool, 1, 1, 1, 1)

	acceptResult, err := service.AcceptDecision(ctx, invocation(decider, "cor_accept"), goldenpath.AcceptDecisionInput{
		DecisionID:        decision.ID,
		ClientOperationID: "test:decision:accept",
		Outcome:           "Use a token bucket.",
		Rationale:         "Bound abuse while keeping bursts usable.",
	})
	if err != nil {
		t.Fatalf("decider AcceptDecision() error = %v", err)
	}
	decision = acceptResult.Decision
	if !acceptResult.Accepted || decision.Status != "accepted" || len(decision.DeciderIDs) != 1 || decision.DeciderIDs[0] != decider.ID {
		t.Fatalf("accepted Decision = %#v", acceptResult)
	}
	duplicateAcceptance, err := service.AcceptDecision(
		ctx,
		invocation(decider, "cor_accept_retry"),
		goldenpath.AcceptDecisionInput{
			DecisionID:        decision.ID,
			ClientOperationID: "test:decision:accept",
			Outcome:           "Use a token bucket.",
			Rationale:         "Bound abuse while keeping bursts usable.",
		},
	)
	if err != nil || duplicateAcceptance.Accepted || duplicateAcceptance.Decision.ID != decision.ID {
		t.Fatalf("duplicate Decision acceptance = %#v, error = %v", duplicateAcceptance, err)
	}
	_, err = service.AcceptDecision(
		ctx,
		invocation(decider, "cor_accept_changed_retry"),
		goldenpath.AcceptDecisionInput{
			DecisionID:        decision.ID,
			ClientOperationID: "test:decision:accept",
			Outcome:           "Use a token bucket.",
			Rationale:         "Changed replay must conflict.",
		},
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("changed Decision acceptance replay error = %v, want conflict", err)
	}

	ticketResult, err := service.CreateTicketFromDecision(ctx, invocation(contributor, "cor_ticket"), goldenpath.CreateTicketInput{
		DecisionID:        decision.ID,
		ClientOperationID: "test:ticket:create",
		Title:             "Implement login token bucket",
	})
	if err != nil {
		t.Fatalf("CreateTicketFromDecision() error = %v", err)
	}
	ticket := ticketResult.Ticket
	if !ticketResult.Created || ticket.Status != "open" || ticket.GoverningProjectID != "prj_auth" {
		t.Fatalf("created Ticket = %#v", ticketResult)
	}
	assertCounts(t, ctx, pool, 1, 2, 3, 3)
	duplicateTicket, err := service.CreateTicketFromDecision(
		ctx,
		invocation(contributor, "cor_ticket_retry"),
		goldenpath.CreateTicketInput{
			DecisionID:        decision.ID,
			ClientOperationID: "test:ticket:create",
			Title:             "Implement login token bucket",
		},
	)
	if err != nil || duplicateTicket.Created || duplicateTicket.Ticket.ID != ticket.ID {
		t.Fatalf("duplicate Ticket creation = %#v, error = %v", duplicateTicket, err)
	}
	_, err = service.CreateTicketFromDecision(
		ctx,
		invocation(contributor, "cor_ticket_changed_retry"),
		goldenpath.CreateTicketInput{
			DecisionID:        decision.ID,
			ClientOperationID: "test:ticket:create",
			Title:             "Changed replay must conflict",
		},
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("changed Ticket creation replay error = %v, want conflict", err)
	}
	assertCounts(t, ctx, pool, 1, 2, 3, 3)

	visibleRelations, err := service.ListRelations(ctx, decider, entityref.Ref{Type: "decision", ID: decision.ID})
	if err != nil {
		t.Fatalf("decider ListRelations() error = %v", err)
	}
	if len(visibleRelations) != 1 || visibleRelations[0].State != goldenpath.ProjectionVisible ||
		visibleRelations[0].Target != (entityref.Ref{Type: "thread", ID: "thr_private"}) ||
		visibleRelations[0].Title != "Private rate-limit discussion" {
		t.Fatalf("visible relations = %#v", visibleRelations)
	}

	for _, testPrincipal := range []authz.Principal{reader, admin} {
		restrictedRelations, err := service.ListRelations(ctx, testPrincipal, entityref.Ref{Type: "decision", ID: decision.ID})
		if err != nil {
			t.Fatalf("%s ListRelations() error = %v", testPrincipal.ID, err)
		}
		if len(restrictedRelations) != 1 {
			t.Fatalf("%s restricted relations = %#v", testPrincipal.ID, restrictedRelations)
		}
		restricted := restrictedRelations[0]
		if restricted.State != goldenpath.ProjectionRestricted || restricted.RelationType != "" ||
			restricted.Target != (entityref.Ref{}) || restricted.Title != "" {
			t.Fatalf("%s restricted projection leaks fields: %#v", testPrincipal.ID, restricted)
		}
	}

	_, err = service.ListRelations(ctx, reader, entityref.Ref{Type: "thread", ID: "thr_private"})
	if !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("direct restricted Thread ListRelations() error = %v, want not found", err)
	}

	ticketRelations, err := service.ListRelations(ctx, reader, entityref.Ref{Type: "ticket", ID: ticket.ID})
	if err != nil {
		t.Fatalf("reader Ticket ListRelations() error = %v", err)
	}
	if len(ticketRelations) != 1 || ticketRelations[0].State != goldenpath.ProjectionVisible ||
		ticketRelations[0].Target.ID != decision.ID || ticketRelations[0].RelationType != "implements" {
		t.Fatalf("Ticket relations = %#v", ticketRelations)
	}

	duplicateEventID := loadDomainEventID(t, ctx, pool, "decision.proposed", decision.ID)
	atomicFailureService := goldenpath.NewService(store, &fixedIDs{values: []string{
		"dec_atomic_failure",
		"lnk_atomic_failure",
		duplicateEventID,
	}}, clock)
	_, err = atomicFailureService.CreateDecisionFromThread(ctx, invocation(contributor, "cor_atomic_failure"), goldenpath.CreateDecisionInput{
		ThreadID:          "thr_private",
		ClientOperationID: "test:decision:atomic-failure",
		Question:          "This transaction must roll back.",
	})
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("duplicate event CreateDecisionFromThread() error = %v, want conflict", err)
	}
	assertAbsent(t, ctx, pool, "radishnexus.decisions", "dec_atomic_failure")
	assertAbsent(t, ctx, pool, "radishnexus.entity_links", "lnk_atomic_failure")
	assertCounts(t, ctx, pool, 1, 2, 3, 3)

	assertDatabaseConstraints(t, ctx, pool, decision.ID, duplicateEventID)
	assertNexusViewReadSlice(t, ctx, pool, store, service, decider, reader, admin, decision, ticket)
	assertJenkinsCIRunSlice(t, ctx, pool, store, service)
	assertMessagingSlice(t, ctx, pool, store, service, messageNotifier)
}

func assertNexusViewReadSlice(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *goldenpostgres.Store,
	service *goldenpath.Service,
	decider authz.Principal,
	reader authz.Principal,
	admin authz.Principal,
	decision goldenpath.Decision,
	ticket goldenpath.Ticket,
) {
	t.Helper()

	projected, err := store.RebuildActivityProjection(ctx)
	if err != nil {
		t.Fatalf("RebuildActivityProjection() error = %v", err)
	}
	if projected != 3 {
		t.Fatalf("RebuildActivityProjection() projected = %d, want 3", projected)
	}
	assertTableCount(t, ctx, pool, "radishnexus.activity_items", 3)

	decisionRef := entityref.Ref{Type: "decision", ID: decision.ID}
	ticketRef := entityref.Ref{Type: "ticket", ID: ticket.ID}
	deciderView := loadNexusView(t, ctx, service, decider, decisionRef)
	assertDecisionNexusView(t, deciderView, decision, goldenpath.ProjectionVisible)

	readerView := loadNexusView(t, ctx, service, reader, decisionRef)
	assertDecisionNexusView(t, readerView, decision, goldenpath.ProjectionRestricted)
	adminView := loadNexusView(t, ctx, service, admin, decisionRef)
	assertDecisionNexusView(t, adminView, decision, goldenpath.ProjectionRestricted)

	ticketView := loadNexusView(t, ctx, service, reader, ticketRef)
	if ticketView.Current.Ref != ticketRef || ticketView.Current.Title != ticket.Title || ticketView.Current.Status != "open" {
		t.Fatalf("Ticket Current = %#v", ticketView.Current)
	}
	if len(ticketView.Relations) != 1 || ticketView.Relations[0].State != goldenpath.ProjectionVisible ||
		ticketView.Relations[0].Target != decisionRef {
		t.Fatalf("Ticket Relations = %#v", ticketView.Relations)
	}
	if len(ticketView.Timeline) != 1 || ticketView.Timeline[0].ActivityType != "ticket.created" ||
		len(ticketView.Timeline[0].Subjects) != 1 ||
		ticketView.Timeline[0].Subjects[0].State != goldenpath.ProjectionVisible ||
		ticketView.Timeline[0].Subjects[0].Ref != decisionRef {
		t.Fatalf("Ticket Timeline = %#v", ticketView.Timeline)
	}

	projected, err = store.RebuildActivityProjection(ctx)
	if err != nil {
		t.Fatalf("idempotent RebuildActivityProjection() error = %v", err)
	}
	if projected != 3 {
		t.Fatalf("idempotent RebuildActivityProjection() projected = %d, want 3", projected)
	}
	if rebuilt := loadNexusView(t, ctx, service, reader, decisionRef); !reflect.DeepEqual(rebuilt, readerView) {
		t.Fatalf("idempotent rebuilt Decision Nexus View changed\n got: %#v\nwant: %#v", rebuilt, readerView)
	}

	_, err = pool.Exec(ctx, `
		UPDATE radishnexus.outbox_deliveries
		SET state = 'delivered', delivered_at = clock_timestamp()
	`)
	if err != nil {
		t.Fatalf("mark Outbox deliveries complete: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM radishnexus.outbox_deliveries`); err != nil {
		t.Fatalf("clean completed Outbox deliveries: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM radishnexus.activity_items`); err != nil {
		t.Fatalf("clear Activity projection: %v", err)
	}
	assertTableCount(t, ctx, pool, "radishnexus.domain_events", 3)
	assertTableCount(t, ctx, pool, "radishnexus.outbox_deliveries", 0)
	assertTableCount(t, ctx, pool, "radishnexus.activity_items", 0)

	projected, err = store.RebuildActivityProjection(ctx)
	if err != nil {
		t.Fatalf("rebuild after Outbox cleanup error = %v", err)
	}
	if projected != 3 {
		t.Fatalf("rebuild after Outbox cleanup projected = %d, want 3", projected)
	}
	if rebuilt := loadNexusView(t, ctx, service, reader, decisionRef); !reflect.DeepEqual(rebuilt, readerView) {
		t.Fatalf("Outbox-independent rebuilt Decision Nexus View changed\n got: %#v\nwant: %#v", rebuilt, readerView)
	}
	if rebuilt := loadNexusView(t, ctx, service, decider, decisionRef); !reflect.DeepEqual(rebuilt, deciderView) {
		t.Fatalf("authorized rebuilt Decision Nexus View changed\n got: %#v\nwant: %#v", rebuilt, deciderView)
	}
	if rebuilt := loadNexusView(t, ctx, service, reader, ticketRef); !reflect.DeepEqual(rebuilt, ticketView) {
		t.Fatalf("rebuilt Ticket Nexus View changed\n got: %#v\nwant: %#v", rebuilt, ticketView)
	}
}

func loadNexusView(
	t *testing.T,
	ctx context.Context,
	service *goldenpath.Service,
	principal authz.Principal,
	target entityref.Ref,
) goldenpath.NexusView {
	t.Helper()
	view, err := service.GetNexusView(ctx, principal, target)
	if err != nil {
		t.Fatalf("%s GetNexusView(%s) error = %v", principal.ID, target.URI(), err)
	}
	return view
}

func assertDecisionNexusView(
	t *testing.T,
	view goldenpath.NexusView,
	decision goldenpath.Decision,
	subjectState goldenpath.ProjectionState,
) {
	t.Helper()
	decisionRef := entityref.Ref{Type: "decision", ID: decision.ID}
	if view.Current.Ref != decisionRef || view.Current.Title != decision.Question || view.Current.Status != "accepted" {
		t.Fatalf("Decision Current = %#v", view.Current)
	}
	if len(view.Relations) != 1 || view.Relations[0].State != subjectState {
		t.Fatalf("Decision Relations = %#v, want subject state %s", view.Relations, subjectState)
	}
	if len(view.Timeline) != 2 || view.Timeline[0].ActivityType != "decision.proposed" ||
		view.Timeline[1].ActivityType != "decision.accepted" ||
		view.Timeline[0].SafeFacts["status"] != "proposed" ||
		view.Timeline[1].SafeFacts["status"] != "accepted" {
		t.Fatalf("Decision Timeline = %#v", view.Timeline)
	}
	if len(view.Timeline[0].Subjects) != 1 || view.Timeline[0].Subjects[0].State != subjectState {
		t.Fatalf("Decision proposed subjects = %#v, want state %s", view.Timeline[0].Subjects, subjectState)
	}
	if subjectState == goldenpath.ProjectionRestricted {
		relation := view.Relations[0]
		subject := view.Timeline[0].Subjects[0]
		if relation.RelationType != "" || relation.Target != (entityref.Ref{}) || relation.Title != "" ||
			subject.Ref != (entityref.Ref{}) || subject.Title != "" {
			t.Fatalf("restricted Nexus View leaks relation or subject fields: relation=%#v subject=%#v", relation, subject)
		}
		return
	}
	threadRef := entityref.Ref{Type: "thread", ID: "thr_private"}
	if view.Relations[0].Target != threadRef || view.Relations[0].Title != "Private rate-limit discussion" ||
		view.Timeline[0].Subjects[0].Ref != threadRef ||
		view.Timeline[0].Subjects[0].Title != "Private rate-limit discussion" {
		t.Fatalf("visible Nexus View lost Thread source: relation=%#v subject=%#v", view.Relations[0], view.Timeline[0].Subjects[0])
	}
}

func principal(userID string) authz.Principal {
	return authz.Principal{Kind: authz.PrincipalUser, ID: userID, WorkspaceID: "wrk_main"}
}

func invocation(principal authz.Principal, correlationID string) goldenpath.Invocation {
	return goldenpath.Invocation{
		Principal:     principal,
		SourceKind:    "api",
		CorrelationID: correlationID,
	}
}

func seedGoldenPath(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.users (id, display_name) VALUES
			('usr_contributor', 'Contributor'),
			('usr_decider', 'Decider'),
			('usr_reader', 'Reader'),
			('usr_admin', 'Admin');
		INSERT INTO radishnexus.workspaces (id, name) VALUES
			('wrk_main', 'Main'),
			('wrk_other', 'Other');
		INSERT INTO radishnexus.workspace_memberships (workspace_id, user_id) VALUES
			('wrk_main', 'usr_contributor'),
			('wrk_main', 'usr_decider'),
			('wrk_main', 'usr_reader'),
			('wrk_main', 'usr_admin'),
			('wrk_other', 'usr_contributor');
		INSERT INTO radishnexus.teams (id, workspace_id, name) VALUES
			('tem_main', 'wrk_main', 'Main Team'),
			('tem_other', 'wrk_other', 'Other Team');
		INSERT INTO radishnexus.projects (
			id, workspace_id, key, name, owner_team_id, visibility, status,
			created_by_kind, created_by_id
		) VALUES
			('prj_auth', 'wrk_main', 'AUTH', 'Authentication', 'tem_main', 'workspace', 'active', 'user', 'usr_contributor'),
			('prj_other', 'wrk_other', 'OTHER', 'Other', 'tem_other', 'workspace', 'active', 'user', 'usr_contributor');
		INSERT INTO radishnexus.project_memberships (workspace_id, project_id, user_id, role) VALUES
			('wrk_main', 'prj_auth', 'usr_contributor', 'contributor'),
			('wrk_main', 'prj_auth', 'usr_decider', 'decider'),
			('wrk_main', 'prj_auth', 'usr_reader', 'viewer'),
			('wrk_main', 'prj_auth', 'usr_admin', 'admin');
		INSERT INTO radishnexus.channels (
			id, workspace_id, governing_project_id, name, visibility, status,
			created_by_kind, created_by_id
		) VALUES
			('chn_project', 'wrk_main', 'prj_auth', 'Project Channel', 'project', 'active', 'user', 'usr_contributor'),
			('chn_restricted', 'wrk_main', 'prj_auth', 'Restricted Channel', 'restricted', 'active', 'user', 'usr_contributor'),
			('chn_archived', 'wrk_main', 'prj_auth', 'Archived Channel', 'project', 'archived', 'user', 'usr_contributor'),
			('chn_other', 'wrk_other', 'prj_other', 'Other Channel', 'project', 'active', 'user', 'usr_contributor');
		INSERT INTO radishnexus.channel_memberships (workspace_id, channel_id, user_id) VALUES
			('wrk_main', 'chn_restricted', 'usr_contributor'),
			('wrk_main', 'chn_restricted', 'usr_decider');
		INSERT INTO radishnexus.threads (
			id, workspace_id, governing_project_id, title, visibility, created_by
		) VALUES
			('thr_private', 'wrk_main', 'prj_auth', 'Private rate-limit discussion', 'restricted', 'usr_contributor'),
			('thr_other', 'wrk_other', 'prj_other', 'Other Workspace thread', 'project', 'usr_contributor');
		INSERT INTO radishnexus.thread_memberships (workspace_id, thread_id, user_id) VALUES
			('wrk_main', 'thr_private', 'usr_contributor'),
			('wrk_main', 'thr_private', 'usr_decider');
	`)
	if err != nil {
		t.Fatalf("seed Golden Path error = %v", err)
	}
}

func assertCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, decisions, links, events, outbox int) {
	t.Helper()
	for table, want := range map[string]int{
		"radishnexus.decisions":         decisions,
		"radishnexus.entity_links":      links,
		"radishnexus.domain_events":     events,
		"radishnexus.outbox_deliveries": outbox,
	} {
		assertTableCount(t, ctx, pool, table, want)
	}
}

func assertTableCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil {
		t.Fatalf("count %s error = %v", table, err)
	}
	if got != want {
		t.Fatalf("count %s = %d, want %d", table, got, want)
	}
}

func assertDecisionStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, decisionID, want string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM radishnexus.decisions WHERE id = $1
	`, decisionID).Scan(&got); err != nil {
		t.Fatalf("load Decision status error = %v", err)
	}
	if got != want {
		t.Fatalf("Decision status = %q, want %q", got, want)
	}
}

func assertAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, id string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+table+" WHERE id = $1)", id).Scan(&exists); err != nil {
		t.Fatalf("check %s absence error = %v", table, err)
	}
	if exists {
		t.Fatalf("%s unexpectedly contains %s", table, id)
	}
}

func loadDomainEventID(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	eventType string,
	primaryID string,
) string {
	t.Helper()
	var eventID string
	if err := pool.QueryRow(ctx, `
		SELECT event_id
		FROM radishnexus.domain_events
		WHERE event_type = $1 AND primary_entity_id = $2
	`, eventType, primaryID).Scan(&eventID); err != nil {
		t.Fatalf("load %s event for %s: %v", eventType, primaryID, err)
	}
	return eventID
}

func assertDatabaseConstraints(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	decisionID string,
	eventID string,
) {
	t.Helper()

	_, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.decisions (
			id, workspace_id, governing_project_id, question, status,
			proposer_id, created_by_kind, created_by_id
		) VALUES (
			'dec_without_evidence', 'wrk_main', 'prj_auth', 'Missing evidence?', 'proposed',
			'usr_contributor', 'user', 'usr_contributor'
		)
	`)
	assertPGCode(t, err, "23514", "Decision evidence constraint")

	_, err = pool.Exec(ctx, `
		INSERT INTO radishnexus.entity_links (
			id, workspace_id, from_type, from_id, relation_type, to_type, to_id,
			assertion, origin, created_by_kind, created_by_id
		) VALUES (
			'lnk_cross_workspace', 'wrk_main', 'decision', $1, 'derived-from', 'thread', 'thr_other',
			'asserted', 'user', 'user', 'usr_contributor'
		)
	`, decisionID)
	assertPGCode(t, err, "23514", "cross-Workspace EntityLink constraint")

	_, err = pool.Exec(ctx, `
		UPDATE radishnexus.decisions
		SET status = 'superseded', updated_at = clock_timestamp()
		WHERE id = $1
	`, decisionID)
	assertPGCode(t, err, "23514", "superseded Decision replacement constraint")

	_, err = pool.Exec(ctx, `
		UPDATE radishnexus.domain_events
		SET payload = '{"status":"tampered"}'::jsonb
		WHERE event_id = $1
	`, eventID)
	assertPGCode(t, err, "23514", "immutable domain event constraint")

	_, err = pool.Exec(ctx, `
		UPDATE radishnexus.collaboration_command_receipts
		SET payload_sha256 = repeat('0', 64)
		WHERE workspace_id = 'wrk_main'
		  AND actor_id = 'usr_contributor'
		  AND command_kind = 'decision.propose'
		  AND target_type = 'thread'
		  AND target_id = 'thr_private'
		  AND client_operation_id = 'test:decision:propose'
	`)
	assertPGCode(t, err, "23514", "immutable collaboration command receipt constraint")
}

func assertPGCode(t *testing.T, err error, wantCode, operation string) {
	t.Helper()
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != wantCode {
		t.Fatalf("%s error = %v, want PostgreSQL code %s", operation, err, wantCode)
	}
}
