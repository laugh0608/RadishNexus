//go:build integration

package postgres_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func legacyTicketClassificationBundle(t *testing.T, b cp.Bundle, profile string) cp.Bundle {
	t.Helper()
	b.Manifest.Profile = profile
	b.Files = slices.Clone(b.Files)
	b.Manifest.Files = slices.Clone(b.Manifest.Files)
	for i, file := range b.Files {
		if file.Path != "objects.jsonl" {
			continue
		}
		var out bytes.Buffer
		for _, line := range bytes.Split(bytes.TrimSpace(file.Content), []byte("\n")) {
			var o cp.Object
			if err := cp.Decode(line, &o); err != nil {
				t.Fatal(err)
			}
			if o.Type == "ticket" {
				var fields map[string]json.RawMessage
				if err := cp.Decode(o.Data, &fields); err != nil {
					t.Fatal(err)
				}
				delete(fields, "ticket_type")
				delete(fields, "classification_revision")
				delete(fields, "label_ids")
				o.Data = cp.Encode(fields)
			}
			out.Write(cp.Encode(o))
			out.WriteByte('\n')
		}
		b.Files[i].Content = out.Bytes()
		for j, d := range b.Manifest.Files {
			if d.Path == file.Path {
				b.Manifest.Files[j].SHA256 = cp.Hash(out.Bytes())
				b.Manifest.Files[j].Bytes = int64(out.Len())
			}
		}
	}
	return b
}

