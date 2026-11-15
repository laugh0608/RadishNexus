//go:build integration

package postgres_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

func executionWriteHTTP(t *testing.T, s *httptest.Server, auth authn.Session, method, path, body string, want int) []byte {
	t.Helper()
	req, err := http.NewRequest(method, s.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", s.URL)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httptransport.CSRFHeaderName, auth.CSRFToken)
	req.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: auth.Token})
	req.AddCookie(&http.Cookie{Name: httptransport.CSRFCookieName, Value: auth.CSRFToken})
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != want || resp.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("%s %s returned %d: %s (%v)", method, path, resp.StatusCode, raw, err)
	}
	return raw
}

func TestTicketExecutionHTTPPermissionsAndRoundTrip(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	legacy := f.bundle(t)
	legacy = legacyTicketClassificationBundle(t, legacy, cp.ProfileV7)
	digest := cp.Digest(legacy.Manifest)
	if _, _, err := cp.Unpack(legacy); err != nil || cp.Digest(legacy.Manifest) != digest {
		t.Fatal("v7 compatibility", err)
	}
	ch := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: f.project, Name: "Execution chat", Visibility: "project", MemberUserIDs: []string{}}).Object.ID
	inv := contextInvocation(f.principal)
	message, err := f.core.CreateMessage(f.ctx, inv, goldenpath.CreateMessageInput{ChannelID: ch, ClientOperationID: "execution-top", Body: "Original synthetic message"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := f.core.StartThreadFromMessage(f.ctx, inv, goldenpath.StartThreadFromMessageInput{ChannelID: ch, MessageID: message.Message.ID, Title: "Execution Thread", Visibility: "project"})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := contextHTTPS(t, f, "")
	workspace := "/api/v1/workspaces/" + f.workspace
	channel := workspace + "/channels/" + ch
	messageEndpoint := channel + "/messages/" + message.Message.ID
	threadEndpoint := workspace + "/threads/" + thread.ID
	replyBody := string(cp.Encode(map[string]any{"client_operation_id": "execution-reply", "thread_id": thread.ID, "body": "Reply synthetic body"}))
	replyRaw := executionWriteHTTP(t, s, f.owner, "POST", channel+"/messages", replyBody, 201)
	var reply struct{ Data struct{ Ref cp.Ref } }
	if err := json.Unmarshal(replyRaw, &reply); err != nil || reply.Data.Ref.Type != "message" {
		t.Fatal(string(replyRaw), err)
	}

	create := func(path, op, title string, want int) string {
		t.Helper()
		body := cp.Encode(map[string]any{"client_operation_id": op, "title": title, "confirmed": true})
		raw := executionWriteHTTP(t, s, f.owner, "POST", path+"/tickets", string(body), want)
		if want > 299 {
			return ""
		}
		var out struct {
			Data struct {
				Ticket        struct{ Ref cp.Ref }
				SourceMessage struct{ Ref cp.Ref } `json:"source_message"`
			}
		}
		if err := json.Unmarshal(raw, &out); err != nil || out.Data.SourceMessage.Ref.Type != "message" || !strings.HasSuffix(path, "/"+out.Data.SourceMessage.Ref.ID) || strings.Contains(string(raw), "synthetic message") {
			t.Fatal(string(raw), err)
		}
		return out.Data.Ticket.Ref.ID
	}
	create(workspace+"/channels/"+f.channel+"/messages/"+f.message, "no-private", "Restricted", 409)
	id := create(messageEndpoint, "source-retry", "Public execution", 201)
	if got := create(messageEndpoint, "source-retry", "Public execution", 200); got != id {
		t.Fatal(got, id)
	}
	create(messageEndpoint, "source-retry", "Conflicting input", 409)
	replyID := create(channel+"/messages/"+reply.Data.Ref.ID, "reply-source", "Reply execution", 201)
	if replyID == "" || replyID == id {
		t.Fatal(replyID, id)
	}
	contextHTTPRead(t, s, f.owner, messageEndpoint, 200)
	contextHTTPRead(t, s, f.owner, threadEndpoint+"/messages?limit=1", 200)
	ticketEndpoint := workspace + "/tickets/" + id
	contextHTTPRead(t, s, f.owner, ticketEndpoint+"/nexus-view", 409)
	body := string(contextHTTPRead(t, s, f.owner, ticketEndpoint+"/nexus-view?include=due_date,execution_context", 200))
	if !strings.Contains(body, `"source_message"`) || !strings.Contains(body, `"relation_type":"started-from"`) || strings.Contains(body, "Original synthetic message") {
		t.Fatal(body)
	}
	contextHTTPRead(t, s, f.owner, workspace+"/tickets/"+f.ticket+"/nexus-view", 200)
	if body = string(contextHTTPRead(t, s, f.owner, messageEndpoint+"/tickets?limit=1", 200)); !strings.Contains(body, id) {
		t.Fatal(body)
	}

	linkBody := func(op, thread string) string {
		return string(cp.Encode(map[string]any{"client_operation_id": op, "thread_id": thread, "confirmed": true}))
	}
	link := func(op, thread string, want int) string {
		t.Helper()
		raw := executionWriteHTTP(t, s, f.owner, "POST", ticketEndpoint+"/discussions", linkBody(op, thread), want)
		if want > 299 {
			return ""
		}
		var result struct{ Data struct{ Link cp.Ref } }
		if err := json.Unmarshal(raw, &result); err != nil || result.Data.Link.Type != "entity-link" {
			t.Fatal(string(raw), err)
		}
		return result.Data.Link.ID
	}
	linkID := link("attach", thread.ID, 201)
	if got := link("attach", thread.ID, 200); got != linkID {
		t.Fatal(got, linkID)
	}
	link("competing", thread.ID, 409)
	if body = string(contextHTTPRead(t, s, f.owner, threadEndpoint+"/tickets", 200)); !strings.Contains(body, id) {
		t.Fatal(body)
	}
	unlinkBody := `{"client_operation_id":"remove","confirmed":true}`
	unlink := ticketEndpoint + "/discussion-links/" + linkID
	executionWriteHTTP(t, s, f.owner, "DELETE", unlink, unlinkBody, 200)
	newLink := link("attach-later", thread.ID, 201)
	executionWriteHTTP(t, s, f.owner, "DELETE", unlink, unlinkBody, 200)
	if body = string(contextHTTPRead(t, s, f.owner, ticketEndpoint+"/discussions", 200)); !strings.Contains(body, newLink) || strings.Contains(body, linkID) {
		t.Fatal(body)
	}

	hiddenMessage, err := f.core.CreateMessage(f.ctx, contextInvocation(f.member), goldenpath.CreateMessageInput{ChannelID: ch, ClientOperationID: "hidden-source", Body: "Synthetic source for private replies"})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := f.core.StartThreadFromMessage(f.ctx, contextInvocation(f.member), goldenpath.StartThreadFromMessageInput{ChannelID: ch, MessageID: hiddenMessage.Message.ID, Title: "Synthetic restricted discussion", Visibility: "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	service := goldenpath.NewTicketExecutionService(f.store, goldenpath.CryptoIDGenerator{})

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, operation := range []string{"competing-one", "competing-two"} {
		go func() {
			<-start
			_, e := service.Write(f.ctx, inv, goldenpath.TicketDiscussionLink, goldenpath.TicketExecutionInput{TicketID: replyID, ThreadID: thread.ID, ClientOperationID: operation, Confirmed: true})
			results <- e
		}()
	}
	close(start)
	firstErr, secondErr := <-results, <-results
	if !((firstErr == nil && errors.Is(secondErr, authz.ErrConflict)) || (secondErr == nil && errors.Is(firstErr, authz.ErrConflict))) {
		t.Fatal("concurrent discussions", firstErr, secondErr)
	}
	private, err := service.Write(f.ctx, contextInvocation(f.member), goldenpath.TicketDiscussionLink, goldenpath.TicketExecutionInput{TicketID: id, ThreadID: hidden.ID, ClientOperationID: "private-link", Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"/discussions", "/nexus-view?include=execution_context"} {
		body = string(contextHTTPRead(t, s, f.owner, ticketEndpoint+suffix, 200))
		if strings.Contains(body, hidden.ID) || strings.Contains(body, private.LinkID) || strings.Contains(body, "Synthetic restricted discussion") {
			t.Fatal(body)
		}
	}
	contextHTTPRead(t, s, f.owner, workspace+"/threads/"+hidden.ID+"/tickets", 404)
	executionWriteHTTP(t, s, f.owner, "DELETE", ticketEndpoint+"/discussion-links/"+private.LinkID, `{"client_operation_id":"guess-link","confirmed":true}`, 404)
	bundle := f.bundle(t)
	legacyExecution := legacyTicketClassificationBundle(t, bundle, cp.ProfileV8)
	before := cp.Digest(legacyExecution.Manifest)
	_, legacyIndex, err := cp.Unpack(legacyExecution)
	if err != nil || cp.Digest(legacyExecution.Manifest) != before {
		t.Fatal("v8 Message execution history", err)
	}
	hasMessageSource := false
	for _, event := range legacyIndex.History {
		if event.Entity.ID == id && event.Type == "ticket.created" && event.Version == 2 {
			hasMessageSource = true
		}
	}
	if !hasMessageSource {
		t.Fatal("v8 normalization lost Message source")
	}

	if bundle.Manifest.Profile != cp.Profile {
		t.Fatal(bundle.Manifest.Profile)
	}
	for _, file := range bundle.Files {
		if strings.Contains(string(file.Content), hidden.ID) || strings.Contains(string(file.Content), private.LinkID) {
			t.Fatal("hidden package data", file.Path)
		}
	}
	incompatible := bundle
	incompatible.Manifest.Profile = cp.ProfileV7
	if _, _, err = cp.Unpack(incompatible); err == nil {
		t.Fatal("old profile accepted new facts")
	}

	target := contextDatabase(t)
	plan := target.plan(t, bundle)
	applied, err := target.service.Apply(target.ctx, target.principal, target.owner.Token, bundle, plan, plan.PlanDigest)
	if err != nil {
		t.Fatal(err)
	}
	targetTicket := contextMapping(t, plan, "ticket", id)
	targetMessage := contextMapping(t, plan, "message", message.Message.ID)
	targetLink := contextMapping(t, plan, "entity-link", newLink)
	targetView, err := target.core.GetNexusView(target.ctx, target.principal, entityref.Ref{Type: "ticket", ID: targetTicket})
	if err != nil || targetView.TicketMessageSource == nil || targetView.TicketMessageSource.MessageID != targetMessage {
		t.Fatal(targetView, err)
	}
	if _, err = target.store.RebuildActivityProjection(target.ctx); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := target.core.GetNexusView(target.ctx, target.principal, entityref.Ref{Type: "ticket", ID: targetTicket})
	if err != nil || !reflect.DeepEqual(rebuilt.Timeline, targetView.Timeline) {
		t.Fatal("import Activity rebuild drift", err)
	}
	targetService := goldenpath.NewTicketExecutionService(target.store, goldenpath.CryptoIDGenerator{})
	if _, err = targetService.Write(target.ctx, contextInvocation(target.principal), goldenpath.TicketDiscussionUnlink, goldenpath.TicketExecutionInput{TicketID: targetTicket, LinkID: targetLink, ClientOperationID: "remove-old-imported", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	second, err := target.service.Export(target.ctx, target.principal, target.owner.Token, applied.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	final := contextDatabase(t)
	plan2 := final.plan(t, second)
	if _, err = final.service.Apply(final.ctx, final.principal, final.owner.Token, second, plan2, plan2.PlanDigest); err != nil {
		t.Fatal(err)
	}
	finalTicket := contextMapping(t, plan2, "ticket", targetTicket)
	finalView, err := final.core.GetNexusView(final.ctx, final.principal, entityref.Ref{Type: "ticket", ID: finalTicket})
	if err != nil || finalView.TicketMessageSource == nil || finalView.TicketMessageSource.MessageID != contextMapping(t, plan2, "message", targetMessage) {
		t.Fatal(finalView, err)
	}
	page, err := final.store.ListTicketExecution(final.ctx, final.principal, entityref.Ref{Type: "ticket", ID: finalTicket}, "", goldenpath.DiscoveryPageInput{Limit: 25})
	if err != nil || len(page.Rows) != 0 {
		t.Fatal(page, err)
	}
}

func TestTicketExecutionCreationRollsBackBeforeRetry(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	ch := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: f.project, Name: "Public", Visibility: "project", MemberUserIDs: []string{}}).Object.ID
	msg, err := f.core.CreateMessage(f.ctx, contextInvocation(f.member), goldenpath.CreateMessageInput{ChannelID: ch, ClientOperationID: "rollback-source", Body: "Synthetic rollback body"})
	if err != nil {
		t.Fatal(err)
	}
	service := goldenpath.NewTicketExecutionService(f.store, goldenpath.CryptoIDGenerator{})
	input := goldenpath.TicketExecutionInput{ChannelID: ch, MessageID: msg.Message.ID, ClientOperationID: "retry-rollback", Title: "Transactional execution", Confirmed: true}
	inv := contextInvocation(f.member)
	before := contextCount(t, f, "tickets")
	contextSQL(t, f, `CREATE FUNCTION radishnexus.fail_execution() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic failure'; END $$;
	CREATE TRIGGER fail_execution BEFORE INSERT ON radishnexus.activity_items FOR EACH ROW WHEN (NEW.activity_type='ticket.created') EXECUTE FUNCTION radishnexus.fail_execution()`)
	if _, err = service.Write(f.ctx, inv, goldenpath.TicketFromMessage, input); err == nil {
		t.Fatal("partial success reported")
	}
	if count := contextCount(t, f, "tickets"); count != before {
		t.Fatal(count, before)
	}
	contextSQL(t, f, `DROP TRIGGER fail_execution ON radishnexus.activity_items; DROP FUNCTION radishnexus.fail_execution()`)
	out, err := service.Write(f.ctx, inv, goldenpath.TicketFromMessage, input)
	if err != nil || !out.Created {
		t.Fatal(out, err)
	}
	contextSQL(t, f, `UPDATE radishnexus.project_memberships SET role='viewer' WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3`, f.workspace, f.project, f.member.ID)
	if _, err = service.Write(f.ctx, inv, goldenpath.TicketFromMessage, input); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("receipt bypassed current permissions", err)
	}
}
