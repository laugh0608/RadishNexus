//go:build integration

package postgres_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/testsupport/pgfault"
)

func TestDecisionReviewHistoryPermissionsAndRoundTrip(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	inv := contextInvocation(f.principal)
	ref := entityref.Ref{Type: "decision", ID: f.decision}
	before, err := f.core.GetNexusView(f.ctx, f.principal, ref)
	if err != nil || !before.DecisionCapabilities.Review {
		t.Fatalf("review capability: %v", err)
	}
	input := goldenpath.ReviewDecisionInput{
		DecisionID: f.decision, ClientOperationID: "manual-review", BaseDecisionRevision: 2,
		Conclusion: "still_applicable", Reason: "  Constraint confirmed.  ",
	}
	original, applied, err := f.core.ReviewDecision(f.ctx, inv, input)
	if err != nil || !applied || original.Facts.Reason != "Constraint confirmed." ||
		!original.OccurredAt.After(before.Current.UpdatedAt) {
		t.Fatalf("record review: %v", err)
	}
	again, applied, err := f.core.ReviewDecision(f.ctx, inv, input)
	if err != nil || applied || !reflect.DeepEqual(original, again) {
		t.Fatalf("exact review retry: %v", err)
	}
	changed := input
	changed.Conclusion = "reconsider"
	if _, _, err := f.core.ReviewDecision(f.ctx, inv, changed); !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("changed operation: %v", err)
	}
	after, err := f.core.GetNexusView(f.ctx, f.principal, ref)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("review changed Decision or ordinary timeline: %v", err)
	}

	msg, err := f.core.CreateMessage(f.ctx, inv, goldenpath.CreateMessageInput{
		ChannelID: f.channel, ClientOperationID: "review-extra-message", Body: "Supplemental test evidence.",
	})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := f.core.StartThreadFromMessage(f.ctx, inv, goldenpath.StartThreadFromMessageInput{
		ChannelID: f.channel, MessageID: msg.Message.ID, Title: "Additional review discussion", Visibility: "restricted",
	})
	if err != nil {
		t.Fatal(err)
	}
	contextSQL(t, f, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,management_role) VALUES($1,$2,$3,'member')`, f.workspace, f.thread, f.member.ID)
	pageInput := goldenpath.DecisionReviewPageInput{Limit: 1}
	changed.AdditionalEvidence = &entityref.Ref{Type: "thread", ID: thread.ID}
	for n := 0; n < 52; n++ {
		changed.ClientOperationID = fmt.Sprintf("private-review-%d", n)
		if _, applied, err := f.core.ReviewDecision(f.ctx, inv, changed); err != nil || !applied {
			t.Fatalf("record protected review %d: %v", n, err)
		}
	}
	page, err := f.core.ListDecisionReviews(f.ctx, f.member, f.decision, pageInput)
	if err != nil || page.HasMore || len(page.Items) != 1 || page.Items[0].EventID != original.EventID {
		t.Fatalf("hidden reviews affected pagination: %v", err)
	}
	page, err = f.core.ListDecisionReviews(f.ctx, f.principal, f.decision, pageInput)
	if err != nil || !page.HasMore || len(page.Items) != 1 || page.Items[0].Facts.AdditionalEvidence == nil {
		t.Fatalf("owner review page: %v", err)
	}
	newest := page.Items[0]
	contextSQL(t, f, `DELETE FROM radishnexus.thread_memberships WHERE workspace_id=$1 AND thread_id=$2 AND user_id=$3`, f.workspace, thread.ID, f.principal.ID)
	if _, _, err := f.core.ReviewDecision(f.ctx, inv, changed); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("revoked extra evidence allowed retry: %v", err)
	}
	if _, err := f.service.Export(f.ctx, f.principal, f.owner.Token, f.project); !errors.Is(err, cp.ErrIncomplete) {
		t.Fatalf("incomplete review closure exported: %v", err)
	}
	contextSQL(t, f, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,management_role) VALUES($1,$2,$3,'manager')`, f.workspace, thread.ID, f.principal.ID)

	// A review can be later than the still-accepted Decision's updated_at.
	f.bundle(t)

	if _, err := f.core.AcceptDecision(f.ctx, inv, goldenpath.AcceptDecisionInput{
		DecisionID: f.proposed, ClientOperationID: "review-replacement", Outcome: "Revised", Rationale: "New constraints",
	}); err != nil {
		t.Fatal(err)
	}
	replaced, err := f.core.SupersedeDecision(f.ctx, inv, supersessionInput(f.decision, f.proposed, "review-supersede"))
	if err != nil || !replaced.OccurredAt.After(newest.OccurredAt) {
		t.Fatalf("replacement order: %v", err)
	}
	again, applied, err = f.core.ReviewDecision(f.ctx, inv, input)
	if err != nil || applied || !reflect.DeepEqual(original, again) {
		t.Fatalf("retry after replacement: %v", err)
	}
	input.ClientOperationID = "review-after-replacement"
	if _, _, err := f.core.ReviewDecision(f.ctx, inv, input); !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("fresh review of superseded Decision: %v", err)
	}
	if _, err := f.store.RebuildActivityProjection(f.ctx); err != nil {
		t.Fatal(err)
	}
	page, err = f.core.ListDecisionReviews(f.ctx, f.principal, f.decision, pageInput)
	if err != nil || !page.HasMore || len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0], newest) {
		t.Fatalf("rebuild changed review: %v", err)
	}

	bundle := f.bundle(t)
	if bundle.Manifest.Profile != cp.Profile {
		t.Fatal("unexpected review export profile")
	}
	decisionID := f.decision
	for round := 0; round < 2; round++ {
		target := contextDatabase(t)
		plan := target.plan(t, bundle)
		result, err := target.service.Apply(target.ctx, target.principal, target.owner.Token, bundle, plan, plan.PlanDigest)
		if err != nil {
			t.Fatalf("apply round %d: %v", round, err)
		}
		decisionID = contextMapping(t, plan, "decision", decisionID)
		page, err = target.core.ListDecisionReviews(target.ctx, target.principal, decisionID, pageInput)
		if err != nil || !page.HasMore || len(page.Items) != 1 || !page.Items[0].Imported ||
			page.Items[0].Facts.AdditionalEvidence == nil || !page.Items[0].OccurredAt.Equal(newest.OccurredAt) {
			t.Fatalf("review round %d: %v", round, err)
		}
		bundle, err = target.service.Export(target.ctx, target.principal, target.owner.Token, result.ProjectID)
		if err != nil {
			t.Fatalf("re-export round %d: %v", round, err)
		}
	}
}

