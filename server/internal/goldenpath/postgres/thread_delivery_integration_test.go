//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const threadDeliveryEmpty = "{\"data\":{\"items\":[],\"next_cursor\":null}}"

type threadDeliveryHTTPItem struct {
	Ref     entityref.Ref `json:"ref"`
	Origins []string      `json:"origins"`
}

type threadDeliveryHTTPPage struct {
	Data struct {
		Items []threadDeliveryHTTPItem `json:"items"`
		Next  string                   `json:"next_cursor"`
	} `json:"data"`
}

func deliveryPage(t *testing.T, raw []byte, keys ...string) threadDeliveryHTTPPage {
	t.Helper()
	var exact struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &exact); err != nil || len(exact.Data) != 2 || exact.Data["next_cursor"] == nil {
		t.Fatal(string(raw), err)
	}
	if err := json.Unmarshal(exact.Data["items"], &items); err != nil {
		t.Fatal(string(raw), err)
	}
	for _, item := range items {
		if len(item) != len(keys) {
			t.Fatal("unexpected delivery fields", item)
		}
		for _, key := range keys {
			if item[key] == nil {
				t.Fatal("missing delivery field", key)
			}
		}
	}
	var page threadDeliveryHTTPPage
	if err := json.Unmarshal(raw, &page); err != nil || page.Data.Items == nil {
		t.Fatal(string(raw), err)
	}
	return page
}

func deliveryMessage(t *testing.T, f *contextFixture, channel, thread, op string) string {
	t.Helper()
	msg, err := f.core.CreateMessage(f.ctx, contextInvocation(f.principal),
		goldenpath.CreateMessageInput{ChannelID: channel, ThreadID: thread, Body: "Synthetic delivery message", ClientOperationID: op})
	if err != nil {
		t.Fatal(err)
	}
	return msg.Message.ID
}

func deliveryDiscussion(t *testing.T, f *contextFixture, channel, op string) (thread, message string) {
	t.Helper()
	message = deliveryMessage(t, f, channel, "", op)
	result, err := f.core.StartThreadFromMessage(f.ctx, contextInvocation(f.principal),
		goldenpath.StartThreadFromMessageInput{ChannelID: channel, MessageID: message, Title: "Synthetic delivery Thread " + op, Visibility: "project"})
	if err != nil {
		t.Fatal(err)
	}
	return result.ID, message
}

