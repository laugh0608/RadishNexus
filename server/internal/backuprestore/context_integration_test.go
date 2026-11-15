//go:build integration

package backuprestore

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/db"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBackupRestoreImportedContext(t *testing.T) {
	sourceURL, targetURL := os.Getenv("SOURCE_DATABASE_URL"), os.Getenv("TARGET_DATABASE_URL")
	if sourceURL == "" || targetURL == "" {
		t.Skip("source and target required")
	}
	ctx := context.Background()
	password := "context backup synthetic password"
	schema := func(ctx context.Context, tx pgx.Tx) error {
		checker, e := db.NewReadinessChecker(tx)
		if e != nil {
			return e
		}
		return checker.CheckReady(ctx)
	}
	initialize := func(base string) (string, *pgx.Conn, *pgxpool.Pool, authn.Session, authz.Principal, *goldenpath.ContextService) {
		uri := recoveryBackupDatabase(t, ctx, base)
		conn := connectIntegrationDatabase(t, ctx, uri)
		t.Cleanup(func() { conn.Close(ctx) })
		if e := db.Migrate(ctx, conn); e != nil {
			t.Fatal(e)
		}
		pool := connectIntegrationPool(t, ctx, uri)
		t.Cleanup(pool.Close)
		auth := authn.NewService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
		boot, e := auth.Bootstrap(ctx, authn.BootstrapInput{Email: "context-backup@example.test", Password: password, DisplayName: "Context backup", WorkspaceName: "Context backup"})
		if e != nil {
			t.Fatal(e)
		}
		session, e := auth.Login(ctx, authn.LoginInput{Email: "context-backup@example.test", Password: password})
		if e != nil {
			t.Fatal(e)
		}
		principal := authz.Principal{Kind: authz.PrincipalUser, ID: boot.UserID, WorkspaceID: boot.WorkspaceID}
		return uri, conn, pool, session, principal, goldenpath.NewContextService(goldenpostgres.NewContextStore(pool, schema))
	}
	_, _, original, session, principal, exporter := initialize(sourceURL)
	configuration := goldenpath.NewConfigurationService(goldenpostgres.New(original), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	invocation := goldenpath.Invocation{Principal: principal, SourceKind: "web", CorrelationID: "cor_context_backup"}
	team, e := configuration.Configure(ctx, invocation, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: principal.WorkspaceID, ClientOperationID: "context-backup-team", Name: "Context team"})
	if e != nil {
		t.Fatal(e)
	}
	project, e := configuration.Configure(ctx, invocation, goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: principal.WorkspaceID, ClientOperationID: "context-backup-project", Name: "Context project", Key: "context", OwnerTeamID: team.Object.ID, InitialAdminUserID: principal.ID, Visibility: "restricted"})
	if e != nil {
		t.Fatal(e)
	}
	core := goldenpath.NewService(goldenpostgres.New(original), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	channel, e := configuration.Configure(ctx, invocation, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: project.Object.ID, ClientOperationID: "assignment-backup-channel", Name: "Responsibility", Visibility: "project", MemberUserIDs: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	message, e := core.CreateMessage(ctx, invocation, goldenpath.CreateMessageInput{ChannelID: channel.Object.ID, ClientOperationID: "assignment-backup-message", Body: "Synthetic responsibility source"})
	if e != nil {
		t.Fatal(e)
	}
	thread, e := core.StartThreadFromMessage(ctx, invocation, goldenpath.StartThreadFromMessageInput{ChannelID: channel.Object.ID, MessageID: message.Message.ID, Title: "Responsibility", Visibility: "project"})
	if e != nil {
		t.Fatal(e)
	}
	decision, e := core.CreateDecisionFromThread(ctx, invocation, goldenpath.CreateDecisionInput{ThreadID: thread.ID, ClientOperationID: "assignment-backup-proposal", Question: "Who owns this?"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = core.AcceptDecision(ctx, invocation, goldenpath.AcceptDecisionInput{DecisionID: decision.Decision.ID, ClientOperationID: "assignment-backup-accept", Outcome: "Assign", Rationale: "Keep responsibility"}); e != nil {
		t.Fatal(e)
	}
	ticket, e := core.CreateTicketFromDecision(ctx, invocation, goldenpath.CreateTicketInput{DecisionID: decision.Decision.ID, ClientOperationID: "assignment-backup-ticket", Title: "Restore responsibility"})
	if e != nil {
		t.Fatal(e)
	}
	classification := goldenpath.NewTicketClassificationService(goldenpostgres.New(original), goldenpath.CryptoIDGenerator{})
	sourceLabel, e := classification.WriteLabel(ctx, invocation, goldenpath.LabelInput{ProjectID: project.Object.ID, Action: "create", Name: "Synthetic source label", ClientOperationID: "source-label", Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = classification.Set(ctx, invocation, goldenpath.ClassificationInput{TicketID: ticket.Ticket.ID, BaseRevision: 1, ClientOperationID: "source-classification", Confirmed: true, Classification: cp.Classification{Type: "task", LabelIDs: []string{sourceLabel.Ref.ID}}}); e != nil {
		t.Fatal(e)
	}
	assignments := goldenpath.NewTicketAssignmentService(goldenpostgres.New(original), goldenpath.CryptoIDGenerator{})
	candidates, e := assignments.ListAssigneeCandidates(ctx, principal, ticket.Ticket.ID, goldenpath.DiscoveryPageInput{Limit: 25})
	if e != nil || len(candidates.Items) != 1 {
		t.Fatal(candidates, e)
	}
	if _, e = assignments.SetTicketAssignee(ctx, invocation, goldenpath.TicketAssignmentInput{TicketID: ticket.Ticket.ID, ClientOperationID: "assignment-backup-set", BaseAssignmentRevision: 1, BaseStatusRevision: 1, AssigneeUserID: &principal.ID, TargetLifecycleRevision: candidates.Items[0].LifecycleRevision, ProjectManagementRevision: candidates.ProjectManagementRevision, Confirmed: true}); e != nil {
		t.Fatal(e)
	}
	priorities := goldenpath.NewTicketPriorityService(goldenpostgres.New(original), goldenpath.CryptoIDGenerator{})
	if _, e = priorities.SetTicketPriority(ctx, invocation, goldenpath.TicketPriorityInput{TicketID: ticket.Ticket.ID, ClientOperationID: "priority-backup-set", BasePriorityRevision: 1, BaseStatusRevision: 1, Priority: "high", Confirmed: true}); e != nil {
		t.Fatal(e)
	}
	day := "2032-02-29"
	dates := goldenpath.NewTicketDueDateService(goldenpostgres.New(original), goldenpath.CryptoIDGenerator{})
	dateInput := goldenpath.TicketDueDateInput{TicketID: ticket.Ticket.ID, ClientOperationID: "due-backup-set", BaseDueDateRevision: 1, BaseStatusRevision: 1, DueDate: &day, Confirmed: true}
	if _, e = dates.SetTicketDueDate(ctx, invocation, dateInput); e != nil {
		t.Fatal(e)
	}
	rejected, e := core.CreateDecisionFromThread(ctx, invocation, goldenpath.CreateDecisionInput{ThreadID: thread.ID, ClientOperationID: "rejection-backup-proposal", Question: "Rejected source must survive"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = core.RejectDecision(ctx, invocation, goldenpath.RejectDecisionInput{DecisionID: rejected.Decision.ID, ClientOperationID: "rejection-backup-original", RejectionReason: "Retain rejected history in backup"}); e != nil {
		t.Fatal(e)
	}
	replacement, e := core.CreateDecisionFromThread(ctx, invocation, goldenpath.CreateDecisionInput{ThreadID: thread.ID, ClientOperationID: "backup-replacement-proposal", Question: "Replacement keeps original execution"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = core.AcceptDecision(ctx, invocation, goldenpath.AcceptDecisionInput{DecisionID: replacement.Decision.ID, ClientOperationID: "backup-replacement-accept", Outcome: "Replacement", Rationale: "Preserve context"}); e != nil {
		t.Fatal(e)
	}
	if _, applied, err := core.ReviewDecision(ctx, invocation, goldenpath.ReviewDecisionInput{
		DecisionID: decision.Decision.ID, ClientOperationID: "source-review-backup", BaseDecisionRevision: 2,
		Conclusion: "reconsider", Reason: "Review retained before replacement.",
	}); err != nil || !applied {
		t.Fatal("source review", err)
	}
	if _, e = core.SupersedeDecision(ctx, invocation, goldenpath.SupersedeDecisionInput{DecisionID: decision.Decision.ID, ReplacementID: replacement.Decision.ID, ClientOperationID: "backup-original-supersession", BaseDecisionRevision: 2, BaseReplacementRevision: 2, SupersessionReason: "Original historical supersession"}); e != nil {
		t.Fatal(e)
	}
	execution := goldenpath.NewTicketExecutionService(goldenpostgres.New(original), goldenpath.CryptoIDGenerator{})
	sourceTicket, e := execution.Write(ctx, invocation, goldenpath.TicketFromMessage, goldenpath.TicketExecutionInput{ChannelID: channel.Object.ID, MessageID: message.Message.ID, Title: "Work from original message", ClientOperationID: "backup-execution-source", Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = execution.Write(ctx, invocation, goldenpath.TicketDiscussionLink, goldenpath.TicketExecutionInput{TicketID: sourceTicket.Ticket.ID, ThreadID: thread.ID, ClientOperationID: "backup-execution-link", Confirmed: true}); e != nil {
		t.Fatal(e)
	}

	bundle, e := exporter.Export(ctx, principal, session.Token, project.Object.ID)
	if e != nil {
		t.Fatal(e)
	}
	importedURL, importedConn, imported, owner, actor, importer := initialize(sourceURL)
	plan, e := importer.Prepare(ctx, actor, owner.Token, bundle, "00000000-0000-4000-8000-000000000044")
	if e != nil {
		t.Fatal(e)
	}
	result, e := importer.Apply(ctx, actor, owner.Token, bundle, plan, plan.PlanDigest)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = importer.Export(ctx, actor, owner.Token, result.ProjectID); e != nil {
		t.Fatal(e)
	}
	var importedTeam, importedThread, importedDecision, importedReplacement, importedTicket, importedChannel, importedMessage, importedExecutionTicket string
	for _, m := range plan.Mappings {
		if m.Kind == "team" && m.SourceID == team.Object.ID {
			importedTeam = m.TargetID
		}
		if m.Kind == "channel" && m.SourceID == channel.Object.ID {
			importedChannel = m.TargetID
		}
		if m.Kind == "message" && m.SourceID == message.Message.ID {
			importedMessage = m.TargetID
		}
		if m.Kind == "ticket" && m.SourceID == sourceTicket.Ticket.ID {
			importedExecutionTicket = m.TargetID
		}
		if m.Kind == "decision" && m.SourceID == decision.Decision.ID {
			importedDecision = m.TargetID
		}
		if m.Kind == "decision" && m.SourceID == replacement.Decision.ID {
			importedReplacement = m.TargetID
		}
		if m.Kind == "ticket" && m.SourceID == ticket.Ticket.ID {
			importedTicket = m.TargetID
		}
		if m.Kind == "thread" && m.SourceID == thread.ID {
			importedThread = m.TargetID
		}
	}
	importedCore := goldenpath.NewService(goldenpostgres.New(imported), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	currentInvocation := goldenpath.Invocation{Principal: actor, SourceKind: "web", CorrelationID: "cor_restored_rejection"}
	dates = goldenpath.NewTicketDueDateService(goldenpostgres.New(imported), goldenpath.CryptoIDGenerator{})
	nativeDate := goldenpath.TicketDueDateInput{TicketID: importedTicket, ClientOperationID: "due-backup-clear", BaseDueDateRevision: 2, BaseStatusRevision: 1, Confirmed: true}
	cleared, e := dates.SetTicketDueDate(ctx, currentInvocation, nativeDate)
	if e != nil || cleared.DueDate != nil || cleared.DueDateRevision != 3 {
		t.Fatal(cleared, e)
	}
	native, e := importedCore.CreateDecisionFromThread(ctx, currentInvocation, goldenpath.CreateDecisionInput{ThreadID: importedThread, ClientOperationID: "native-backup-proposal", Question: "Native rejection receipt must survive"})
	if e != nil {
		t.Fatal(e)
	}
	rejectInput := goldenpath.RejectDecisionInput{DecisionID: native.Decision.ID, ClientOperationID: "native-backup-reject", RejectionReason: "Original native rejection"}
	if _, e = importedCore.RejectDecision(ctx, currentInvocation, rejectInput); e != nil {
		t.Fatal(e)
	}
	nativeReplacement, e := importedCore.CreateDecisionFromThread(ctx, currentInvocation, goldenpath.CreateDecisionInput{ThreadID: importedThread, ClientOperationID: "native-replacement-proposal", Question: "Continue imported replacement chain"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = importedCore.AcceptDecision(ctx, currentInvocation, goldenpath.AcceptDecisionInput{DecisionID: nativeReplacement.Decision.ID, ClientOperationID: "native-replacement-accept", Outcome: "Continue here", Rationale: "Current owner explicitly accepted"}); e != nil {
		t.Fatal(e)
	}
	reviewInput := goldenpath.ReviewDecisionInput{
		DecisionID: importedReplacement, ClientOperationID: "native-review-backup", BaseDecisionRevision: 3,
		Conclusion: "reconsider", Reason: "Native review retained before next replacement.",
	}
	review, applied, err := importedCore.ReviewDecision(ctx, currentInvocation, reviewInput)
	if err != nil || !applied {
		t.Fatal("native review", err)
	}
	supersedeInput := goldenpath.SupersedeDecisionInput{DecisionID: importedReplacement, ReplacementID: nativeReplacement.Decision.ID, ClientOperationID: "native-replacement-command", BaseDecisionRevision: 3, BaseReplacementRevision: 2, SupersessionReason: "Native continuation after imported supersession"}
	superseded, e := importedCore.SupersedeDecision(ctx, currentInvocation, supersedeInput)
	if e != nil {
		t.Fatal(e)
	}
	execution = goldenpath.NewTicketExecutionService(goldenpostgres.New(imported), goldenpath.CryptoIDGenerator{})
	nativeExecutionInput := goldenpath.TicketExecutionInput{ChannelID: importedChannel, MessageID: importedMessage, Title: "Native message execution", ClientOperationID: "backup-native-execution", Confirmed: true}
	nativeExecution, e := execution.Write(ctx, currentInvocation, goldenpath.TicketFromMessage, nativeExecutionInput)
	if e != nil {
		t.Fatal(e)
	}
	nativeLinkInput := goldenpath.TicketExecutionInput{TicketID: nativeExecution.Ticket.ID, ThreadID: importedThread, ClientOperationID: "backup-native-discussion", Confirmed: true}
	nativeLink, e := execution.Write(ctx, currentInvocation, goldenpath.TicketDiscussionLink, nativeLinkInput)
	if e != nil {
		t.Fatal(e)
	}
	nativeUnlinkInput := goldenpath.TicketExecutionInput{TicketID: nativeExecution.Ticket.ID, LinkID: nativeLink.LinkID, ClientOperationID: "backup-native-discussion-removal", Confirmed: true}
	if _, e = execution.Write(ctx, currentInvocation, goldenpath.TicketDiscussionUnlink, nativeUnlinkInput); e != nil {
		t.Fatal(e)
	}

	classification = goldenpath.NewTicketClassificationService(goldenpostgres.New(imported), goldenpath.CryptoIDGenerator{})
	labelInput := goldenpath.LabelInput{ProjectID: result.ProjectID, Action: "create", Name: "Native backup", ClientOperationID: "native-label-create", Confirmed: true}
	nativeLabel, e := classification.WriteLabel(ctx, currentInvocation, labelInput)
	if e != nil {
		t.Fatal(e)
	}
	classInput := goldenpath.ClassificationInput{TicketID: nativeExecution.Ticket.ID, BaseRevision: 1, ClientOperationID: "native-classification", Confirmed: true, Classification: cp.Classification{Type: "defect", LabelIDs: []string{nativeLabel.Ref.ID}}}
	if _, e = classification.Set(ctx, currentInvocation, classInput); e != nil {
		t.Fatal(e)
	}
	archiveInput := goldenpath.LabelInput{ProjectID: result.ProjectID, LabelID: nativeLabel.Ref.ID, BaseRevision: 1, Action: "archive", ClientOperationID: "native-label-archive", Confirmed: true}
	if _, e = classification.WriteLabel(ctx, currentInvocation, archiveInput); e != nil {
		t.Fatal(e)
	}

	configuration = goldenpath.NewConfigurationService(goldenpostgres.New(imported), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	configureTrace := func(input goldenpath.ConfigurationInput) string {
		t.Helper()
		result, err := configuration.Configure(ctx, currentInvocation, input)
		if err != nil {
			t.Fatal(err)
		}
		return result.Object.ID
	}
	component := configureTrace(goldenpath.ConfigurationInput{Kind: "component.create", ScopeID: actor.WorkspaceID, ClientOperationID: "trace-component", Name: "Backup trace service", Key: "backup-trace", OwnerTeamID: importedTeam, Delivery: &goldenpath.DeliveryConfigurationInput{Type: "service"}})
	stage := configureTrace(goldenpath.ConfigurationInput{Kind: "environment.create", ScopeID: actor.WorkspaceID, ClientOperationID: "trace-stage", Name: "Backup staging", Key: "backup-stage", OwnerTeamID: importedTeam, Delivery: &goldenpath.DeliveryConfigurationInput{Classification: "staging"}})
	configureTrace(goldenpath.ConfigurationInput{Kind: "environment.authorization.grant", ScopeID: stage, UserID: actor.ID, ClientOperationID: "trace-stage-grant", Delivery: &goldenpath.DeliveryConfigurationInput{Confirmed: true}})
	now := time.Now().UTC().Truncate(time.Second)
	run, e := importedCore.RecordCompletedJenkinsRun(ctx, goldenpath.VerifiedJenkinsDelivery{WorkspaceID: actor.WorkspaceID, SourceID: "backup-trace", DeliveryID: "trace-delivery", PayloadSHA256: strings.Repeat("a", 64)},
		goldenpath.RecordCompletedCIRunInput{ComponentID: component, ExternalRunKey: "backup-trace-run", Status: "succeeded", CompletedAt: now.Add(-time.Minute)})
	if e != nil {
		t.Fatal(e)
	}
	staged, e := importedCore.RecordStagingDeployment(ctx, currentInvocation, goldenpath.RecordStagingDeploymentInput{EnvironmentID: stage, CIRunID: run.CIRun.ID, Status: "failed", CompletedAt: now, ClientOperationID: "trace-stage-record", Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	trace := goldenpath.NewTicketCIRunService(goldenpostgres.New(imported), goldenpath.CryptoIDGenerator{})
	traceInput := goldenpath.TicketCIRunInput{TicketID: nativeExecution.Ticket.ID, RunID: run.CIRun.ID, ClientOperationID: "trace-first-association", Confirmed: true}
	firstTrace, e := trace.Write(ctx, currentInvocation, goldenpath.TicketCIRunLink, traceInput)
	if e != nil {
		t.Fatal(e)
	}
	removeTrace := goldenpath.TicketCIRunInput{TicketID: traceInput.TicketID, LinkID: firstTrace.LinkID, ClientOperationID: "trace-remove", Confirmed: true}
	if _, e = trace.Write(ctx, currentInvocation, goldenpath.TicketCIRunUnlink, removeTrace); e != nil {
		t.Fatal(e)
	}
	newTrace := traceInput
	newTrace.ClientOperationID = "trace-second-generation"
	secondTrace, e := trace.Write(ctx, currentInvocation, goldenpath.TicketCIRunLink, newTrace)
	if e != nil || secondTrace.LinkID == firstTrace.LinkID {
		t.Fatal(secondTrace, e)
	}

	// Restore the entire Thread → Ticket → CI Run → Deployment path into a clean database.
	deliveryLinkInput := nativeLinkInput
	deliveryLinkInput.ClientOperationID = "backup-active-delivery-discussion"
	if _, e = execution.Write(ctx, currentInvocation, goldenpath.TicketDiscussionLink, deliveryLinkInput); e != nil {
		t.Fatal(e)
	}
	deliveryReader := goldenpath.NewThreadDeliveryService(goldenpostgres.New(imported))
	deliveryInput := goldenpath.DiscoveryPageInput{Limit: 25}
	deliveryPaths := []goldenpath.ThreadDeliveryScope{
		{ThreadID: importedThread},
		{ThreadID: importedThread, TicketID: traceInput.TicketID},
		{ThreadID: importedThread, TicketID: traceInput.TicketID, RunID: run.CIRun.ID},
	}
	deliveryBaseline := make([]goldenpath.ThreadDeliveryPage, len(deliveryPaths))
	for i, path := range deliveryPaths {
		deliveryBaseline[i], e = deliveryReader.List(ctx, actor, path, deliveryInput)
		if e != nil {
			t.Fatal(e)
		}
	}
	rootFound := false
	for _, item := range deliveryBaseline[0].Items {
		if item.Ref.ID == traceInput.TicketID {
			rootFound = reflect.DeepEqual(item.Origins, []string{goldenpath.DeliveryDiscussion})
		}
	}
	if !rootFound || len(deliveryBaseline[1].Items) != 1 || deliveryBaseline[1].Items[0].Ref.ID != run.CIRun.ID ||
		len(deliveryBaseline[2].Items) != 1 || deliveryBaseline[2].Items[0].Ref.ID != staged.ID {
		t.Fatal("source Thread delivery path is incomplete", deliveryBaseline)
	}

	componentRelations := goldenpath.NewTicketComponentService(goldenpostgres.New(imported), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	if _, e = componentRelations.WriteTicketComponent(ctx, currentInvocation, "ticket.component.link", goldenpath.TicketComponentInput{
		TicketID: importedTicket, ComponentID: component, ClientOperationID: "backup-nexus-component", Confirmed: true,
	}); e != nil {
		t.Fatal(e)
	}
	nexusSources := []entityref.Ref{
		{Type: "thread", ID: importedThread},
		{Type: "decision", ID: importedDecision},
		{Type: "ticket", ID: importedTicket},
	}
	type nexusSnapshot struct {
		summary goldenpath.NexusView
		pages   []goldenpath.NexusRelationPage
	}
	readNexusRelations := func(pool *pgxpool.Pool) []nexusSnapshot {
		t.Helper()
		reader := goldenpostgres.New(pool)
		result := make([]nexusSnapshot, len(nexusSources))
		for i, source := range nexusSources {
			summary, err := reader.GetNexusSummary(ctx, actor, source)
			if err != nil {
				t.Fatal(source.Type, err)
			}
			result[i].summary = summary
			input := goldenpath.NexusRelationPageInput{Limit: 1}
			for range 12 {
				page, err := reader.ListNexusRelations(ctx, actor, source, input)
				if err != nil {
					t.Fatal(source.Type, err)
				}
				result[i].pages = append(result[i].pages, page)
				if page.Next == nil {
					break
				}
				input.After = page.Next
			}
			if result[i].pages[len(result[i].pages)-1].Next != nil {
				t.Fatal("Nexus View pagination did not terminate", source.Type)
			}
		}
		return result
	}
	nexusBefore := readNexusRelations(imported)
	if len(nexusBefore[0].pages) < 2 || len(nexusBefore[1].pages[0].Items) != 1 ||
		nexusBefore[1].pages[0].Items[0].Target.ID != importedTicket || len(nexusBefore[2].pages[0].Items) != 1 ||
		nexusBefore[2].pages[0].Items[0].Target.ID != component {
		t.Fatal("incomplete relation backup fixture")
	}

	baseline := snapshotIncludedTables(t, ctx, imported)
	activity := snapshotTable(t, ctx, imported, "radishnexus.activity_items")
	restoredURL := recoveryBackupDatabase(t, ctx, targetURL)
	restoredConn := connectIntegrationDatabase(t, ctx, restoredURL)
	defer restoredConn.Close(ctx)
	path := filepath.Join(t.TempDir(), "imported-context")
	service := NewService()
	if _, e = service.Backup(ctx, importedConn, importedURL, path); e != nil {
		t.Fatal(e)
	}
	if _, e = service.Restore(ctx, restoredConn, restoredURL, path); e != nil {
		t.Fatal(e)
	}
	restored := connectIntegrationPool(t, ctx, restoredURL)
	defer restored.Close()
	if !reflect.DeepEqual(baseline, snapshotIncludedTables(t, ctx, restored)) {
		t.Fatal("import provenance restore drifted")
	}
	if snapshotTable(t, ctx, restored, "radishnexus.activity_items") != "[]" {
		t.Fatal("backup restored derived projection")
	}
	if _, e = goldenpostgres.New(restored).RebuildActivityProjection(ctx); e != nil {
		t.Fatal(e)
	}
	if activity != snapshotTable(t, ctx, restored, "radishnexus.activity_items") {
		t.Fatal("rebuilt import projection drifted")
	}
	auth := authn.NewService(authpostgres.New(restored), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	fresh, e := auth.Login(ctx, authn.LoginInput{Email: "context-backup@example.test", Password: password})
	if e != nil {
		t.Fatal(e)
	}
	restoredCore := goldenpath.NewService(goldenpostgres.New(restored), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	retried, e := restoredCore.RejectDecision(ctx, currentInvocation, rejectInput)
	if e != nil || retried.Rejected || retried.Decision.RejectionReason != rejectInput.RejectionReason {
		t.Fatal("restored rejection receipt", e)
	}
	replayed, e := restoredCore.SupersedeDecision(ctx, currentInvocation, supersedeInput)
	if e != nil || !reflect.DeepEqual(replayed, superseded) {
		t.Fatal("restored native supersession receipt", e)
	}
	restoredReview, applied, err := restoredCore.ReviewDecision(ctx, currentInvocation, reviewInput)
	if err != nil || applied || !reflect.DeepEqual(restoredReview, review) {
		t.Fatal("restored native review receipt", err)
	}
	restoredDates := goldenpath.NewTicketDueDateService(goldenpostgres.New(restored), goldenpath.CryptoIDGenerator{})
	currentDay, e := restoredDates.ReadTicketDueDate(ctx, actor, importedTicket)
	if e != nil || currentDay.DueDateRevision != 3 || currentDay.DueDate != nil {
		t.Fatal("restored due date", currentDay, e)
	}
	dateReceipt, e := restoredDates.SetTicketDueDate(ctx, currentInvocation, nativeDate)
	if e != nil || !reflect.DeepEqual(dateReceipt, cleared) {
		t.Fatal("restored due date receipt", dateReceipt, e)
	}
	view, e := restoredCore.GetNexusView(ctx, actor, entityref.Ref{Type: "ticket", ID: importedExecutionTicket})
	if e != nil || view.TicketMessageSource == nil || view.TicketMessageSource.MessageID != importedMessage {
		t.Fatal("restored imported Message Ticket", e)
	}
	execution = goldenpath.NewTicketExecutionService(goldenpostgres.New(restored), goldenpath.CryptoIDGenerator{})
	executionReceipt, e := execution.Write(ctx, currentInvocation, goldenpath.TicketFromMessage, nativeExecutionInput)
	if e != nil || executionReceipt.Created || executionReceipt.Ticket.ID != nativeExecution.Ticket.ID {
		t.Fatal("restored Message Ticket receipt", e)
	}
	linkReceipt, e := execution.Write(ctx, currentInvocation, goldenpath.TicketDiscussionLink, nativeLinkInput)
	if e != nil || linkReceipt.Created || linkReceipt.LinkID != nativeLink.LinkID {
		t.Fatal("restored discussion link receipt", e)
	}
	removedReceipt, e := execution.Write(ctx, currentInvocation, goldenpath.TicketDiscussionUnlink, nativeUnlinkInput)
	if e != nil || removedReceipt.LinkID != nativeLink.LinkID {
		t.Fatal("restored discussion unlink receipt", e)
	}

	classification = goldenpath.NewTicketClassificationService(goldenpostgres.New(restored), goldenpath.CryptoIDGenerator{})
	rev, e := classification.Set(ctx, currentInvocation, classInput)
	if e != nil || rev != 2 {
		t.Fatal("restored classification receipt", rev, e)
	}
	labelReceipt, e := classification.WriteLabel(ctx, currentInvocation, labelInput)
	if e != nil || labelReceipt != nativeLabel {
		t.Fatal("restored label creation receipt", labelReceipt, e)
	}
	labelReceipt, e = classification.WriteLabel(ctx, currentInvocation, archiveInput)
	if e != nil || labelReceipt.Status != "archived" || labelReceipt.Ref.ID != nativeLabel.Ref.ID || labelReceipt.Revision != 2 {
		t.Fatal("restored label archive receipt", labelReceipt, e)
	}
	discovery := goldenpath.NewDiscoveryService(goldenpostgres.New(restored))
	filtered, e := discovery.ListProjectTickets(ctx, actor, result.ProjectID, goldenpath.DiscoveryPageInput{Limit: 25, IncludeClassification: true, TicketFilters: goldenpath.TicketFilters{Type: "defect", Labels: nativeLabel.Ref.ID}})
	if e != nil || len(filtered.Items) != 1 || filtered.Items[0].Ref.ID != nativeExecution.Ticket.ID ||
		filtered.Items[0].Classification.Labels[0].Status != "archived" {
		t.Fatal("restored classified discovery", filtered, e)
	}

	trace = goldenpath.NewTicketCIRunService(goldenpostgres.New(restored), goldenpath.CryptoIDGenerator{})
	for _, test := range []struct {
		kind  string
		input goldenpath.TicketCIRunInput
		want  string
	}{
		{goldenpath.TicketCIRunLink, traceInput, firstTrace.LinkID},
		{goldenpath.TicketCIRunUnlink, removeTrace, firstTrace.LinkID},
		{goldenpath.TicketCIRunLink, newTrace, secondTrace.LinkID},
	} {
		retry, err := trace.Write(ctx, currentInvocation, test.kind, test.input)
		if err != nil || retry.Created || retry.LinkID != test.want {
			t.Fatal(retry, err)
		}
	}
	listInput := goldenpath.CIRunTraceInput{DiscoveryPageInput: goldenpath.DiscoveryPageInput{Limit: 25}}
	history, e := trace.List(ctx, actor, goldenpath.TraceTicketRunHistory, traceInput.TicketID, listInput)
	if e != nil || len(history.Items) != 3 || history.Items[0].Link.ID != secondTrace.LinkID || history.Items[1].RelationState != "removed" {
		t.Fatal(history, e)
	}
	deployments, e := trace.List(ctx, actor, goldenpath.TraceRunDeployments, run.CIRun.ID, listInput)
	if e != nil || len(deployments.Items) != 1 || deployments.Items[0].Ref.ID != staged.ID || deployments.Items[0].Status != "failed" {
		t.Fatal(deployments, e)
	}

	deliveryReader = goldenpath.NewThreadDeliveryService(goldenpostgres.New(restored))
	for i, path := range deliveryPaths {
		page, err := deliveryReader.List(ctx, actor, path, deliveryInput)
		if err != nil || !reflect.DeepEqual(page, deliveryBaseline[i]) {
			t.Fatalf("restored Thread delivery page %d changed: %v (%v)", i, page, err)
		}
	}

	if nexusAfter := readNexusRelations(restored); !reflect.DeepEqual(nexusBefore, nexusAfter) {
		t.Fatal("Nexus View summaries, relation pages, or ordering changed after restore")
	}

	restoredContext := goldenpath.NewContextService(goldenpostgres.NewContextStore(restored, schema))
	status, e := restoredContext.Result(ctx, actor, fresh.Token, plan.OperationID)
	if e != nil || !reflect.DeepEqual(status, result) {
		t.Fatal("restored import receipt changed", e)
	}
	if _, e = restored.Exec(ctx, `UPDATE radishnexus.user_accounts SET status='active' WHERE user_id IN (SELECT target_id FROM radishnexus.context_import_mappings WHERE kind='actor')`); e == nil {
		t.Fatal("restored historical actor can activate")
	}
}
