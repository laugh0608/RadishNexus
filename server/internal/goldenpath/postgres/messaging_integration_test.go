//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
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
	decider := principal("usr_decider")
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
			ChannelID: "chn_restricted", MessageID: first.Message.ID, Title: "Must remain restricted", Visibility: "project",
		},
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("restricted Channel project Thread error = %v, want conflict", err)
	}
	thread, err := service.StartThreadFromMessage(
		ctx,
		invocation(contributor, "cor_thread_start"),
		goldenpath.StartThreadFromMessageInput{
			ChannelID: "chn_restricted", MessageID: first.Message.ID, Title: "Investigate messaging boundary", Visibility: "restricted",
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
			ChannelID: "chn_restricted", MessageID: first.Message.ID, Title: "Duplicate source", Visibility: "restricted",
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
	assertRestrictedChannelMessageQuery(
		t,
		ctx,
		service,
		contributor,
		decider,
		reader,
		first.Message,
		reply.Message,
	)
	assertCanonicalMessagePagination(t, ctx, store, service, contributor)

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

	decisionResult, err := service.CreateDecisionFromThread(
		ctx,
		invocation(contributor, "cor_message_decision"),
		goldenpath.CreateDecisionInput{
			ThreadID:          thread.ID,
			ClientOperationID: "test:message:decision",
			Question:          "Adopt the minimal messaging boundary?",
		},
	)
	if err != nil || decisionResult.Decision.GoverningProjectID != "prj_auth" {
		t.Fatalf("CreateDecisionFromThread(messaging Thread) = %#v, error = %v", decisionResult, err)
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
	_, err = service.ListChannelMessages(ctx, contributor, goldenpath.ListChannelMessagesInput{
		ChannelID: "chn_restricted",
		Limit:     10,
	})
	if !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("ListChannelMessages() after Channel revoke error = %v, want not found", err)
	}
	_, err = service.CreateDecisionFromThread(
		ctx,
		invocation(contributor, "cor_decision_after_revoke"),
		goldenpath.CreateDecisionInput{
			ThreadID:          thread.ID,
			ClientOperationID: "test:message:decision:revoked",
			Question:          "Should be hidden after revoke?",
		},
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
	assertMessagingHTTPTransport(t, ctx, pool, service)

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

func assertMessagingHTTPTransport(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	service *goldenpath.Service,
) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	sessionToken := deploymentHTTPToken(11)
	csrfToken := deploymentHTTPToken(12)
	deciderSessionToken := deploymentHTTPToken(21)
	deciderCSRFToken := deploymentHTTPToken(22)
	tokenDigest := sha256.Sum256([]byte(sessionToken))
	csrfDigest := sha256.Sum256([]byte(csrfToken))
	deciderTokenDigest := sha256.Sum256([]byte(deciderSessionToken))
	deciderCSRFDigest := sha256.Sum256([]byte(deciderCSRFToken))
	if _, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.local_accounts (
			user_id, login_name, password_hash, created_at, password_changed_at
		)
		SELECT 'usr_contributor', 'http.contributor', password_hash, $1::timestamptz, $1::timestamptz
		FROM radishnexus.local_accounts
		WHERE user_id = 'usr_reader'
		UNION ALL
		SELECT 'usr_decider', 'http.decider', password_hash, $1::timestamptz, $1::timestamptz
		FROM radishnexus.local_accounts
		WHERE user_id = 'usr_reader'
	`, now); err != nil {
		t.Fatalf("seed messaging HTTP local account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.user_sessions (
			id, user_id, token_digest, csrf_token_digest, created_at, expires_at
		) VALUES
			('ses_messaging_http', 'usr_contributor', $1, $2, $5, $6),
			('ses_collaboration_decider', 'usr_decider', $3, $4, $5, $6)
	`, tokenDigest[:], csrfDigest[:], deciderTokenDigest[:], deciderCSRFDigest[:],
		now, now.Add(authn.SessionLifetime)); err != nil {
		t.Fatalf("seed messaging HTTP Session: %v", err)
	}
	authService := authn.NewService(authpostgres.New(pool), nil, nil, fixedClock{now: now})
	sessionPolicy, err := httptransport.NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatalf("NewBrowserSessionPolicy() error = %v", err)
	}
	proxyPolicy, err := httptransport.NewTrustedProxyPolicy("10.0.0.0/8")
	if err != nil {
		t.Fatalf("NewTrustedProxyPolicy() error = %v", err)
	}
	handler := httptransport.WithRequestID(httptransport.NewChannelMessagesHandler(
		authService,
		service,
		sessionPolicy,
		proxyPolicy,
	))
	collaborationHandler := httptransport.WithRequestID(httptransport.NewCollaborationHandler(
		authService,
		service,
		sessionPolicy,
		proxyPolicy,
	))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodGet,
		"/api/v1/workspaces/wrk_main/channels/chn_project/messages?limit=2",
		"",
		sessionToken,
		csrfToken,
	))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"older_cursor":"`) ||
		strings.Contains(response.Body.String(), "client_operation") {
		t.Fatalf("HTTP canonical Message page = status %d, body %q", response.Code, response.Body.String())
	}

	createBody := `{"client_operation_id":"http:message:1","body":"Authoritative HTTP message."}`
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		"/api/v1/workspaces/wrk_main/channels/chn_project/messages",
		createBody,
		sessionToken,
		csrfToken,
	))
	if response.Code != http.StatusCreated {
		t.Fatalf("HTTP create Message = status %d, body %q", response.Code, response.Body.String())
	}
	var created struct {
		Data struct {
			Ref struct {
				ID string `json:"id"`
			} `json:"ref"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.Data.Ref.ID == "" {
		t.Fatalf("decode HTTP created Message = %#v, error = %v", created, err)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		"/api/v1/workspaces/wrk_main/channels/chn_project/messages",
		createBody,
		sessionToken,
		csrfToken,
	))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), created.Data.Ref.ID) {
		t.Fatalf("HTTP exact Message retry = status %d, body %q", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		"/api/v1/workspaces/wrk_main/channels/chn_project/messages",
		`{"client_operation_id":"http:message:1","body":"Changed replay."}`,
		sessionToken,
		csrfToken,
	))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"conflict"`) {
		t.Fatalf("HTTP changed Message retry = status %d, body %q", response.Code, response.Body.String())
	}

	threadPath := "/api/v1/workspaces/wrk_main/channels/chn_project/messages/" + created.Data.Ref.ID + "/threads"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		threadPath,
		`{"title":"HTTP source discussion","visibility":"restricted"}`,
		sessionToken,
		csrfToken,
	))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"source_message":{"type":"message","id":"`+created.Data.Ref.ID+`"}`) {
		t.Fatalf("HTTP start Thread = status %d, body %q", response.Code, response.Body.String())
	}
	var createdThread struct {
		Data struct {
			Ref struct {
				ID string `json:"id"`
			} `json:"ref"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &createdThread); err != nil || createdThread.Data.Ref.ID == "" {
		t.Fatalf("decode HTTP created Thread = %#v, error = %v", createdThread, err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.thread_memberships (workspace_id, thread_id, user_id)
		VALUES ('wrk_main', $1, 'usr_decider')
	`, createdThread.Data.Ref.ID); err != nil {
		t.Fatalf("grant HTTP Thread evidence to decider: %v", err)
	}

	assertCollaborationHTTPTransport(
		t,
		ctx,
		pool,
		collaborationHandler,
		createdThread.Data.Ref.ID,
		sessionToken,
		csrfToken,
		deciderSessionToken,
		deciderCSRFToken,
	)

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		"/api/v1/workspaces/wrk_main/channels/chn_restricted/messages/"+created.Data.Ref.ID+"/threads",
		`{"title":"Wrong Channel","visibility":"restricted"}`,
		sessionToken,
		csrfToken,
	))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatalf("HTTP cross-Channel source = status %d, body %q", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodGet,
		"/api/v1/workspaces/wrk_main/channels/chn_project/messages?before=invalid",
		"",
		sessionToken,
		csrfToken,
	))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid"`) {
		t.Fatalf("HTTP invalid Message cursor = status %d, body %q", response.Code, response.Body.String())
	}

	wrongCSRFToken := deploymentHTTPToken(13)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		"/api/v1/workspaces/wrk_main/channels/chn_project/messages",
		`{"client_operation_id":"http:csrf:failed","body":"Must not write."}`,
		sessionToken,
		wrongCSRFToken,
	))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"csrf_failed"`) {
		t.Fatalf("HTTP stored CSRF mismatch = status %d, body %q", response.Code, response.Body.String())
	}

	if _, err := pool.Exec(ctx, `
		DELETE FROM radishnexus.channel_memberships
		WHERE workspace_id = 'wrk_main'
		  AND channel_id = 'chn_restricted'
		  AND user_id = 'usr_contributor'
	`); err != nil {
		t.Fatalf("revoke Channel membership for HTTP: %v", err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodGet,
		"/api/v1/workspaces/wrk_main/channels/chn_restricted/messages",
		"",
		sessionToken,
		csrfToken,
	))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatalf("HTTP revoked Channel = status %d, body %q", response.Code, response.Body.String())
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.channel_memberships (workspace_id, channel_id, user_id)
		VALUES ('wrk_main', 'chn_restricted', 'usr_contributor')
	`); err != nil {
		t.Fatalf("restore Channel membership after HTTP: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE radishnexus.user_sessions
		SET revoked_at = $1
		WHERE id = 'ses_messaging_http'
	`, now); err != nil {
		t.Fatalf("revoke messaging HTTP Session: %v", err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodGet,
		"/api/v1/workspaces/wrk_main/channels/chn_project/messages",
		"",
		sessionToken,
		csrfToken,
	))
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"unauthenticated"`) {
		t.Fatalf("HTTP revoked Session = status %d, body %q", response.Code, response.Body.String())
	}
}