func TestDecisionReviewOrderedAgainstSupersession(t *testing.T) {
	for _, reviewFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("review_first_%t", reviewFirst), func(t *testing.T) {
			f, ids := supersessionFixture(t)
			inv := contextInvocation(f.principal)
			release := contextGate(t, f, "collaboration_command_receipts")
			review := func() error {
				_, _, err := f.core.ReviewDecision(f.ctx, inv, goldenpath.ReviewDecisionInput{
					DecisionID: ids[0], ClientOperationID: "race-review", BaseDecisionRevision: 2,
					Conclusion: "reconsider", Reason: "New evidence.",
				})
				return err
			}
			supersede := func() error {
				_, err := f.core.SupersedeDecision(f.ctx, inv, supersessionInput(ids[0], ids[1], "race-supersede"))
				return err
			}
			first, second := supersede, review
			if reviewFirst {
				first, second = review, supersede
			}
			one, two := make(chan error, 1), make(chan error, 1)
			go func() { one <- first() }()
			contextWaitGate(t, f)
			go func() { two <- second() }()
			waitSupersessionDecisionLock(t, f)
			release()
			if err := <-one; err != nil {
				t.Fatal(err)
			}
			if err := <-two; (reviewFirst && err != nil) || (!reviewFirst && !errors.Is(err, authz.ErrConflict)) {
				t.Fatalf("second action: %v", err)
			}
		})
	}
}

func TestDecisionReviewAtomicFailuresAndLostCommit(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	input := goldenpath.ReviewDecisionInput{
		DecisionID: f.decision, ClientOperationID: "review-fault", BaseDecisionRevision: 2,
		Conclusion: "still_applicable", Reason: "Authoritative evidence.",
	}
	inv := contextInvocation(f.principal)
	tables := []string{"collaboration_command_receipts", "domain_events", "outbox_deliveries", "activity_items"}
	before := make(map[string]int, len(tables))
	for _, table := range tables {
		before[table] = contextCount(t, f, table)
	}
	for _, table := range tables {
		contextSQL(t, f, `CREATE FUNCTION public.review_fault() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN RAISE EXCEPTION 'isolated review failure'; END $$;
			CREATE TRIGGER review_fault BEFORE INSERT ON radishnexus.`+table+` FOR EACH STATEMENT EXECUTE FUNCTION public.review_fault()`)
		if _, _, err := f.core.ReviewDecision(f.ctx, inv, input); err == nil {
			t.Fatalf("review ignored fault at %s", table)
		}
		contextSQL(t, f, `DROP TRIGGER review_fault ON radishnexus.`+table+`; DROP FUNCTION public.review_fault()`)
		for _, check := range tables {
			if contextCount(t, f, check) != before[check] {
				t.Fatalf("partial review after %s fault: %s", table, check)
			}
		}
	}

	config := f.pool.Config().Copy()
	proxy, finish := pgfault.LoseCommit(t, config.ConnConfig)
	config.ConnConfig = proxy
	config.MaxConns = 1
	faulty, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	service := goldenpath.NewService(goldenpostgres.New(faulty), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	_, _, err = service.ReviewDecision(f.ctx, inv, input)
	faulty.Close()
	finish()
	if err == nil {
		t.Fatal("lost acknowledgement reported success")
	}
	review, applied, err := f.core.ReviewDecision(f.ctx, inv, input)
	if err != nil || applied {
		t.Fatalf("lost acknowledgement retry: %v", err)
	}
	page, err := f.core.ListDecisionReviews(f.ctx, f.principal, f.decision, goldenpath.DecisionReviewPageInput{Limit: 2})
	if err != nil || len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0], review) {
		t.Fatalf("lost acknowledgement changed review history: %v", err)
	}
	for _, table := range tables {
		if contextCount(t, f, table) != before[table]+1 {
			t.Fatalf("expected one new review fact in %s", table)
		}
	}
}
