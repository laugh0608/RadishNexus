//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func assertMessagingSlice(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *goldenpostgres.Store,
	service *goldenpath.Service,
) {
	t.Helper()
	contributor := principal("usr_contributor")
	reader := principal("usr_reader")

	_, err := service.CreateMessage(ctx, invocation(reader, "cor_message_forbidden"), goldenpath.CreateMessageInput{
		ChannelID:         "chn_project",
		ClientOperationID: "web:reader:1",
		Body:              "A viewer cannot send this.",
	})
	if !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("viewer CreateMessage() error = %v, want forbidden", err)
	}
	_, err = service.CreateMessage(ctx, invocation(reader, "cor_message_hidden"), goldenpath.CreateMessageInput{
		ChannelID:         "chn_restricted",
		ClientOperationID: "web:reader:2",
		Body:              "A non-member cannot discover this Channel.",
	})
	if !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("non-member restricted CreateMessage() error = %v, want not found", err)
	}
	_, err = service.CreateMessage(ctx, invocation(contributor, "cor_message_archived"), goldenpath.CreateMessageInput{
		ChannelID:         "chn_archived",
		ClientOperationID: "web:contributor:archived",
		Body:              "Archived channels reject new facts.",
	})
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("archived Channel CreateMessage() error = %v, want conflict", err)
	}

	body := "  preserve exact message body\n"
	input := goldenpath.CreateMessageInput{
		ChannelID:         "chn_restricted",
		ClientOperationID: "web:contributor:root-1",
		Body:              body,
	}
	first, err := service.CreateMessage(ctx, invocation(contributor, "cor_message_create"), input)
	if err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}
	if !first.Created || first.Message.Body != body || first.Message.ChannelID != "chn_restricted" ||
		first.Message.ThreadID != nil {
		t.Fatalf("created Message = %#v", first)
	}
	duplicate, err := service.CreateMessage(ctx, invocation(contributor, "cor_message_retry"), input)
	if err != nil {
		t.Fatalf("duplicate CreateMessage() error = %v", err)
	}
	if duplicate.Created || duplicate.Message.ID != first.Message.ID || duplicate.Message.Body != body {
		t.Fatalf("duplicate Message = %#v, first = %#v", duplicate, first)
	}
	changed := input
	changed.Body = "changed retry body"
	_, err = service.CreateMessage(ctx, invocation(contributor, "cor_message_changed"), changed)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("changed idempotency replay error = %v, want conflict", err)
	}
	assertMessageEventEnvelope(t, ctx, pool, first.Message.ID, body, input.ClientOperationID)

	_, err = service.StartThreadFromMessage(
		ctx,
		invocation(contributor, "cor_thread_scope"),
		goldenpath.StartThreadFromMessageInput{
			MessageID: first.Message.ID, Title: "Must remain restricted", Visibility: "project",
		},
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("restricted Channel project Thread error = %v, want conflict", err)
	}
	thread, err := service.StartThreadFromMessage(
		ctx,
		invocation(contributor, "cor_thread_start"),
		goldenpath.StartThreadFromMessageInput{
			MessageID: first.Message.ID, Title: "Investigate messaging boundary", Visibility: "restricted",
		},
	)
	if err != nil {
		t.Fatalf("StartThreadFromMessage() error = %v", err)
	}
	if thread.OriginChannelID == nil || *thread.OriginChannelID != "chn_restricted" ||
		thread.GoverningProjectID != "prj_auth" || thread.Visibility != "restricted" {
		t.Fatalf("started Thread = %#v", thread)
	}
	assertStartedFromFacts(t, ctx, pool, thread.ID, first.Message.ID, contributor.ID)

	_, err = service.StartThreadFromMessage(
		ctx,
		invocation(contributor, "cor_thread_duplicate"),
		goldenpath.StartThreadFromMessageInput{
			MessageID: first.Message.ID, Title: "Duplicate source", Visibility: "restricted",
		},
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("duplicate source StartThreadFromMessage() error = %v, want conflict", err)
	}

	reply, err := service.CreateMessage(ctx, invocation(contributor, "cor_message_reply"), goldenpath.CreateMessageInput{
		ChannelID:         "chn_restricted",
		ThreadID:          thread.ID,
		ClientOperationID: "web:contributor:reply-1",
		Body:              "Reply inside the Thread.",
	})
	if err != nil || !reply.Created || reply.Message.ThreadID == nil || *reply.Message.ThreadID != thread.ID {
		t.Fatalf("Thread reply = %#v, error = %v", reply, err)
	}

	relations, err := service.ListRelations(ctx, contributor, entityref.Ref{Type: "thread", ID: thread.ID})
	if err != nil {
		t.Fatalf("ListRelations(messaging Thread) error = %v", err)
	}
	if len(relations) != 1 || relations[0].State != goldenpath.ProjectionVisible ||
		relations[0].RelationType != "started-from" ||
		relations[0].Target != (entityref.Ref{Type: "message", ID: first.Message.ID}) ||
		relations[0].Title != "Message" || relations[0].Title == body {
		t.Fatalf("messaging Thread relations = %#v", relations)
	}

	decision, err := service.CreateDecisionFromThread(
		ctx,
		invocation(contributor, "cor_message_decision"),
		goldenpath.CreateDecisionInput{ThreadID: thread.ID, Question: "Adopt the minimal messaging boundary?"},
	)
	if err != nil || decision.GoverningProjectID != "prj_auth" {
		t.Fatalf("CreateDecisionFromThread(messaging Thread) = %#v, error = %v", decision, err)
	}

	if _, err := pool.Exec(ctx, `
		DELETE FROM radishnexus.channel_memberships
		WHERE workspace_id = 'wrk_main'
		  AND channel_id = 'chn_restricted'
		  AND user_id = 'usr_contributor'
	`); err != nil {
		t.Fatalf("revoke Channel membership: %v", err)
	}
	_, err = service.CreateMessage(ctx, invocation(contributor, "cor_message_after_revoke"), input)
	if !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("duplicate Message after Channel revoke error = %v, want not found", err)
	}
	_, err = service.CreateDecisionFromThread(
		ctx,
		invocation(contributor, "cor_decision_after_revoke"),
		goldenpath.CreateDecisionInput{ThreadID: thread.ID, Question: "Should be hidden after revoke?"},
	)
	if !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("messaging Thread after Channel revoke error = %v, want not found", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.channel_memberships (workspace_id, channel_id, user_id)
		VALUES ('wrk_main', 'chn_restricted', 'usr_contributor')
	`); err != nil {
		t.Fatalf("restore Channel membership: %v", err)
	}

	assertMessagingAtomicRollback(t, ctx, pool, store, service, contributor)
	assertConcurrentMessageIdempotency(t, ctx, pool, store, contributor)
	assertMessagingDatabaseConstraints(t, ctx, pool, first.Message.ID, thread.ID)

	if _, err := store.RebuildActivityProjection(ctx); err != nil {
		t.Fatalf("RebuildActivityProjection() after messaging error = %v", err)
	}
	var messagingActivities int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM radishnexus.activity_items AS activity
		JOIN radishnexus.domain_events AS event ON event.event_id = activity.event_id
		WHERE event.event_type IN ('message.created', 'thread.started')
	`).Scan(&messagingActivities); err != nil {
		t.Fatalf("count messaging Activity items: %v", err)
	}
	if messagingActivities != 0 {
		t.Fatalf("messaging Activity items = %d, want 0", messagingActivities)
	}
}