func TestTicketClassificationHTTPSAndTwoImports(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	legacy := legacyTicketClassificationBundle(t, f.bundle(t), cp.ProfileV8)
	digest := cp.Digest(legacy.Manifest)
	if _, index, err := cp.Unpack(legacy); err != nil || cp.Digest(legacy.Manifest) != digest ||
		index.Decoded[cp.Ref{Type: "ticket", ID: f.ticket}].(cp.Ticket).ClassificationRevision != 1 {
		t.Fatal("strict v8 read", err)
	}
	s, _ := contextHTTPS(t, f, "")
	ws := "/api/v1/workspaces/" + f.workspace
	project := ws + "/projects/" + f.project
	directory := project + "/ticket-labels"
	setLabel := func(path string, fields map[string]any, status int) goldenpath.TicketLabelEntry {
		t.Helper()
		fields["confirmed"] = true
		result := executionWriteHTTP(t, s, f.owner, "POST", path, string(cp.Encode(fields)), status)
		var entry struct{ Data goldenpath.TicketLabelEntry }
		if status == 200 && json.Unmarshal(result, &entry) != nil {
			t.Fatal(string(result))
		}
		return entry.Data
	}
	alpha := setLabel(directory, map[string]any{"client_operation_id": "create-alpha", "name": "  Alpha  "}, 200)
	other := setLabel(directory, map[string]any{"client_operation_id": "create-beta", "name": "研发"}, 200)
	setLabel(directory, map[string]any{"client_operation_id": "name-conflict", "name": "alpha"}, 409)
	ctx := func(path string, want int) string { return string(contextHTTPRead(t, s, f.owner, path, want)) }
	requireBody := func(body string, want ...string) {
		t.Helper()
		for _, fragment := range want {
			if !strings.Contains(body, fragment) {
				t.Fatalf("missing %q in %s", fragment, body)
			}
		}
	}
	requireBody(ctx(directory+"?status=all&limit=1", 200), `"next_cursor":"`, `"can_manage":true`)
	ticket := ws + "/tickets/" + f.ticket
	requireBody(ctx(ticket+"/classification", 200), `"ticket_type":"unset"`, `"classification_revision":1`, `"can_set":true`)
	ch := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: f.project, Name: "Synthetic discovery", Visibility: "project", MemberUserIDs: []string{}}).Object.ID
	inv := contextInvocation(f.principal)
	message, err := f.core.CreateMessage(f.ctx, inv, goldenpath.CreateMessageInput{ChannelID: ch, ClientOperationID: "class-message", Body: "Synthetic classification source"})
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := ws + "/channels/" + ch + "/messages/" + message.Message.ID + "/tickets"
	decisionPath := ws + "/decisions/" + f.decision + "/tickets"
	classify := func(operation, kind string, rev int, ids ...string) string {
		if ids == nil {
			ids = []string{}
		}
		return string(cp.Encode(map[string]any{"client_operation_id": operation, "confirmed": true, "ticket_type": kind, "label_ids": ids, "base_classification_revision": rev}))
	}
	create := func(path, operation, title, kind string, ids []string, want int) string {
		t.Helper()
		body := cp.Encode(map[string]any{"client_operation_id": operation, "title": title, "classification": cp.Classification{Type: kind, LabelIDs: ids}, "confirmed": true})
		if path == decisionPath {
			body = cp.Encode(map[string]any{"client_operation_id": operation, "title": title, "classification": cp.Classification{Type: kind, LabelIDs: ids}})
		}
		raw := executionWriteHTTP(t, s, f.owner, "POST", path, string(body), want)
		if want >= 400 {
			return ""
		}
		var result struct {
			Data struct{ Ticket struct{ Ref cp.Ref } }
		}
		if err := json.Unmarshal(raw, &result); err != nil || result.Data.Ticket.Ref.ID == "" {
			t.Fatal(string(raw), err)
		}
		return result.Data.Ticket.Ref.ID
	}
	if got := create(decisionPath, "source-ticket", "Historical work", "unset", []string{}, 200); got != f.ticket {
		t.Fatal(got, f.ticket)
	}
	messageTicket := create(sourcePath, "class-create-message", "Work 100%_Alpha", "defect", []string{alpha.Ref.ID, other.Ref.ID}, 201)
	decisionTicket := create(decisionPath, "class-create-decision", "Work 100xyAlpha", "task", []string{alpha.Ref.ID}, 201)
	create(decisionPath, "class-atomic-rollback", "Temporary outside", "task", []string{"lbl_wrong_scope"}, 400)
	create(decisionPath, "class-atomic-rollback", "Retried transaction", "task", []string{}, 201)
	write := func(path, body string, want int) {
		t.Helper()
		executionWriteHTTP(t, s, f.owner, "POST", path, body, want)
	}
	original := classify("class-canceled", "task", 1, alpha.Ref.ID)
	write(ticket+"/classification", original, 200)
	setLabel(directory+"/"+alpha.Ref.ID+"/rename", map[string]any{"client_operation_id": "rename-alpha", "base_revision": 1, "name": "Delivery"}, 200)
	setLabel(directory+"/"+alpha.Ref.ID+"/archive", map[string]any{"client_operation_id": "archive-alpha", "base_revision": 2}, 200)
	write(ticket+"/classification", original, 200)
	write(ticket+"/classification", classify("new-stale", "task", 1, alpha.Ref.ID), 409)
	write(ticket+"/classification", classify("same", "task", 2, alpha.Ref.ID), 409)
	write(ticket+"/classification", classify("retain", "requirement", 2, alpha.Ref.ID), 200)
	write(ticket+"/classification", classify("remove", "requirement", 3), 200)
	write(ticket+"/classification", classify("cannot-restore", "task", 4, alpha.Ref.ID), 409)
	if got := create(sourcePath, "class-create-message", "Work 100%_Alpha", "defect", []string{other.Ref.ID, alpha.Ref.ID}, 200); got != messageTicket {
		t.Fatal(got, messageTicket)
	}
	create(sourcePath, "class-create-message", "Work 100%_Alpha", "requirement", []string{alpha.Ref.ID, other.Ref.ID}, 409)
	requireBody(ctx(ws+"/tickets/"+messageTicket+"/classification", 200), `"name":"Delivery"`, `"status":"archived"`, `"classification_revision":2`)
	requireBody(ctx(ws+"/tickets/"+messageTicket+"/nexus-view?include=classification,execution_context", 400), `"error"`)
	requireBody(ctx(ws+"/tickets/"+messageTicket+"/nexus-view?include=execution_context,classification", 200), `"name":"Alpha"`, `"activity_type":"ticket.classification-changed"`)
	filter := project + "/tickets?include=due_date,classification&labels=" + other.Ref.ID + "," + alpha.Ref.ID + "&q=100%25_ALPHA&type=defect"
	body := ctx(filter, 200)
	requireBody(body, messageTicket, `"due_date":null`, `"status":"archived"`)
	if strings.Contains(body, decisionTicket) || strings.Contains(ctx(project+"/tickets?q=100%25_Alpha", 200), `"classification"`) {
		t.Fatal("unmatched or unrequested fields returned")
	}
	ctx(project+"/tickets?labels=lbl_wrong_scope", 400)
	assignments := goldenpath.NewTicketAssignmentService(f.store, goldenpath.CryptoIDGenerator{})
	candidates, err := assignments.ListAssigneeCandidates(f.ctx, f.principal, messageTicket, goldenpath.DiscoveryPageInput{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	assigned := false
	for _, candidate := range candidates.Items {
		if candidate.ID == f.principal.ID {
			_, err = assignments.SetTicketAssignee(f.ctx, inv, goldenpath.TicketAssignmentInput{TicketID: messageTicket, ClientOperationID: "classification-my-tickets", BaseAssignmentRevision: 1,
				BaseStatusRevision: 1, AssigneeUserID: &f.principal.ID, TargetLifecycleRevision: candidate.LifecycleRevision, ProjectManagementRevision: candidates.ProjectManagementRevision, Confirmed: true})
			if err != nil {
				t.Fatal(err)
			}
			assigned = true
		}
	}
	if !assigned {
		t.Fatal("current actor missing from assignment candidates")
	}
	requireBody(ctx(ws+"/my-tickets?include=classification", 200), messageTicket, `"ticket_type":"defect"`, `"name":"Delivery"`, `"status":"archived"`)

	member, reader := f.invite(t, "classification-viewer@example.test")
	f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "project.member.set", ScopeID: f.project, UserID: reader.ID, Role: "viewer"})
	requireBody(string(contextHTTPRead(t, s, member, ticket+"/classification", 200)), `"can_set":false`)
	executionWriteHTTP(t, s, member, "POST", ticket+"/classification", classify("viewer-write", "task", 4), 403)
	executionWriteHTTP(t, s, member, "POST", directory, `{"name":"Viewer","confirmed":true,"client_operation_id":"viewer-label"}`, 403)

	source := f.bundle(t)
	for round := range 2 {
		target := contextDatabase(t)
		plan := target.plan(t, source)
		result, err := target.service.Apply(target.ctx, target.principal, target.owner.Token, source, plan, plan.PlanDigest)
		if err != nil {
			t.Fatal(round, err)
		}
		classes := goldenpath.NewTicketClassificationService(target.store, goldenpath.CryptoIDGenerator{})
		label := contextMapping(t, plan, "ticket-label", alpha.Ref.ID)
		otherLabel := contextMapping(t, plan, "ticket-label", other.Ref.ID)
		first := contextMapping(t, plan, "ticket", messageTicket)
		second := contextMapping(t, plan, "ticket", decisionTicket)
		snapshot, err := classes.ReadTicketClassification(target.ctx, target.principal, first)
		if err != nil || snapshot.Type != "defect" || snapshot.Revision != 2 || len(snapshot.Labels) != 2 {
			t.Fatal(round, snapshot, err)
		}
		if _, err = target.store.RebuildActivityProjection(target.ctx); err != nil {
			t.Fatal(err)
		}
		if round == 0 {
			_, err = classes.WriteLabel(target.ctx, contextInvocation(target.principal), goldenpath.LabelInput{ProjectID: result.ProjectID, LabelID: otherLabel, Action: "rename", Name: "Integration", BaseRevision: 1, ClientOperationID: "native-after-import", Confirmed: true})
			if err != nil {
				t.Fatal(err)
			}
			_, err = classes.Set(target.ctx, contextInvocation(target.principal), goldenpath.ClassificationInput{TicketID: second, Classification: cp.Classification{Type: "requirement", LabelIDs: []string{label, otherLabel}}, BaseRevision: 2, ClientOperationID: "native-class-after-import", Confirmed: true})
			if err != nil {
				t.Fatal(err)
			}
		} else {
			snapshot, err = classes.ReadTicketClassification(target.ctx, target.principal, second)
			if err != nil || snapshot.Type != "requirement" || snapshot.Revision != 3 || len(snapshot.Labels) != 2 {
				t.Fatal(round, snapshot, err)
			}
		}
		source, err = target.service.Export(target.ctx, target.principal, target.owner.Token, result.ProjectID)
		if err != nil {
			t.Fatal(round, err)
		}
		alpha.Ref.ID, other.Ref.ID, messageTicket, decisionTicket = label, otherLabel, first, second
	}
}

