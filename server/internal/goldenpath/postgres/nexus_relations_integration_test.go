//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/markdown"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type nexusRelationTracer struct {
	relationQueryCounter
	sql  string
	args []any
}

func (tracer *nexusRelationTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, query pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(query.SQL, "SELECT id,created_at,target_type,target_id,title FROM (") && strings.HasSuffix(query.SQL, "LIMIT $4") {
		tracer.sql, tracer.args = query.SQL, slices.Clone(query.Args)
	}
	return tracer.relationQueryCounter.TraceQueryStart(ctx, conn, query)
}

type nexusExplainPlan struct {
	Node       string             `json:"Node Type"`
	Table      string             `json:"Relation Name"`
	Index      string             `json:"Index Name"`
	Rows       int                `json:"Actual Rows"`
	Removed    int                `json:"Rows Removed by Filter"`
	SharedHit  int                `json:"Shared Hit Blocks"`
	SharedRead int                `json:"Shared Read Blocks"`
	Children   []nexusExplainPlan `json:"Plans"`
}

func (plan nexusExplainPlan) links() string {
	var descriptions []string
	if plan.Table == "entity_links" || strings.Contains(plan.Index, "entity_links") {
		descriptions = append(descriptions, fmt.Sprintf("%s %s rows=%d removed=%d blocks=%d/%d", plan.Node, plan.Index, plan.Rows, plan.Removed, plan.SharedHit, plan.SharedRead))
	}
	for _, child := range plan.Children {
		if description := child.links(); description != "" {
			descriptions = append(descriptions, description)
		}
	}
	return strings.Join(descriptions, "; ")
}

