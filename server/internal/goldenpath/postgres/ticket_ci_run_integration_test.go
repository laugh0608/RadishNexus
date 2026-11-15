//go:build integration

package postgres_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func TestTicketCIRunHTTPSAndPortableScope(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	owner := f.principal
	team := f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: f.workspace, Name: "Synthetic delivery team"}).Object.ID
	component := f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "component.create", ScopeID: f.workspace, OwnerTeamID: team, Name: "Traceability service", Key: "trace-service", Delivery: &goldenpath.DeliveryConfigurationInput{Type: "service"}}).Object.ID
	environment := f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "environment.create", ScopeID: f.workspace, OwnerTeamID: team, Name: "Traceability staging", Key: "trace-stage", Delivery: &goldenpath.DeliveryConfigurationInput{Classification: "staging"}}).Object.ID
	f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "environment.authorization.grant", ScopeID: environment, UserID: owner.ID, Delivery: &goldenpath.DeliveryConfigurationInput{Confirmed: true}})
	makeRun := func(state, key string) string {
		t.Helper()
		v, err := f.core.RecordCompletedJenkinsRun(f.ctx, goldenpath.VerifiedJenkinsDelivery{WorkspaceID: f.workspace, SourceID: "trace-tests", DeliveryID: "trace-" + key, PayloadSHA256: strings.Repeat("0", 64)},
			goldenpath.RecordCompletedCIRunInput{ComponentID: component, ExternalRunKey: "trace-" + key, Status: state, CompletedAt: time.Now().Add(-time.Minute).UTC()})
		if err != nil {
			t.Fatal(err)
		}
		return v.CIRun.ID
	}
	succeeded, failed, canceled := makeRun("succeeded", "succeeded"), makeRun("failed", "failed"), makeRun("canceled", "canceled")
	s, _ := contextHTTPS(t, f, "")
	ws := "/api/v1/workspaces/" + f.workspace
	ticket := ws + "/tickets/" + f.ticket
	runs := ticket + "/ci-runs"
	run := ws + "/ci-runs/" + succeeded
	write := func(method, path, body string, want int) []byte {
		t.Helper()
		return executionWriteHTTP(t, s, f.owner, method, path, body, want)
	}
	read := func(path string, want int) []byte {
		t.Helper()
		return contextHTTPRead(t, s, f.owner, path, want)
	}
	check := func(raw []byte, needles ...string) {
		t.Helper()
		for _, needle := range needles {
			if !bytes.Contains(raw, []byte(needle)) {
				t.Fatalf("missing %s from %s", needle, raw)
			}
		}
	}
	linkBody := func(op, id string) string {
		return fmt.Sprintf(`{"client_operation_id":%q,"ci_run_id":%q,"confirmed":true}`, op, id)
	}
	linkID := func(raw []byte) string {
		t.Helper()
		var v struct {
			Data struct {
				ID string `json:"link_id"`
			}
		}
		if err := json.Unmarshal(raw, &v); err != nil || !cp.ValidID(v.Data.ID, "lnk_") {
			t.Fatal(string(raw), err)
		}
		return v.Data.ID
	}
	var ticketBefore time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT updated_at FROM radishnexus.tickets WHERE workspace_id=$1 AND id=$2`, f.workspace, f.ticket).Scan(&ticketBefore); err != nil {
		t.Fatal(err)
	}
	check(read(ws+"/components/"+component+"/ci-runs", 200), succeeded, failed, canceled)
	write("POST", runs, linkBody("wrong-scope", "cir_nonexistent"), 404)
	write("POST", runs, `{"client_operation_id":"bad","ci_run_id":"cir_example","confirmed":false}`, 400)
	proposal := linkBody("link-initial", succeeded)
	first := linkID(write("POST", runs, proposal, 201))
	if linkID(write("POST", runs, proposal, 200)) != first {
		t.Fatal("creation replay drift")
	}
	write("POST", runs, linkBody("link-initial", failed), 409)
	write("POST", runs, linkBody("duplicate-link", succeeded), 409)
	linkID(write("POST", runs, linkBody("link-failure", failed), 201))
	linkID(write("POST", runs, linkBody("link-cancel", canceled), 201))
	check(read(runs, 200), first, succeeded, failed, canceled, `"can_unlink":true`)
	check(read(run+"/tickets", 200), f.ticket)
	if bytes.Contains(read(ticket+"/nexus-view?include=due_date,execution_context,classification", 200), []byte(succeeded)) ||
		bytes.Contains(read(run+"/nexus-view", 200), []byte(f.ticket)) {
		t.Fatal("legacy Nexus View exposed new relationship")
	}

	deployBody := fmt.Sprintf(`{"client_operation_id":"trace-stage-record","environment_id":%q,"status":"failed","started_at":null,"completed_at":%q,"confirmed":true}`, environment, time.Now().UTC().Format(time.RFC3339))
	rawDeploy := write("POST", run+"/staging-deployments", deployBody, 201)
	var deploy struct {
		Data struct{ Deployment entityref.Ref }
	}
	if err := json.Unmarshal(rawDeploy, &deploy); err != nil || deploy.Data.Deployment.Type != "deployment" {
		t.Fatal(string(rawDeploy), err)
	}
	deployment := deploy.Data.Deployment.ID
	check(read(run+"/deployments", 200), deployment, environment, `"status":"failed"`)
	check(read(ws+"/deployments/"+deployment+"/nexus-view", 200), succeeded)
	check(read(ws+"/ci-runs/"+failed+"/deployments", 200), `"items":[]`)

	const remove = `{"client_operation_id":"remove-original","confirmed":true}`
	unlink := ticket + "/ci-run-links/" + first
	if linkID(write("DELETE", unlink, remove, 200)) != first {
		t.Fatal("wrong removal")
	}
	second := linkID(write("POST", runs, linkBody("relink", succeeded), 201))
	if second == first || linkID(write("DELETE", unlink, remove, 200)) != first || linkID(write("POST", runs, proposal, 200)) != first {
		t.Fatal("relation generation or replay drift")
	}
	write("DELETE", unlink, `{"client_operation_id":"remove-again","confirmed":true}`, 409)
	body := read(runs, 200)
	check(body, second)
	if bytes.Contains(body, []byte(first)) {
		t.Fatal("removed relation returned as active")
	}
	history := ticket + "/ci-run-link-history"
	pageBody := read(history+"?limit=2", 200)
	var page struct {
		Data struct {
			Next string `json:"next_cursor"`
		}
	}
	if err := json.Unmarshal(pageBody, &page); err != nil || page.Data.Next == "" {
		t.Fatal(string(pageBody), err)
	}
	check(pageBody, second, first, `"relation_state":"removed"`)
	check(read(history+"?limit=2&after="+url.QueryEscape(page.Data.Next), 200), canceled, failed)
	read(runs+"?after="+url.QueryEscape(page.Data.Next), 400)

	guest, reader := f.invite(t, "ci-run-unrelated@example.test")
	check(contextHTTPRead(t, s, guest, run+"/tickets?limit=1", 200), `"items":[]`, `"next_cursor":null`)
	contextHTTPRead(t, s, guest, runs, 404)
	public := f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: f.workspace,
		Name: "Public trace project", Key: "public-trace", OwnerTeamID: team, InitialAdminUserID: owner.ID, Visibility: "workspace"}).Object.ID
	channel := f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: public,
		Name: "Public trace channel", Visibility: "project", MemberUserIDs: []string{}}).Object.ID
	execution := goldenpath.NewTicketExecutionService(f.store, goldenpath.CryptoIDGenerator{})
	trace := goldenpath.NewTicketCIRunService(f.store, goldenpath.CryptoIDGenerator{})
	var publicTickets []string
	for i := range 2 {
		operation := fmt.Sprintf("public-trace-%d", i)
		inv := contextInvocation(owner)
		source, err := f.core.CreateMessage(f.ctx, inv, goldenpath.CreateMessageInput{ChannelID: channel, Body: "Synthetic reverse pagination", ClientOperationID: operation})
		if err != nil {
			t.Fatal(err)
		}
		result, err := execution.Write(f.ctx, inv, goldenpath.TicketFromMessage, goldenpath.TicketExecutionInput{ChannelID: channel, MessageID: source.Message.ID, Title: "Public " + operation, ClientOperationID: operation, Confirmed: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = trace.Write(f.ctx, inv, goldenpath.TicketCIRunLink, goldenpath.TicketCIRunInput{TicketID: result.Ticket.ID, RunID: succeeded, ClientOperationID: operation, Confirmed: true}); err != nil {
			t.Fatal(err)
		}
		publicTickets = append(publicTickets, result.Ticket.ID)
	}
	slices.Sort(publicTickets)
	reverse := contextHTTPRead(t, s, guest, run+"/tickets?limit=1", 200)
	check(reverse, publicTickets[0])
	if err := json.Unmarshal(reverse, &page); err != nil || page.Data.Next == "" || bytes.Contains(reverse, []byte(f.ticket)) {
		t.Fatal(string(reverse), err)
	}
	contextHTTPRead(t, s, f.owner, run+"/tickets?limit=1&after="+url.QueryEscape(page.Data.Next), 400)
	reverse = contextHTTPRead(t, s, guest, run+"/tickets?limit=1&after="+url.QueryEscape(page.Data.Next), 200)
	check(reverse, publicTickets[1], `"next_cursor":null`)
	if bytes.Contains(reverse, []byte(f.ticket)) {
		t.Fatal("restricted Ticket leaked into reverse page")
	}

	f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "project.member.set", ScopeID: f.project, UserID: reader.ID, Role: "contributor"})
	revocationRun := makeRun("succeeded", "revoked-actor")
	guestLink := linkBody("revoked-actor", revocationRun)
	executionWriteHTTP(t, s, guest, "POST", runs, guestLink, 201)
	role := "contributor"
	f.configure(t, owner, goldenpath.ConfigurationInput{Kind: "project.member.remove", ScopeID: f.project, UserID: reader.ID, ExpectedRole: &role})
	executionWriteHTTP(t, s, guest, "POST", runs, guestLink, 404)
	// Archive keeps read access but prevents replays from recovering a write.
	if _, err := f.pool.Exec(f.ctx, `UPDATE radishnexus.projects SET status='archived' WHERE workspace_id=$1 AND id=$2`, f.workspace, f.project); err != nil {
		t.Fatal(err)
	}
	check(read(runs, 200), `"can_link":false`, second)
	write("POST", runs, proposal, 403)
	if _, err := f.pool.Exec(f.ctx, `UPDATE radishnexus.projects SET status='active' WHERE workspace_id=$1 AND id=$2`, f.workspace, f.project); err != nil {
		t.Fatal(err)
	}
	var ticketAfter time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT updated_at FROM radishnexus.tickets WHERE workspace_id=$1 AND id=$2`, f.workspace, f.ticket).Scan(&ticketAfter); err != nil || !ticketBefore.Equal(ticketAfter) {
		t.Fatal("relationship changed ticket", err)
	}

	before := read(history, 200)
	if _, err := f.store.RebuildActivityProjection(f.ctx); err != nil {
		t.Fatal(err)
	}
	if after := read(history, 200); !bytes.Equal(before, after) {
		t.Fatalf("Activity rebuild drift: %s vs %s", before, after)
	}

	source := f.bundle(t)
	digest := cp.Digest(source.Manifest)
	for round := range 2 {
		data, index, err := cp.Unpack(source)
		if err != nil || source.Manifest.Profile != cp.Profile || len(data.Links) == 0 ||
			len(index.Decoded) == 0 || (round == 0 && digest != cp.Digest(source.Manifest)) {
			t.Fatal("v9 collaboration scope", round, err)
		}
		for _, file := range source.Files {
			for _, id := range []string{succeeded, failed, canceled, revocationRun, deployment, first, second, "tracks-build", "ticket.ci-run-linked"} {
				if bytes.Contains(file.Content, []byte(id)) {
					t.Fatalf("portable scope included %s in %s", id, file.Path)
				}
			}
		}
		target := contextDatabase(t)
		plan := target.plan(t, source)
		result, err := target.service.Apply(target.ctx, target.principal, target.owner.Token, source, plan, plan.PlanDigest)
		if err != nil {
			t.Fatal(round, err)
		}
		source, err = target.service.Export(target.ctx, target.principal, target.owner.Token, result.ProjectID)
		if err != nil {
			t.Fatal(round, err)
		}
	}
}