func deliveryDecision(t *testing.T, f *contextFixture, thread, op string) string {
	t.Helper()
	proposed, err := f.core.CreateDecisionFromThread(f.ctx, contextInvocation(f.principal),
		goldenpath.CreateDecisionInput{ThreadID: thread, ClientOperationID: op, Question: "Which delivery path?"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.core.AcceptDecision(f.ctx, contextInvocation(f.principal), goldenpath.AcceptDecisionInput{
		DecisionID: proposed.Decision.ID, ClientOperationID: op, Outcome: "Use the original discussion", Rationale: "Synthetic evidence",
	}); err != nil {
		t.Fatal(err)
	}
	return proposed.Decision.ID
}

func deliveryDecisionTicket(t *testing.T, f *contextFixture, decision, op string) string {
	t.Helper()
	result, err := f.core.CreateTicketFromDecision(f.ctx, contextInvocation(f.principal),
		goldenpath.CreateTicketInput{DecisionID: decision, ClientOperationID: op, Title: "Synthetic decision Ticket " + op})
	if err != nil {
		t.Fatal(err)
	}
	return result.Ticket.ID
}

func deliveryExecution(t *testing.T, f *contextFixture, kind string, input goldenpath.TicketExecutionInput) goldenpath.TicketExecutionResult {
	t.Helper()
	input.Confirmed = true
	result, err := goldenpath.NewTicketExecutionService(f.store, goldenpath.CryptoIDGenerator{}).Write(f.ctx, contextInvocation(f.principal), kind, input)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func deliveryMessageTicket(t *testing.T, f *contextFixture, channel, message, op string) string {
	t.Helper()
	return deliveryExecution(t, f, goldenpath.TicketFromMessage, goldenpath.TicketExecutionInput{
		ChannelID: channel, MessageID: message, Title: "Synthetic message Ticket " + op, ClientOperationID: op,
	}).Ticket.ID
}

func TestThreadDeliveryHTTPSAndV9(t *testing.T) {
	f := contextDatabase(t)
	targets := []*contextFixture{contextDatabase(t), contextDatabase(t)}
	f.seed(t)
	owner := f.principal
	channel := f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: f.project, Name: "Synthetic delivery discussion", Visibility: "project", MemberUserIDs: []string{}}).Object.ID
	thread, top := deliveryDiscussion(t, f, channel, "primary")
	neighbor, neighborTop := deliveryDiscussion(t, f, channel, "neighbor")
	decision := deliveryDecision(t, f, thread, "original-delivery-decision")
	decisionTicket := deliveryDecisionTicket(t, f, decision, "decision-delivery-ticket")
	replacement := deliveryDecision(t, f, neighbor, "replacement-delivery-decision")
	if _, err := f.core.SupersedeDecision(f.ctx, contextInvocation(owner), supersessionInput(decision, replacement, "delivery-supersession")); err != nil {
		t.Fatal(err)
	}
	reply := deliveryMessage(t, f, channel, thread, "delivery-reply")
	replyTicket := deliveryMessageTicket(t, f, channel, reply, "reply-delivery-ticket")
	topTicket := deliveryMessageTicket(t, f, channel, top, "top-delivery-ticket")
	neighborTicket := deliveryMessageTicket(t, f, channel, neighborTop, "neighbor-delivery-ticket")
	attach := func(ticket, discussion, op string) string {
		t.Helper()
		return deliveryExecution(t, f, goldenpath.TicketDiscussionLink, goldenpath.TicketExecutionInput{TicketID: ticket, ThreadID: discussion, ClientOperationID: op}).LinkID
	}
	detach := func(ticket, link, op string) {
		t.Helper()
		deliveryExecution(t, f, goldenpath.TicketDiscussionUnlink, goldenpath.TicketExecutionInput{TicketID: ticket, LinkID: link, ClientOperationID: op})
	}
	replyLink := attach(replyTicket, thread, "reply-discussion")
	discussionLink := attach(neighborTicket, thread, "neighbor-discussion")
	attach(f.ticket, f.thread, "private-discussion")

	team := f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: f.workspace, Name: "Synthetic trace team"}).Object.ID
	otherProject := f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: f.workspace, Name: "Synthetic unrelated project",
		Key: "delivery-other", OwnerTeamID: team, InitialAdminUserID: owner.ID, Visibility: "workspace"}).Object.ID
	otherChannel := f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: otherProject, Name: "Synthetic unrelated channel", Visibility: "project", MemberUserIDs: []string{}}).Object.ID
	otherTicket := deliveryMessageTicket(t, f, otherChannel, deliveryMessage(t, f, otherChannel, "", "other-project"), "other-project-ticket")
	component := f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "component.create", ScopeID: f.workspace, OwnerTeamID: team, Name: "Synthetic trace service", Key: "delivery-service", Delivery: &goldenpath.DeliveryConfigurationInput{Type: "service"}}).Object.ID
	environment := f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "environment.create", ScopeID: f.workspace, OwnerTeamID: team, Name: "Synthetic trace staging", Key: "delivery-stage", Delivery: &goldenpath.DeliveryConfigurationInput{Classification: "staging"}}).Object.ID
	f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "environment.authorization.grant", ScopeID: environment, UserID: owner.ID, Delivery: &goldenpath.DeliveryConfigurationInput{Confirmed: true}})
	secondEnvironment := f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "environment.create", ScopeID: f.workspace, OwnerTeamID: team, Name: "Synthetic alternate staging", Key: "delivery-stage-b", Delivery: &goldenpath.DeliveryConfigurationInput{Classification: "staging"}}).Object.ID
	f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "environment.authorization.grant", ScopeID: secondEnvironment, UserID: owner.ID, Delivery: &goldenpath.DeliveryConfigurationInput{Confirmed: true}})
	now := time.Now().UTC().Truncate(time.Second)
	makeRun := func(status string) string {
		t.Helper()
		run, err := f.core.RecordCompletedJenkinsRun(f.ctx, goldenpath.VerifiedJenkinsDelivery{WorkspaceID: f.workspace, SourceID: "thread-delivery-test", DeliveryID: status, PayloadSHA256: strings.Repeat("0", 64)},
			goldenpath.RecordCompletedCIRunInput{ComponentID: component, ExternalRunKey: status, Status: status, CompletedAt: now.Add(-time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		return run.CIRun.ID
	}
	run, failed, unlinked := makeRun("succeeded"), makeRun("failed"), makeRun("canceled")
	trace := goldenpath.NewTicketCIRunService(f.store, goldenpath.CryptoIDGenerator{})
	linkBuild := func(ticket, run, op string) string {
		t.Helper()
		result, err := trace.Write(f.ctx, contextInvocation(owner), goldenpath.TicketCIRunLink, goldenpath.TicketCIRunInput{TicketID: ticket, RunID: run, ClientOperationID: op, Confirmed: true})
		if err != nil {
			t.Fatal(err)
		}
		return result.LinkID
	}
	runLink := linkBuild(decisionTicket, run, "delivery-build-succeeded")
	linkBuild(decisionTicket, failed, "delivery-build-failed")
	linkBuild(neighborTicket, unlinked, "delivery-build-discussion-only")
	linkBuild(otherTicket, run, "other-project-build")
	staged, err := f.core.RecordStagingDeployment(f.ctx, contextInvocation(owner), goldenpath.RecordStagingDeploymentInput{
		EnvironmentID: environment, CIRunID: run, Status: "failed", CompletedAt: now, ClientOperationID: "delivery-stage", Confirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondDeployment, err := f.core.RecordStagingDeployment(f.ctx, contextInvocation(owner), goldenpath.RecordStagingDeploymentInput{
		EnvironmentID: secondEnvironment, CIRunID: run, Status: "succeeded", CompletedAt: now, ClientOperationID: "delivery-stage-b", Confirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	memberSession, err := f.auth.Login(f.ctx, authn.LoginInput{Email: "context.member@example.test", Password: contextTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := contextHTTPS(t, f, "")
	ws := "/api/v1/workspaces/" + f.workspace
	root := ws + "/threads/" + thread + "/delivery-tickets"
	runs := root + "/" + decisionTicket + "/ci-runs"
	deployments := runs + "/" + run + "/deployments"
	read := func(path string, status int) []byte {
		t.Helper()
		return contextHTTPRead(t, srv, f.owner, path, status)
	}
	readTickets := func(path string) threadDeliveryHTTPPage {
		t.Helper()
		return deliveryPage(t, read(path, 200), "ref", "title", "status", "origins")
	}
	checkEmpty := func(b []byte) {
		t.Helper()
		if strings.TrimSpace(string(b)) != threadDeliveryEmpty {
			t.Fatal(string(b))
		}
	}
	expected := map[string][]string{
		decisionTicket: {"decision-origin"}, replyTicket: {"message-origin", "execution-discussion"}, neighborTicket: {"execution-discussion"},
	}
	ids := []string{decisionTicket, replyTicket, neighborTicket}
	slices.Sort(ids)
	next := ""
	for index, id := range ids {
		path := root + "?limit=1"
		if next != "" {
			path += "&after=" + url.QueryEscape(next)
		}
		page := readTickets(path)
		if len(page.Data.Items) != 1 || page.Data.Items[0].Ref.ID != id || !slices.Equal(page.Data.Items[0].Origins, expected[id]) || (page.Data.Next != "") != (index < len(ids)-1) {
			t.Fatal(page, id)
		}
		next = page.Data.Next
		if index == 0 {
			contextHTTPRead(t, srv, memberSession, root+"?after="+url.QueryEscape(next), 400)
			read(ws+"/threads/"+neighbor+"/delivery-tickets?after="+url.QueryEscape(next), 400)
			read(runs+"?after="+url.QueryEscape(next), 400)
		}
	}
	read(root+"?limit=51", 400)
	read(root+"?status=done", 400)
	executionWriteHTTP(t, srv, f.owner, "POST", root, "{}", 405)
	contextHTTPRead(t, srv, authn.Session{}, root, 401)
	checkEmpty(read(ws+"/threads/"+neighbor+"/delivery-tickets", 200))
	read(root+"/"+topTicket+"/ci-runs", 404)
	read(ws+"/tickets/"+otherTicket+"/ci-runs", 200)
	read(root+"/"+otherTicket+"/ci-runs", 404)
	read(root+"/"+otherTicket+"/ci-runs/"+run+"/deployments", 404)
	read(root+"/"+f.ticket+"/ci-runs", 404)
	read(runs+"/"+unlinked+"/deployments", 404)
	contextHTTPRead(t, srv, memberSession, ws+"/tickets/"+f.ticket+"/nexus-view", 200)
	contextHTTPRead(t, srv, memberSession, ws+"/threads/"+f.thread+"/delivery-tickets", 404)
	contextSQL(t, f, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,management_role) VALUES($1,$2,$3,'member')`, f.workspace, f.thread, f.member.ID)
	contextHTTPRead(t, srv, memberSession, ws+"/threads/"+f.thread+"/delivery-tickets", 200)
	contextSQL(t, f, `DELETE FROM radishnexus.thread_memberships WHERE workspace_id=$1 AND thread_id=$2 AND user_id=$3`, f.workspace, f.thread, f.member.ID)
	contextHTTPRead(t, srv, memberSession, ws+"/threads/"+f.thread+"/delivery-tickets/"+f.ticket+"/ci-runs", 404)

	runPage := deliveryPage(t, read(runs+"?limit=1", 200), "ref", "component", "status", "completed_at")
	if len(runPage.Data.Items) != 1 || runPage.Data.Next == "" {
		t.Fatal(runPage)
	}
	afterRun := "?after=" + url.QueryEscape(runPage.Data.Next)
	page2 := deliveryPage(t, read(runs+afterRun, 200), "ref", "component", "status", "completed_at")
	if len(page2.Data.Items) != 1 || page2.Data.Items[0].Ref.ID == runPage.Data.Items[0].Ref.ID || page2.Data.Next != "" {
		t.Fatal(page2)
	}
	read(root+"/"+replyTicket+"/ci-runs"+afterRun, 400)
	deployed := []string{staged.ID, secondDeployment.ID}
	slices.Sort(deployed)
	deploy := deliveryPage(t, read(deployments+"?limit=1", 200), "ref", "environment", "status", "completed_at")
	if len(deploy.Data.Items) != 1 || deploy.Data.Items[0].Ref.ID != deployed[0] || deploy.Data.Next == "" {
		t.Fatal(deploy)
	}
	afterDeployment := "?after=" + url.QueryEscape(deploy.Data.Next)
	deploy = deliveryPage(t, read(deployments+afterDeployment, 200), "ref", "environment", "status", "completed_at")
	if len(deploy.Data.Items) != 1 || deploy.Data.Items[0].Ref.ID != deployed[1] || deploy.Data.Next != "" {
		t.Fatal(deploy)
	}
	read(runs+"/"+failed+"/deployments"+afterDeployment, 400)
	read(root+"/"+replyTicket+"/ci-runs/"+run+"/deployments"+afterDeployment, 400)
	checkEmpty(read(runs+"/"+failed+"/deployments", 200))
	contextHTTPRead(t, srv, memberSession, deployments, 200)
	beforeLegacy := read(ws+"/threads/"+thread+"/nexus-view", 200)
	beforeExplicit := read(ws+"/threads/"+thread+"/tickets", 200)
	if bytes.Contains(beforeLegacy, []byte(run)) || bytes.Contains(beforeLegacy, []byte(replyTicket)) ||
		!bytes.Contains(beforeExplicit, []byte(replyTicket)) || bytes.Contains(beforeExplicit, []byte(decisionTicket)) {
		t.Fatal("existing Thread fields changed")
	}

	// A retained discussion and both creation paths are recovered twice.
	bundle := f.bundle(t)
	sourceThread, sourceDecision, sourceReply, sourceDiscussion := thread, decisionTicket, replyTicket, neighborTicket
	for _, target := range targets {
		plan := target.plan(t, bundle)
		result, err := target.service.Apply(target.ctx, target.principal, target.owner.Token, bundle, plan, plan.PlanDigest)
		if err != nil || result.Status != "applied" || bundle.Manifest.Profile != cp.Profile {
			t.Fatal("v9 apply", result, err)
		}
		target.project = result.ProjectID
		targetServer, _ := contextHTTPS(t, target, "")
		targetRoot := "/api/v1/workspaces/" + target.workspace + "/threads/"
		contextHTTPRead(t, targetServer, target.owner, targetRoot+sourceThread+"/delivery-tickets", 404)
		sourceThread = contextMapping(t, plan, "thread", sourceThread)
		sourceDecision = contextMapping(t, plan, "ticket", sourceDecision)
		sourceReply = contextMapping(t, plan, "ticket", sourceReply)
		sourceDiscussion = contextMapping(t, plan, "ticket", sourceDiscussion)
		targetRoot += sourceThread + "/delivery-tickets"
		recovered := deliveryPage(t, contextHTTPRead(t, targetServer, target.owner, targetRoot, 200), "ref", "title", "status", "origins")
		actual := map[string][]string{}
		for _, item := range recovered.Data.Items {
			actual[item.Ref.ID] = item.Origins
		}
		if !reflect.DeepEqual(actual, map[string][]string{
			sourceDecision: {"decision-origin"}, sourceReply: {"message-origin", "execution-discussion"}, sourceDiscussion: {"execution-discussion"},
		}) {
			t.Fatal("recovered reachability", actual)
		}
		checkEmpty(contextHTTPRead(t, targetServer, target.owner, targetRoot+"/"+sourceDecision+"/ci-runs", 200))
		contextHTTPRead(t, targetServer, target.owner, targetRoot+"/"+sourceDecision+"/ci-runs/"+run+"/deployments", 404)
		bundle = target.bundle(t)
	}

	detach(replyTicket, replyLink, "remove-secondary-origin")
	detach(neighborTicket, discussionLink, "remove-only-origin")
	read(root+"/"+neighborTicket+"/ci-runs", 404)
	read(root+"/"+neighborTicket+"/ci-runs/"+unlinked+"/deployments", 404)
	checkEmpty(read(root+"/"+replyTicket+"/ci-runs", 200))
	if page := readTickets(root); len(page.Data.Items) != 2 ||
		!slices.ContainsFunc(page.Data.Items, func(item threadDeliveryHTTPItem) bool {
			return item.Ref.ID == replyTicket && slices.Equal(item.Origins, []string{"message-origin"})
		}) {
		t.Fatal("secondary origin removal", page)
	}
	if _, err = trace.Write(f.ctx, contextInvocation(owner), goldenpath.TicketCIRunUnlink, goldenpath.TicketCIRunInput{
		TicketID: decisionTicket, LinkID: runLink, ClientOperationID: "remove-delivery-run", Confirmed: true,
	}); err != nil {
		t.Fatal(err)
	}
	read(deployments, 404)
	read(ws+"/deployments/"+staged.ID+"/nexus-view", 200)
	contextSQL(t, f, `INSERT INTO radishnexus.threads(id,workspace_id,governing_project_id,title,visibility,created_by)
		VALUES ('thr_legacy_delivery',$1,$2,'Legacy discussion','project',$3)`, f.workspace, f.project, owner.ID)
	legacy := deliveryDecisionTicket(t, f, deliveryDecision(t, f, "thr_legacy_delivery", "legacy-origin"), "legacy-ticket")
	legacyPage := readTickets(ws + "/threads/thr_legacy_delivery/delivery-tickets")
	if len(legacyPage.Data.Items) != 1 || legacyPage.Data.Items[0].Ref.ID != legacy {
		t.Fatal("legacy Decision origin", legacyPage)
	}
	contextSQL(t, f, `UPDATE radishnexus.projects SET status='archived' WHERE workspace_id=$1 AND id=$2`, f.workspace, f.project)
	read(root, 200)
	read(runs, 200)
}

type deliveryPlanTracer struct {
	relationQueryCounter
	sql  string
	args []any
}

func (tracer *deliveryPlanTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, query pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(query.SQL, "WITH delivery_paths AS") {
		tracer.sql, tracer.args = query.SQL, slices.Clone(query.Args)
	}
	return tracer.relationQueryCounter.TraceQueryStart(ctx, conn, query)
}

func TestThreadDeliveryQueryPlan(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	channel := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: f.project, Name: "Delivery scale", Visibility: "project", MemberUserIDs: []string{}}).Object.ID
	thread, _ := deliveryDiscussion(t, f, channel, "scale-main")
	neighbor, _ := deliveryDiscussion(t, f, channel, "scale-neighbor")
	for i := range 180 {
		op := fmt.Sprintf("delivery-scale-%03d", i)
		if i < 30 {
			deliveryDecisionTicket(t, f, deliveryDecision(t, f, thread, op), op)
			continue
		}
		source := thread
		if i >= 120 {
			source = neighbor
		}
		ticket := deliveryMessageTicket(t, f, channel, deliveryMessage(t, f, channel, source, op), op)
		if i%3 == 0 {
			deliveryExecution(t, f, goldenpath.TicketDiscussionLink, goldenpath.TicketExecutionInput{TicketID: ticket, ThreadID: source, ClientOperationID: op})
		}
	}
	contextSQL(t, f, `ANALYZE radishnexus.entity_links, radishnexus.threads, radishnexus.messages, radishnexus.decisions, radishnexus.tickets`)
	tracer := &deliveryPlanTracer{}
	config := f.pool.Config()
	config.ConnConfig.Tracer = tracer
	measured, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	reader := goldenpath.NewThreadDeliveryService(goldenpostgres.New(measured))
	scope := goldenpath.ThreadDeliveryScope{ThreadID: thread}
	count := func(limit int, after string) goldenpath.ThreadDeliveryPage {
		t.Helper()
		page, err := reader.List(f.ctx, f.principal, scope, goldenpath.DiscoveryPageInput{Limit: limit, AfterID: after})
		if err != nil || len(page.Items) != limit || page.NextID == "" {
			t.Fatal("delivery scale page", len(page.Items), page.NextID, err)
		}
		return page
	}
	count(1, "")
	first := tracer.queries.Swap(0)
	firstPage := count(50, "")
	wide := tracer.queries.Swap(0)
	count(50, firstPage.NextID)
	continuation := tracer.queries.Swap(0)
	if first != wide || wide != continuation || wide > 16 || tracer.sql == "" {
		t.Fatalf("delivery SQL count varies with page width or cursor: %d/%d/%d", first, wide, continuation)
	}
	var raw []byte
	if err = f.pool.QueryRow(f.ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+tracer.sql, tracer.args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan []struct {
		Execution float64 `json:"Execution Time"`
		Plan      struct {
			Node       string `json:"Node Type"`
			Rows       int    `json:"Actual Rows"`
			SharedHit  int    `json:"Shared Hit Blocks"`
			SharedRead int    `json:"Shared Read Blocks"`
		} `json:"Plan"`
	}
	if err = json.Unmarshal(raw, &plan); err != nil || len(plan) != 1 || plan[0].Plan.Node != "Limit" || plan[0].Plan.Rows != 51 {
		t.Fatalf("unexpected delivery query plan %s: %v", raw, err)
	}
	t.Logf("Thread delivery 120 matching, 30 duplicate paths, 60 sibling Tickets: SQL %d/%d/%d; Limit %d, %.3f ms, shared hit/read %d/%d",
		first, wide, continuation, plan[0].Plan.Rows, plan[0].Execution, plan[0].Plan.SharedHit, plan[0].Plan.SharedRead)
}