func TestTicketClassificationCatalogConcurrencyAndScope(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	svc := goldenpath.NewTicketClassificationService(f.store, goldenpath.CryptoIDGenerator{})
	inv := contextInvocation(f.principal)
	newLabel := func(project, name string) goldenpath.TicketLabelEntry {
		t.Helper()
		label, err := svc.WriteLabel(f.ctx, inv, goldenpath.LabelInput{ProjectID: project, Name: name, Action: "create", ClientOperationID: name, Confirmed: true})
		if err != nil {
			t.Fatal(err)
		}
		return label
	}
	team := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: f.workspace, Name: "Other label team"}).Object.ID
	parallel := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: f.workspace, Name: "Other label scope", Key: "otherscope", OwnerTeamID: team, InitialAdminUserID: f.principal.ID, Visibility: "restricted"}).Object.ID
	foreign := newLabel(parallel, "DifferentProject")
	archive := newLabel(f.project, "ArchiveRace")
	classification := newLabel(f.project, "RenameRace")
	base := goldenpath.ClassificationInput{TicketID: f.ticket, BaseRevision: 1, ClientOperationID: "classification-race", Confirmed: true,
		Classification: cp.Classification{Type: "task", LabelIDs: []string{foreign.Ref.ID}}}
	if _, err := svc.Set(f.ctx, inv, base); !errors.Is(err, authz.ErrInvalid) {
		t.Fatal("foreign label accepted", err)
	}
	archiveInput := goldenpath.LabelInput{ProjectID: f.project, LabelID: archive.Ref.ID, BaseRevision: 1, Action: "archive", ClientOperationID: "archive-race", Confirmed: true}
	base.Classification.LabelIDs = []string{archive.Ref.ID}
	start := make(chan struct{})
	writes := make(chan error, 2)
	go func() { <-start; _, err := svc.Set(f.ctx, inv, base); writes <- err }()
	go func() { <-start; _, err := svc.WriteLabel(f.ctx, inv, archiveInput); writes <- err }()
	close(start)
	first, second := <-writes, <-writes
	if (first != nil && !errors.Is(first, authz.ErrConflict)) || (second != nil && !errors.Is(second, authz.ErrConflict)) || (first != nil && second != nil) {
		t.Fatal(first, second)
	}
	snapshot, err := svc.ReadTicketClassification(f.ctx, f.principal, f.ticket)
	if err != nil || (snapshot.Revision == 2 && (len(snapshot.Labels) != 1 || snapshot.Labels[0].Status != "archived")) ||
		(snapshot.Revision == 1 && len(snapshot.Labels) != 0) {
		t.Fatal(snapshot, err)
	}
	start = make(chan struct{})
	for _, name := range []string{"Race Winner A", "Race Winner B"} {
		go func() {
			<-start
			_, err := svc.WriteLabel(f.ctx, inv, goldenpath.LabelInput{ProjectID: f.project, LabelID: classification.Ref.ID, BaseRevision: 1,
				Name: name, Action: "rename", ClientOperationID: strings.ReplaceAll(name, " ", "-"), Confirmed: true})
			writes <- err
		}()
	}
	close(start)
	first, second = <-writes, <-writes
	if !((first == nil && errors.Is(second, authz.ErrConflict)) || (second == nil && errors.Is(first, authz.ErrConflict))) {
		t.Fatal(first, second)
	}
	if _, err = f.pool.Exec(f.ctx, `UPDATE radishnexus.projects SET status='archived' WHERE workspace_id=$1 AND id=$2`, f.workspace, f.project); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.ReadTicketClassification(f.ctx, f.principal, f.ticket); err != nil || got.CanSet {
		t.Fatal("archived Project read", got, err)
	}
	if _, err := svc.Set(f.ctx, inv, base); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("archived Project write", err)
	}
}