func assertMessageEventEnvelope(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	messageID string,
	body string,
	operationID string,
) {
	t.Helper()
	var payloadBytes []byte
	var consumer string
	if err := pool.QueryRow(ctx, `
		SELECT event.payload, outbox.consumer
		FROM radishnexus.domain_events AS event
		JOIN radishnexus.outbox_deliveries AS outbox USING (event_id)
		WHERE event.event_type = 'message.created'
		  AND event.primary_entity_type = 'message'
		  AND event.primary_entity_id = $1
	`, messageID).Scan(&payloadBytes, &consumer); err != nil {
		t.Fatalf("load Message event envelope: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("decode Message event payload: %v", err)
	}
	if consumer != "realtime-dispatcher" || len(payload) != 1 || payload["channel"] == nil {
		t.Fatalf("Message event payload = %#v, consumer = %q", payload, consumer)
	}
	payloadText := string(payloadBytes)
	if contains(payloadText, body) || contains(payloadText, operationID) {
		t.Fatalf("Message event leaked body or idempotency key: %s", payloadText)
	}
}

func assertStartedFromFacts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	threadID string,
	messageID string,
	creatorID string,
) {
	t.Helper()
	var originChannelID string
	var relationCount int
	var memberCount int
	if err := pool.QueryRow(ctx, `
		SELECT origin_channel_id FROM radishnexus.threads WHERE id = $1
	`, threadID).Scan(&originChannelID); err != nil {
		t.Fatalf("load Thread origin Channel: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM radishnexus.entity_links
		WHERE from_type = 'thread' AND from_id = $1
		  AND relation_type = 'started-from' AND to_type = 'message' AND to_id = $2
	`, threadID, messageID).Scan(&relationCount); err != nil {
		t.Fatalf("count Thread source relations: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM radishnexus.thread_memberships
		WHERE thread_id = $1 AND user_id = $2
	`, threadID, creatorID).Scan(&memberCount); err != nil {
		t.Fatalf("count Thread creator memberships: %v", err)
	}
	if originChannelID != "chn_restricted" || relationCount != 1 || memberCount != 1 {
		t.Fatalf("Thread source facts: channel=%q relations=%d memberships=%d", originChannelID, relationCount, memberCount)
	}
}

func assertMessagingAtomicRollback(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *goldenpostgres.Store,
	service *goldenpath.Service,
	contributor authz.Principal,
) {
	t.Helper()
	source, err := service.CreateMessage(ctx, invocation(contributor, "cor_atomic_source"), goldenpath.CreateMessageInput{
		ChannelID: "chn_restricted", ClientOperationID: "web:contributor:atomic-source", Body: "Atomic source.",
	})
	if err != nil {
		t.Fatalf("create atomic source Message: %v", err)
	}
	duplicateEventID := loadDomainEventID(t, ctx, pool, "message.created", source.Message.ID)
	atomicService := goldenpath.NewService(store, &fixedIDs{values: []string{
		"thr_message_atomic_failure", "lnk_message_atomic_failure", duplicateEventID,
	}}, fixedClock{now: time.Date(2026, 9, 1, 5, 0, 0, 0, time.UTC)})
	_, err = atomicService.StartThreadFromMessage(
		ctx,
		invocation(contributor, "cor_thread_atomic_failure"),
		goldenpath.StartThreadFromMessageInput{
			MessageID: source.Message.ID, Title: "Must roll back", Visibility: "restricted",
		},
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("duplicate event StartThreadFromMessage() error = %v, want conflict", err)
	}
	assertAbsent(t, ctx, pool, "radishnexus.threads", "thr_message_atomic_failure")
	assertAbsent(t, ctx, pool, "radishnexus.entity_links", "lnk_message_atomic_failure")
}

func assertConcurrentMessageIdempotency(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *goldenpostgres.Store,
	contributor authz.Principal,
) {
	t.Helper()
	type outcome struct {
		result goldenpath.CreateMessageResult
		err    error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, 2)
	now := time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC)
	commands := []goldenpath.CreateMessageCommand{
		{
			Invocation: invocation(contributor, "cor_message_concurrent_a"),
			MessageID:  "msg_concurrent_a", EventID: "evt_message_concurrent_a",
			ChannelID: "chn_restricted", ClientOperationID: "web:contributor:concurrent",
			Body: "Concurrent body.", OccurredAt: now,
		},
		{
			Invocation: invocation(contributor, "cor_message_concurrent_b"),
			MessageID:  "msg_concurrent_b", EventID: "evt_message_concurrent_b",
			ChannelID: "chn_restricted", ClientOperationID: "web:contributor:concurrent",
			Body: "Concurrent body.", OccurredAt: now,
		},
	}
	var wait sync.WaitGroup
	for _, command := range commands {
		command := command
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := store.CreateMessage(ctx, command)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(outcomes)

	created := 0
	var resolvedID string
	for outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("concurrent CreateMessage() error = %v", outcome.err)
		}
		if outcome.result.Created {
			created++
		}
		if resolvedID == "" {
			resolvedID = outcome.result.Message.ID
		} else if outcome.result.Message.ID != resolvedID {
			t.Fatalf("concurrent Message IDs differ: %q and %q", resolvedID, outcome.result.Message.ID)
		}
	}
	if created != 1 {
		t.Fatalf("concurrent created results = %d, want 1", created)
	}
	var messageCount int
	var eventCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM radishnexus.messages
		WHERE workspace_id = 'wrk_main'
		  AND channel_id = 'chn_restricted'
		  AND author_id = 'usr_contributor'
		  AND client_operation_id = 'web:contributor:concurrent'
	`).Scan(&messageCount); err != nil {
		t.Fatalf("count concurrent Messages: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM radishnexus.domain_events
		WHERE event_type = 'message.created' AND primary_entity_id = $1
	`, resolvedID).Scan(&eventCount); err != nil {
		t.Fatalf("count concurrent Message events: %v", err)
	}
	if messageCount != 1 || eventCount != 1 {
		t.Fatalf("concurrent facts: messages=%d events=%d", messageCount, eventCount)
	}
}