func assertCollaborationHTTPTransport(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	handler http.Handler,
	threadID string,
	contributorSessionToken string,
	contributorCSRFToken string,
	deciderSessionToken string,
	deciderCSRFToken string,
) {
	t.Helper()
	threadViewPath := "/api/v1/workspaces/wrk_main/threads/" + threadID + "/nexus-view"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodGet,
		threadViewPath,
		"",
		contributorSessionToken,
		contributorCSRFToken,
	))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"relation_type":"started-from"`) ||
		strings.Contains(response.Body.String(), "Authoritative HTTP message.") {
		t.Fatalf("HTTP Thread Nexus View = status %d, body %q", response.Code, response.Body.String())
	}

	decisionPath := "/api/v1/workspaces/wrk_main/threads/" + threadID + "/decisions"
	decisionBody := `{"client_operation_id":"http:decision:1","question":"Adopt the HTTP collaboration boundary?"}`
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		decisionPath,
		decisionBody,
		contributorSessionToken,
		contributorCSRFToken,
	))
	if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), "client_operation") {
		t.Fatalf("HTTP propose Decision = status %d, body %q", response.Code, response.Body.String())
	}
	var createdDecision struct {
		Data struct {
			Decision struct {
				Ref struct {
					ID string `json:"id"`
				} `json:"ref"`
			} `json:"decision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &createdDecision); err != nil || createdDecision.Data.Decision.Ref.ID == "" {
		t.Fatalf("decode HTTP proposed Decision = %#v, error = %v", createdDecision, err)
	}
	decisionID := createdDecision.Data.Decision.Ref.ID

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		decisionPath,
		decisionBody,
		contributorSessionToken,
		contributorCSRFToken,
	))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), decisionID) {
		t.Fatalf("HTTP exact Decision proposal retry = status %d, body %q", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		decisionPath,
		`{"client_operation_id":"http:decision:1","question":"Changed replay"}`,
		contributorSessionToken,
		contributorCSRFToken,
	))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"conflict"`) {
		t.Fatalf("HTTP changed Decision proposal replay = status %d, body %q", response.Code, response.Body.String())
	}

	acceptPath := "/api/v1/workspaces/wrk_main/decisions/" + decisionID + "/acceptance"
	acceptBody := `{"client_operation_id":"http:accept:1","outcome":"Use the Session boundary.","rationale":"It preserves current authority.","confirmed":true}`
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		acceptPath,
		acceptBody,
		contributorSessionToken,
		contributorCSRFToken,
	))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("HTTP contributor Decision acceptance = status %d, body %q", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		acceptPath,
		acceptBody,
		deciderSessionToken,
		deciderCSRFToken,
	))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"accepted"`) {
		t.Fatalf("HTTP decider Decision acceptance = status %d, body %q", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		acceptPath,
		acceptBody,
		deciderSessionToken,
		deciderCSRFToken,
	))
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP exact Decision acceptance retry = status %d, body %q", response.Code, response.Body.String())
	}

	ticketPath := "/api/v1/workspaces/wrk_main/decisions/" + decisionID + "/tickets"
	ticketBody := `{"client_operation_id":"http:ticket:1","title":"Implement HTTP collaboration"}`
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		ticketPath,
		ticketBody,
		contributorSessionToken,
		contributorCSRFToken,
	))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"source_decision":{"type":"decision","id":"`+decisionID+`"}`) {
		t.Fatalf("HTTP create Ticket = status %d, body %q", response.Code, response.Body.String())
	}
	var createdTicket struct {
		Data struct {
			Ticket struct {
				Ref struct {
					ID string `json:"id"`
				} `json:"ref"`
			} `json:"ticket"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &createdTicket); err != nil || createdTicket.Data.Ticket.Ref.ID == "" {
		t.Fatalf("decode HTTP created Ticket = %#v, error = %v", createdTicket, err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		ticketPath,
		ticketBody,
		contributorSessionToken,
		contributorCSRFToken,
	))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), createdTicket.Data.Ticket.Ref.ID) {
		t.Fatalf("HTTP exact Ticket retry = status %d, body %q", response.Code, response.Body.String())
	}

	for _, viewPath := range []string{
		"/api/v1/workspaces/wrk_main/decisions/" + decisionID + "/nexus-view",
		"/api/v1/workspaces/wrk_main/tickets/" + createdTicket.Data.Ticket.Ref.ID + "/nexus-view",
	} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, messagingHTTPRequest(
			http.MethodGet,
			viewPath,
			"",
			contributorSessionToken,
			contributorCSRFToken,
		))
		if response.Code != http.StatusOK {
			t.Fatalf("HTTP collaboration Nexus View %s = status %d, body %q", viewPath, response.Code, response.Body.String())
		}
	}

	if _, err := pool.Exec(ctx, `
		DELETE FROM radishnexus.thread_memberships
		WHERE workspace_id = 'wrk_main' AND thread_id = $1 AND user_id = 'usr_decider'
	`, threadID); err != nil {
		t.Fatalf("revoke decider HTTP evidence: %v", err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, messagingHTTPRequest(
		http.MethodPost,
		acceptPath,
		acceptBody,
		deciderSessionToken,
		deciderCSRFToken,
	))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("HTTP acceptance retry after evidence revoke = status %d, body %q", response.Code, response.Body.String())
	}
}