func TestNexusRelationPageQueryPlan(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	channel := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: f.project, Name: "Relation paging scale", Visibility: "project", MemberUserIDs: []string{}}).Object.ID
	thread, _ := deliveryDiscussion(t, f, channel, "relation-page-main")
	neighbor, _ := deliveryDiscussion(t, f, channel, "relation-page-neighbor")
	invoke := contextInvocation(f.principal)
	writer := goldenpath.NewService(f.store, goldenpath.CryptoIDGenerator{}, fixedClock{now: time.Now().UTC()})
	propose := func(thread string, n int) {
		t.Helper()
		op := fmt.Sprintf("nexus-relation-plan-%04d", n)
		if _, err := writer.CreateDecisionFromThread(f.ctx, invoke, goldenpath.CreateDecisionInput{ThreadID: thread, ClientOperationID: op, Question: "Synthetic scale " + op}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 120 {
		propose(thread, i)
	}
	tracer := &nexusRelationTracer{}
	cfg := f.pool.Config()
	cfg.ConnConfig.Tracer = tracer
	measured, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	store := goldenpostgres.New(measured)
	threadSource := entityref.Ref{Type: "thread", ID: thread}
	decisionSource := entityref.Ref{Type: "decision", ID: f.decision}
	otherDecision := deliveryDecision(t, f, neighbor, "relation-page-neighbor-decision")
	addTicket := func(decision string, n int) {
		t.Helper()
		op := fmt.Sprintf("nexus-relation-ticket-%04d", n)
		if _, err := writer.CreateTicketFromDecision(f.ctx, invoke, goldenpath.CreateTicketInput{DecisionID: decision, ClientOperationID: op, Title: "Synthetic scale " + op}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i < 120; i++ {
		addTicket(f.decision, i)
	}
	measure := func(source entityref.Ref, noise, limit int, after *goldenpath.NexusRelationBoundary) (int64, *goldenpath.NexusRelationBoundary) {
		t.Helper()
		tracer.queries.Store(0)
		page, err := store.ListNexusRelations(f.ctx, f.principal, source, goldenpath.NexusRelationPageInput{Limit: limit, After: after})
		count := tracer.queries.Swap(0)
		if err != nil || len(page.Items) != limit || page.Next == nil || tracer.sql == "" {
			t.Fatal("measure relation page", page.Next, len(page.Items), err)
		}
		var raw []byte
		if err := f.pool.QueryRow(f.ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+tracer.sql, tracer.args...).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var result []struct {
			Plan      nexusExplainPlan `json:"Plan"`
			Execution float64          `json:"Execution Time"`
		}
		if err := json.Unmarshal(raw, &result); err != nil || len(result) != 1 || result[0].Plan.Rows != limit+1 {
			t.Fatalf("unexpected query plan: %s (%v)", raw, err)
		}
		plan := result[0].Plan
		links := plan.links()
		t.Logf("%s, 120 matching, %d unrelated edges, limit %d, continuation %t: %d SQL calls; %.3f ms; shared hit/read %d/%d; EntityLink: %s",
			source.Type, noise, limit, after != nil, count, result[0].Execution, plan.SharedHit, plan.SharedRead, links)
		if noise > 0 && limit == 1 && !strings.Contains(links, "entity_links_nexus_incoming_page") {
			t.Fatalf("selective %s page did not use the migrated index: %s", source.Type, links)
		}
		return count, page.Next
	}
	sources := []entityref.Ref{threadSource, decisionSource}
	contextSQL(t, f, `ANALYZE radishnexus.entity_links, radishnexus.decisions, radishnexus.tickets, radishnexus.projects`)
	small := make([]int64, len(sources))
	for i, source := range sources {
		before, err := store.ListRelations(f.ctx, f.principal, source)
		if err != nil {
			t.Fatal(err)
		}
		first, err := store.ListNexusRelations(f.ctx, f.principal, source, goldenpath.NexusRelationPageInput{Limit: 1})
		if err != nil || len(first.Items) != 1 || len(before) != 121 || first.Items[0].Target != before[1].Target {
			t.Fatal("legacy ordering differs for equal link timestamps", source.Type, err)
		}
		second, err := store.ListNexusRelations(f.ctx, f.principal, source, goldenpath.NexusRelationPageInput{Limit: 1, After: first.Next})
		if err != nil || len(second.Items) != 1 || second.Items[0].Target != before[2].Target {
			t.Fatal("keyset tie skipped the next Link ID", source.Type, err)
		}
		small[i], _ = measure(source, 0, 1, nil)
	}
	for i := 120; i < 1320; i++ {
		propose(neighbor, i)
		addTicket(otherDecision, i)
	}
	contextSQL(t, f, `ANALYZE radishnexus.entity_links, radishnexus.decisions, radishnexus.tickets, radishnexus.projects`)
	for i, source := range sources {
		busy, _ := measure(source, 2400, 1, nil)
		wide, cursor := measure(source, 2400, 50, nil)
		continuation, _ := measure(source, 2400, 1, cursor)
		if small[i] != busy || busy != wide || wide > 20 || continuation != wide+1 {
			t.Fatalf("%s SQL round trips vary: %d/%d/%d (continued %d)", source.Type, small[i], busy, wide, continuation)
		}
	}
}

type nexusRelationsResponse struct {
	Data struct {
		Relations []json.RawMessage `json:"relations"`
		Items     []json.RawMessage `json:"items"`
		Cursor    *string           `json:"next_cursor"`
	} `json:"data"`
}

func parseNexusRelationsResponse(t *testing.T, raw []byte) nexusRelationsResponse {
	t.Helper()
	var response nexusRelationsResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestNexusRelationPageHTTPSAndPortable(t *testing.T) {
	f := contextDatabase(t)
	target := contextDatabase(t)
	f.seed(t)
	inv := contextInvocation(f.principal)
	if _, err := f.core.AcceptDecision(f.ctx, inv, goldenpath.AcceptDecisionInput{
		DecisionID: f.proposed, ClientOperationID: "nexus-relations-accept", Outcome: "Replacement outcome", Rationale: "Synthetic rationale",
	}); err != nil {
		t.Fatal(err)
	}
	deliveryDecisionTicket(t, f, f.decision, "nexus-second-ticket")
	if _, err := f.core.SupersedeDecision(f.ctx, inv, supersessionInput(f.decision, f.proposed, "nexus-relations-supersede")); err != nil {
		t.Fatal(err)
	}

	team := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: f.workspace, Name: "Synthetic related team"}).Object.ID
	component := f.configure(t, f.principal, goldenpath.ConfigurationInput{
		Kind: "component.create", ScopeID: f.workspace, Name: "Synthetic relation service", Key: "relation-service", OwnerTeamID: team, Delivery: &goldenpath.DeliveryConfigurationInput{Type: "service"},
	}).Object.ID
	componentWriter := goldenpath.NewTicketComponentService(f.store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	linked, err := componentWriter.WriteTicketComponent(f.ctx, inv, "ticket.component.link", goldenpath.TicketComponentInput{
		TicketID: f.ticket, ComponentID: component, ClientOperationID: "nexus-relations-component", Confirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	lateDocument, err := f.documents.WriteDocument(f.ctx, inv, "document.create", goldenpath.DocumentInput{
		TargetID: f.ticket, ClientOperationID: "nexus-relations-document", Title: "Second related Document", FormatVersion: markdown.Format, BodyMarkdown: "Synthetic",
	})
	if err != nil {
		t.Fatal(err)
	}

	srv, _ := contextHTTPS(t, f, "")
	base := "/api/v1/workspaces/" + f.workspace
	read := func(path string, status int) []byte {
		t.Helper()
		return contextHTTPRead(t, srv, f.owner, base+path, status)
	}
	readResponse := func(path string) nexusRelationsResponse {
		t.Helper()
		return parseNexusRelationsResponse(t, read(path, 200))
	}
	thread := "/threads/" + f.thread + "/nexus-view"
	decision := "/decisions/" + f.decision + "/nexus-view"
	ticket := "/tickets/" + f.ticket + "/nexus-view"
	for _, check := range []struct {
		path         string
		legacyQuery  string
		summaryQuery string
		count        int
	}{
		{thread, "", "?relations=summary", 2},
		{decision, "", "?relations=summary", 2},
		{ticket, "?include=due_date,execution_context,classification", "?include=due_date,execution_context,classification&relations=summary", 3},
	} {
		raw := read(check.path+check.legacyQuery, 200)
		full := parseNexusRelationsResponse(t, raw).Data.Relations
		summary := readResponse(check.path + check.summaryQuery).Data.Relations
		var matched, fixed []json.RawMessage
		for _, relation := range full {
			var fields struct {
				Direction string
				Relation  string `json:"relation_type"`
				Target    struct{ Ref entityref.Ref }
			}
			if err := json.Unmarshal(relation, &fields); err != nil {
				t.Fatal(err)
			}
			switch {
			case fields.Direction == "incoming" && fields.Relation != "supersedes", fields.Relation == "relates-to", fields.Relation == "affects":
				matched = append(matched, relation)
			default:
				fixed = append(fixed, relation)
			}
		}
		if len(matched) != check.count || !slices.EqualFunc(fixed, summary, func(a, b json.RawMessage) bool { return bytes.Equal(a, b) }) {
			t.Fatalf("fixed/readable partition for %s: %d linked, %s fixed, %s summary", check.path, len(matched), fixed, summary)
		}
		var cursor *string
		for i, want := range matched {
			path := check.path + "/relations?limit=1"
			if cursor != nil {
				path += "&after=" + url.QueryEscape(*cursor)
			}
			page := readResponse(path).Data
			if len(page.Items) != 1 || !bytes.Equal(page.Items[0], want) || (page.Cursor != nil) != (i < len(matched)-1) {
				t.Fatalf("page %d for %s: %s, want %s", i, check.path, page.Items, want)
			}
			cursor = page.Cursor
			if cursor != nil {
				rawCursor, err := base64.RawURLEncoding.DecodeString(*cursor)
				if err != nil || bytes.Contains(rawCursor, []byte("lnk_")) || bytes.Contains(rawCursor, []byte("created_at")) {
					t.Fatal("unsafe cursor fields", err)
				}
			}
		}
		if !bytes.Equal(raw, read(check.path+check.legacyQuery, 200)) {
			t.Fatal("legacy response changed while paging")
		}
	}
	for _, path := range []string{thread + "?relations=summary&relations=summary", thread + "?include=due_date", ticket + "?include=classification&relations=all",
		thread + "/relations?limit=51", thread + "/relations?after=", thread + "/relations?direction=incoming", thread + "/relations?after=bad"} {
		read(path, 400)
	}
	executionWriteHTTP(t, srv, f.owner, "POST", base+thread+"/relations", "{}", 405)
	contextHTTPRead(t, srv, authn.Session{}, base+thread+"/relations", 401)
	member, err := f.auth.Login(f.ctx, authn.LoginInput{Email: "context.member@example.test", Password: contextTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	contextHTTPRead(t, srv, member, base+thread+"/relations", 404)
	memberDecision := contextHTTPRead(t, srv, member, base+decision+"?relations=summary", 200)
	if !bytes.Contains(memberDecision, []byte(`{"visibility":"restricted"}`)) || bytes.Contains(memberDecision, []byte(f.thread)) {
		t.Fatal("restricted evidence output changed")
	}
	contextHTTPRead(t, srv, member, base+decision+"/relations", 200)
	cursor := readResponse(thread + "/relations?limit=1").Data.Cursor
	read(decision+"/relations?after="+url.QueryEscape(*cursor), 400)
	contextSQL(t, f, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,management_role) VALUES($1,$2,$3,'member')`, f.workspace, f.thread, f.member.ID)
	memberPage := parseNexusRelationsResponse(t, contextHTTPRead(t, srv, member, base+thread+"/relations?limit=1", 200)).Data
	contextHTTPRead(t, srv, member, base+thread+"/relations?after="+url.QueryEscape(*cursor), 400)
	contextSQL(t, f, `DELETE FROM radishnexus.thread_memberships WHERE workspace_id=$1 AND thread_id=$2 AND user_id=$3`, f.workspace, f.thread, f.member.ID)
	contextHTTPRead(t, srv, member, base+thread+"/relations?after="+url.QueryEscape(*memberPage.Cursor), 404)

	messageChannel := f.configure(t, f.principal, goldenpath.ConfigurationInput{
		Kind: "channel.create", ScopeID: f.project, Name: "Synthetic message source", Visibility: "project", MemberUserIDs: []string{},
	}).Object.ID
	message := deliveryMessage(t, f, messageChannel, "", "relation-message-source")
	messageTicket := deliveryMessageTicket(t, f, messageChannel, message, "relation-message-ticket")
	messagePath := "/tickets/" + messageTicket + "/nexus-view"
	read(messagePath+"?relations=summary", 409)
	rawMessage := read(messagePath+"?include=execution_context&relations=summary", 200)
	if !bytes.Contains(rawMessage, []byte(`"source_message"`)) || !bytes.Contains(rawMessage, []byte(message)) ||
		len(readResponse(messagePath+"/relations").Data.Items) != 0 {
		t.Fatal("message-origin summary lost its creation source")
	}

	componentPage := readResponse(ticket + "/relations?limit=2").Data
	if !bytes.Contains(componentPage.Items[1], []byte(component)) {
		t.Fatal("Component did not supply the boundary")
	}
	stale := ticket + "/relations?after=" + url.QueryEscape(*componentPage.Cursor)
	if _, err := componentWriter.WriteTicketComponent(f.ctx, inv, "ticket.component.unlink", goldenpath.TicketComponentInput{
		TicketID: f.ticket, LinkID: linked.LinkID, ClientOperationID: "nexus-relations-unlink", Confirmed: true,
	}); err != nil {
		t.Fatal(err)
	}
	removed := read(stale, 409)
	if _, err := componentWriter.WriteTicketComponent(f.ctx, inv, "ticket.component.link", goldenpath.TicketComponentInput{
		TicketID: f.ticket, ComponentID: component, ClientOperationID: "nexus-relations-relink", Confirmed: true,
	}); err != nil {
		t.Fatal(err)
	}
	replaced := read(stale, 409)
	for _, response := range [][]byte{removed, replaced} {
		if !bytes.Contains(response, []byte(`"code":"conflict"`)) || bytes.Contains(response, []byte(component)) {
			t.Fatal("relation conflict leaked target facts")
		}
	}
	if page := readResponse(ticket + "/relations").Data; len(page.Items) != 3 ||
		!bytes.Contains(page.Items[1], []byte(lateDocument.Ref.ID)) || !bytes.Contains(page.Items[2], []byte(component)) {
		t.Fatal("relinked relation did not move to its new position")
	}

	bundle := f.bundle(t)
	plan := target.plan(t, bundle)
	if _, err := target.service.Apply(target.ctx, target.principal, target.owner.Token, bundle, plan, plan.PlanDigest); err != nil {
		t.Fatal(err)
	}
	targetServer, _ := contextHTTPS(t, target, "")
	targetBase := "/api/v1/workspaces/" + target.workspace
	for _, check := range []struct {
		kind, id string
		count    int
	}{
		{"thread", f.thread, 2}, {"decision", f.decision, 2}, {"ticket", f.ticket, 2},
	} {
		path := targetBase + "/" + check.kind + "s/" + contextMapping(t, plan, check.kind, check.id) + "/nexus-view"
		contextHTTPRead(t, targetServer, target.owner, path+"?relations=summary", 200)
		page := parseNexusRelationsResponse(t, contextHTTPRead(t, targetServer, target.owner, path+"/relations", 200)).Data
		if len(page.Items) != check.count || page.Cursor != nil {
			t.Fatal("restored portable relations", check.kind, page.Items)
		}
	}
	// Legacy data can carry an additional evidence link to a Thread in
	// another Project. The visible source does not grant access to the Decision.
	privateProject := f.configure(t, f.principal, goldenpath.ConfigurationInput{
		Kind: "project.create", ScopeID: f.workspace, Name: "Synthetic hidden project", Key: "hidden-nexus",
		OwnerTeamID: team, Visibility: "restricted", InitialAdminUserID: f.principal.ID,
	}).Object.ID
	privateChannel := f.configure(t, f.principal, goldenpath.ConfigurationInput{
		Kind: "channel.create", ScopeID: privateProject, Name: "Synthetic hidden channel", Visibility: "project", MemberUserIDs: []string{},
	}).Object.ID
	privateThread, _ := deliveryDiscussion(t, f, privateChannel, "hidden-nexus-thread")
	hidden := deliveryDecision(t, f, privateThread, "hidden-nexus-decision")
	contextSQL(t, f, `INSERT INTO radishnexus.entity_links
		(id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id,created_at,updated_at)
		VALUES('lnk_hidden_relation_fixture',$1,'decision',$2,'derived-from','thread',$3,'asserted','user','user',$4,
		  '2000-01-01T00:00:00Z','2000-01-01T00:00:00Z')`, f.workspace, hidden, f.thread, f.principal.ID)
	contextSQL(t, f, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,management_role) VALUES($1,$2,$3,'member')`, f.workspace, f.thread, f.member.ID)
	ownerHidden := readResponse(thread + "/relations?limit=1").Data
	if len(ownerHidden.Items) != 1 || !bytes.Contains(ownerHidden.Items[0], []byte(hidden)) {
		t.Fatal("mixed visibility fixture")
	}
	memberPath := base + thread + "/relations"
	f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "project.member.set", ScopeID: privateProject, UserID: f.member.ID, Role: "viewer"})
	formerBoundary := parseNexusRelationsResponse(t, contextHTTPRead(t, srv, member, memberPath+"?limit=1", 200)).Data
	if formerBoundary.Cursor == nil || !bytes.Contains(formerBoundary.Items[0], []byte(hidden)) {
		t.Fatal("actor with both Project grants missed the cursor boundary")
	}
	viewer := "viewer"
	f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "project.member.remove", ScopeID: privateProject, UserID: f.member.ID, ExpectedRole: &viewer})
	contextHTTPRead(t, srv, member, memberPath+"?after="+url.QueryEscape(*formerBoundary.Cursor), 409)
	visible := parseNexusRelationsResponse(t, contextHTTPRead(t, srv, member, memberPath+"?limit=1", 200)).Data
	if len(visible.Items) != 1 || visible.Cursor == nil || bytes.Contains(visible.Items[0], []byte(hidden)) {
		t.Fatal("hidden relation occupied the first page")
	}
	visible = parseNexusRelationsResponse(t, contextHTTPRead(t, srv, member, memberPath+"?limit=1&after="+url.QueryEscape(*visible.Cursor), 200)).Data
	if len(visible.Items) != 1 || visible.Cursor != nil || bytes.Contains(visible.Items[0], []byte(hidden)) {
		t.Fatal("hidden relation affected the last page")
	}
	contextSQL(t, f, `UPDATE radishnexus.projects SET status='archived' WHERE workspace_id=$1 AND id=$2`, f.workspace, f.project)
	read(thread+"/relations", 200)
	read(decision+"/relations", 200)
	read(ticket+"/relations", 200)
}