func TestTicketCIRunConcurrentAndAtomic(t *testing.T) {
	ctx, pool, core, makeRun := stagingDatabase(t)
	ticket, _ := componentTicket(t, ctx, core, "chn_project", "ci-trace-race")
	id := makeRun("succeeded")
	service := goldenpath.NewTicketCIRunService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
	in := goldenpath.TicketCIRunInput{TicketID: ticket.ID, RunID: id, ClientOperationID: "trace-concurrent", Confirmed: true}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, actor := range []string{"usr_contributor", "usr_decider"} {
		go func() {
			<-start
			_, err := service.Write(ctx, tcInvocation(actor), goldenpath.TicketCIRunLink, in)
			results <- err
		}()
	}
	close(start)
	first, second := <-results, <-results
	if !((first == nil && errors.Is(second, authz.ErrConflict)) || (second == nil && errors.Is(first, authz.ErrConflict))) {
		t.Fatal(first, second)
	}
	other := makeRun("failed")
	in.RunID, in.ClientOperationID = other, "retry-after-rollback"
	if _, err := pool.Exec(ctx, `CREATE FUNCTION radishnexus.fail_trace_projection() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected CI Run projection failure'; END $$;
		CREATE TRIGGER fail_trace_projection BEFORE INSERT ON radishnexus.activity_items
		FOR EACH ROW WHEN (NEW.activity_type='ticket.ci-run-linked') EXECUTE FUNCTION radishnexus.fail_trace_projection()`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Write(ctx, tcInvocation("usr_contributor"), goldenpath.TicketCIRunLink, in); err == nil || !strings.Contains(err.Error(), "injected CI Run projection failure") {
		t.Fatal(err)
	}
	var leaked bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.entity_links WHERE relation_type='tracks-build' AND to_id=$1)
		OR EXISTS(SELECT 1 FROM radishnexus.collaboration_command_receipts WHERE client_operation_id='retry-after-rollback')`, other).Scan(&leaked); err != nil || leaked {
		t.Fatal(leaked, err)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER fail_trace_projection ON radishnexus.activity_items; DROP FUNCTION radishnexus.fail_trace_projection()`); err != nil {
		t.Fatal(err)
	}
	if v, err := service.Write(ctx, tcInvocation("usr_contributor"), goldenpath.TicketCIRunLink, in); err != nil || !v.Created {
		t.Fatal(v, err)
	}
}