func messagingHTTPRequest(
	method string,
	path string,
	body string,
	sessionToken string,
	csrfToken string,
) *http.Request {
	request := httptest.NewRequest(method, "https://nexus.example.test"+path, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: sessionToken})
	if method != http.MethodGet {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://nexus.example.test")
		request.Header.Set(httptransport.CSRFHeaderName, csrfToken)
		request.AddCookie(&http.Cookie{Name: httptransport.CSRFCookieName, Value: csrfToken})
	}
	return request
}

func assertRestrictedChannelMessageQuery(
	t *testing.T,
	ctx context.Context,
	service *goldenpath.Service,
	contributor authz.Principal,
	decider authz.Principal,
	reader authz.Principal,
	root goldenpath.Message,
	reply goldenpath.Message,
) {
	t.Helper()
	contributorPage, err := service.ListChannelMessages(
		ctx,
		contributor,
		goldenpath.ListChannelMessagesInput{ChannelID: "chn_restricted", Limit: 10},
	)
	if err != nil {
		t.Fatalf("contributor ListChannelMessages() error = %v", err)
	}
	assertMessagePageIDs(t, contributorPage, root.ID, reply.ID)
	if contributorPage.OlderCursor != nil || contributorPage.Messages[0].Body != root.Body ||
		contributorPage.Messages[1].Body != reply.Body ||
		contributorPage.Messages[1].ThreadID == nil ||
		*contributorPage.Messages[1].ThreadID != *reply.ThreadID {
		t.Fatalf("contributor Message page = %#v", contributorPage)
	}

	deciderPage, err := service.ListChannelMessages(
		ctx,
		decider,
		goldenpath.ListChannelMessagesInput{ChannelID: "chn_restricted", Limit: 1},
	)
	if err != nil {
		t.Fatalf("Channel member ListChannelMessages() error = %v", err)
	}
	assertMessagePageIDs(t, deciderPage, root.ID)
	if deciderPage.Messages[0].ThreadID != nil {
		t.Fatalf("Channel member page leaked restricted Thread reply: %#v", deciderPage)
	}

	_, err = service.ListChannelMessages(
		ctx,
		reader,
		goldenpath.ListChannelMessagesInput{ChannelID: "chn_restricted", Limit: 10},
	)
	if !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("non-member ListChannelMessages() error = %v, want not found", err)
	}
	archivedPage, err := service.ListChannelMessages(
		ctx,
		reader,
		goldenpath.ListChannelMessagesInput{ChannelID: "chn_archived", Limit: 10},
	)
	if err != nil || len(archivedPage.Messages) != 0 || archivedPage.OlderCursor != nil {
		t.Fatalf("archived Channel Message page = %#v, error = %v", archivedPage, err)
	}
}