func assertMessagingDatabaseConstraints(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	messageID string,
	threadID string,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `UPDATE radishnexus.messages SET body = 'tampered' WHERE id = $1`, messageID)
	assertPGCode(t, err, "23514", "immutable Message constraint")

	_, err = pool.Exec(ctx, `
		INSERT INTO radishnexus.messages (
			id, workspace_id, channel_id, author_id,
			body, client_operation_id, created_at
		) VALUES (
			'msg_whitespace_body', 'wrk_main', 'chn_project', 'usr_contributor',
			E' \n\t', 'web:whitespace-body', clock_timestamp()
		)
	`)
	assertPGCode(t, err, "23514", "Message non-whitespace body constraint")

	_, err = pool.Exec(ctx, `
		INSERT INTO radishnexus.messages (
			id, workspace_id, channel_id, author_id,
			body, client_operation_id, created_at
		) VALUES (
			'msg_control_operation', 'wrk_main', 'chn_project', 'usr_contributor',
			'Valid body', 'web:' || chr(7), clock_timestamp()
		)
	`)
	assertPGCode(t, err, "23514", "Message printable operation ID constraint")

	_, err = pool.Exec(ctx, `
		INSERT INTO radishnexus.threads (
			id, workspace_id, governing_project_id, origin_channel_id,
			title, visibility, created_by
		) VALUES (
			'thr_without_message_source', 'wrk_main', 'prj_auth', 'chn_restricted',
			'Missing source', 'restricted', 'usr_contributor'
		)
	`)
	assertPGCode(t, err, "23514", "messaging Thread source constraint")

	_, err = pool.Exec(ctx, `
		INSERT INTO radishnexus.messages (
			id, workspace_id, channel_id, thread_id, author_id,
			body, client_operation_id, created_at
		) VALUES (
			'msg_cross_channel_reply', 'wrk_main', 'chn_project', $1, 'usr_contributor',
			'Cross-channel reply', 'web:cross-channel', clock_timestamp()
		)
	`, threadID)
	assertPGCode(t, err, "23503", "cross-Channel Message reply constraint")

	_, err = pool.Exec(ctx, `
		DELETE FROM radishnexus.entity_links
		WHERE from_type = 'thread' AND from_id = $1 AND relation_type = 'started-from'
	`, threadID)
	assertPGCode(t, err, "23514", "immutable Thread source relation")
}

func contains(value string, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
