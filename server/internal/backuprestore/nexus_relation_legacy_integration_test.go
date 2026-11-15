//go:build integration

package backuprestore

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func TestBackupRestoreNexusRelationsLegacy(t *testing.T) {
	testBackupRestoreLegacyWithFixture(t, "RADISHNEXUS_NEXUS_RELATIONS_LEGACY_BIN", 29, func(ctx context.Context, source *pgxpool.Pool, owner authz.Principal) func(*pgxpool.Pool, authz.Principal) {
		refs := seedLegacyNexusRelations(t, ctx, source, owner)
		tables := []string{
			"radishnexus.entity_links", "radishnexus.domain_events", "radishnexus.collaboration_command_receipts",
			"radishnexus.outbox_deliveries", "radishnexus.activity_items",
		}
		before := make(map[string]string, len(tables))
		for _, table := range tables {
			before[table] = snapshotTable(t, ctx, source, table)
		}
		read := func(pool *pgxpool.Pool, p authz.Principal, ref entityref.Ref) (goldenpath.NexusView, []goldenpath.NexusRelationPage) {
			t.Helper()
			reader := goldenpostgres.New(pool)
			summary, err := reader.GetNexusSummary(ctx, p, ref)
			if err != nil {
				t.Fatal(ref.Type, err)
			}
			first, err := reader.ListNexusRelations(ctx, p, ref, goldenpath.NexusRelationPageInput{Limit: 1})
			if err != nil || len(first.Items) != 1 || first.Next == nil {
				t.Fatal("missing first page", ref.Type, err)
			}
			second, err := reader.ListNexusRelations(ctx, p, ref, goldenpath.NexusRelationPageInput{Limit: 1, After: first.Next})
			if err != nil || len(second.Items) != 1 || second.Next != nil || first.Items[0].Target == second.Items[0].Target {
				t.Fatal("missing second page", ref.Type, err)
			}
			return summary, []goldenpath.NexusRelationPage{first, second}
		}
		return func(restored *pgxpool.Pool, p authz.Principal) {
			var valid bool
			if err := restored.QueryRow(ctx, `
				SELECT EXISTS(SELECT 1 FROM pg_index
				WHERE indexrelid=to_regclass('radishnexus.entity_links_nexus_incoming_page') AND indisvalid AND indisready)
			`).Scan(&valid); err != nil || !valid {
				t.Fatal("upgraded index is missing or invalid", err)
			}
			for _, table := range tables {
				if before[table] != snapshotTable(t, ctx, restored, table) || before[table] != snapshotTable(t, ctx, source, table) {
					t.Fatal("upgrade or restore changed authoritative rows or projection", table)
				}
			}
			for _, ref := range refs {
				oldSummary, oldPages := read(source, owner, ref)
				newSummary, newPages := read(restored, p, ref)
				if len(oldSummary.Relations) != 1 || !reflect.DeepEqual(oldSummary, newSummary) || !reflect.DeepEqual(oldPages, newPages) {
					t.Fatal("restored and upgraded relation view differs", ref.Type)
				}
			}
		}
	})
}

func seedLegacyNexusRelations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, owner authz.Principal) []entityref.Ref {
	t.Helper()
	store := goldenpostgres.New(pool)
	ids, now := goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{}
	config := goldenpath.NewConfigurationService(store, ids, now)
	core := goldenpath.NewService(store, ids, now)
	inv := goldenpath.Invocation{Principal: owner, SourceKind: "web", CorrelationID: "cor_nexus_029_fixture"}
	configure := func(input goldenpath.ConfigurationInput) string {
		t.Helper()
		result, err := config.Configure(ctx, inv, input)
		if err != nil {
			t.Fatal(err)
		}
		return result.Object.ID
	}
	team := configure(goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: owner.WorkspaceID, Name: "Synthetic old team", ClientOperationID: "nexus-029-team"})
	project := configure(goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: owner.WorkspaceID, Name: "Synthetic old project", Key: "legacy-nexus", OwnerTeamID: team, Visibility: "restricted", InitialAdminUserID: owner.ID, ClientOperationID: "nexus-029-project"})
	channel := configure(goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: project, Name: "Synthetic old channel", Visibility: "project", MemberUserIDs: []string{}, ClientOperationID: "nexus-029-channel"})
	message, err := core.CreateMessage(ctx, inv, goldenpath.CreateMessageInput{ChannelID: channel, Body: "Synthetic 029 source", ClientOperationID: "nexus-029-message"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := core.StartThreadFromMessage(ctx, inv, goldenpath.StartThreadFromMessageInput{ChannelID: channel, MessageID: message.Message.ID, Title: "Synthetic old Thread", Visibility: "project"})
	if err != nil {
		t.Fatal(err)
	}
	var decisionID string
	for i := range 2 {
		op := fmt.Sprintf("nexus-029-decision-%d", i)
		proposed, err := core.CreateDecisionFromThread(ctx, inv, goldenpath.CreateDecisionInput{ThreadID: thread.ID, Question: "Synthetic " + op, ClientOperationID: op})
		if err != nil {
			t.Fatal(err)
		}
		decisionID = proposed.Decision.ID
	}
	if _, err := core.AcceptDecision(ctx, inv, goldenpath.AcceptDecisionInput{DecisionID: decisionID, Outcome: "Accepted on 029", Rationale: "Synthetic evidence", ClientOperationID: "nexus-029-accept"}); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		op := fmt.Sprintf("nexus-029-ticket-%d", i)
		if _, err := core.CreateTicketFromDecision(ctx, inv, goldenpath.CreateTicketInput{DecisionID: decisionID, Title: "Synthetic " + op, ClientOperationID: op}); err != nil {
			t.Fatal(err)
		}
	}
	return []entityref.Ref{{Type: "thread", ID: thread.ID}, {Type: "decision", ID: decisionID}}
}