func assertCanonicalMessagePagination(
	t *testing.T,
	ctx context.Context,
	store *goldenpostgres.Store,
	service *goldenpath.Service,
	contributor authz.Principal,
) {
	t.Helper()
	base := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	fixtures := []struct {
		id        string
		eventID   string
		operation string
		body      string
		createdAt time.Time
	}{
		{id: "msg_page_a", eventID: "evt_message_page_a", operation: "page:a", body: "Page A", createdAt: base},
		{id: "msg_page_b", eventID: "evt_message_page_b", operation: "page:b", body: "Page B", createdAt: base.Add(time.Minute)},
		{id: "msg_page_c", eventID: "evt_message_page_c", operation: "page:c", body: "Page C", createdAt: base.Add(time.Minute)},
		{id: "msg_page_d", eventID: "evt_message_page_d", operation: "page:d", body: "Page D", createdAt: base.Add(2 * time.Minute)},
		{id: "msg_page_e", eventID: "evt_message_page_e", operation: "page:e", body: "Page E", createdAt: base.Add(3 * time.Minute)},
	}
	for _, fixture := range fixtures {
		result, err := store.CreateMessage(ctx, goldenpath.CreateMessageCommand{
			Invocation:        invocation(contributor, "cor_"+fixture.operation),
			MessageID:         fixture.id,
			EventID:           fixture.eventID,
			ChannelID:         "chn_project",
			ClientOperationID: fixture.operation,
			Body:              fixture.body,
			OccurredAt:        fixture.createdAt,
		})
		if err != nil || !result.Created {
			t.Fatalf("seed canonical Message %s = %#v, error = %v", fixture.id, result, err)
		}
	}

	firstPage, err := service.ListChannelMessages(
		ctx,
		contributor,
		goldenpath.ListChannelMessagesInput{ChannelID: "chn_project", Limit: 2},
	)
	if err != nil {
		t.Fatalf("first canonical Message page error = %v", err)
	}
	assertMessagePageIDs(t, firstPage, "msg_page_d", "msg_page_e")
	assertOlderCursor(t, firstPage, "msg_page_d", base.Add(2*time.Minute))

	newest, err := store.CreateMessage(ctx, goldenpath.CreateMessageCommand{
		Invocation:        invocation(contributor, "cor_page_f"),
		MessageID:         "msg_page_f",
		EventID:           "evt_message_page_f",
		ChannelID:         "chn_project",
		ClientOperationID: "page:f",
		Body:              "Page F",
		OccurredAt:        base.Add(4 * time.Minute),
	})
	if err != nil || !newest.Created {
		t.Fatalf("seed newer canonical Message = %#v, error = %v", newest, err)
	}

	secondPage, err := service.ListChannelMessages(
		ctx,
		contributor,
		goldenpath.ListChannelMessagesInput{
			ChannelID: "chn_project", Before: firstPage.OlderCursor, Limit: 2,
		},
	)
	if err != nil {
		t.Fatalf("second canonical Message page error = %v", err)
	}
	assertMessagePageIDs(t, secondPage, "msg_page_b", "msg_page_c")
	assertOlderCursor(t, secondPage, "msg_page_b", base.Add(time.Minute))

	thirdPage, err := service.ListChannelMessages(
		ctx,
		contributor,
		goldenpath.ListChannelMessagesInput{
			ChannelID: "chn_project", Before: secondPage.OlderCursor, Limit: 2,
		},
	)
	if err != nil {
		t.Fatalf("third canonical Message page error = %v", err)
	}
	assertMessagePageIDs(t, thirdPage, "msg_page_a")
	if thirdPage.OlderCursor != nil {
		t.Fatalf("last canonical Message page has older cursor: %#v", thirdPage.OlderCursor)
	}

	refreshed, err := service.ListChannelMessages(
		ctx,
		contributor,
		goldenpath.ListChannelMessagesInput{ChannelID: "chn_project", Limit: 2},
	)
	if err != nil {
		t.Fatalf("refreshed canonical Message page error = %v", err)
	}
	assertMessagePageIDs(t, refreshed, "msg_page_e", "msg_page_f")

	viewerPage, err := service.ListChannelMessages(
		ctx,
		principal("usr_reader"),
		goldenpath.ListChannelMessagesInput{ChannelID: "chn_project", Limit: 2},
	)
	if err != nil {
		t.Fatalf("Project viewer canonical Message page error = %v", err)
	}
	assertMessagePageIDs(t, viewerPage, "msg_page_e", "msg_page_f")
}

func assertMessagePageIDs(t *testing.T, page goldenpath.MessagePage, want ...string) {
	t.Helper()
	if len(page.Messages) != len(want) {
		t.Fatalf("Message page length = %d, want %d: %#v", len(page.Messages), len(want), page)
	}
	for index, messageID := range want {
		if page.Messages[index].ID != messageID {
			t.Fatalf("Message page[%d].ID = %q, want %q: %#v", index, page.Messages[index].ID, messageID, page)
		}
	}
}

func assertOlderCursor(
	t *testing.T,
	page goldenpath.MessagePage,
	wantMessageID string,
	wantCreatedAt time.Time,
) {
	t.Helper()
	if page.OlderCursor == nil || page.OlderCursor.MessageID != wantMessageID ||
		!page.OlderCursor.CreatedAt.Equal(wantCreatedAt) {
		t.Fatalf("Message older cursor = %#v, want %s at %v", page.OlderCursor, wantMessageID, wantCreatedAt)
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
			ChannelID: "chn_restricted", MessageID: source.Message.ID, Title: "Must roll back", Visibility: "restricted",
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
