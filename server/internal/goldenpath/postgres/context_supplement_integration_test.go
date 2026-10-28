//go:build integration

package postgres_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func TestContextRemovedRelationsRoundTrip(t *testing.T) {
	source := contextDatabase(t)
	source.seed(t)
	b := source.bundle(t)
	d, _, err := cp.Unpack(b)
	if err != nil {
		t.Fatal(err)
	}
	// The current native API cannot remove these relation kinds. Model an
	// explicit historical package claim, then use real import/export transactions;
	// never SQL-update the target graph or claim native removal UI coverage.
	removedAt := b.Manifest.CreatedAt
	reason := "Synthetic historical removal"
	removed := map[string]cp.Link{}
	for i := range d.Links {
		link := &d.Links[i]
		if link.Relation == "started-from" || link.Relation == "derived-from" {
			continue
		}
		actor := source.member.ID
		link.State = "removed"
		link.RemovedActor = &actor
		link.RemovedAt = &removedAt
		link.RemovalReason = &reason
		link.UpdatedAt = removedAt
		removed[link.ID] = *link
	}
	b, err = cp.Pack(b.Manifest, d)
	if err != nil {
		t.Fatal(err)
	}
	first := contextDatabase(t)
	p := first.plan(t, b)
	if _, err = first.service.Apply(first.ctx, first.principal, first.owner.Token, b, p, p.PlanDigest); err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) {
			t.Fatalf("import constraint: code=%s constraint=%s message=%s", pgerr.Code, pgerr.ConstraintName, pgerr.Message)
		}
		t.Fatal(err)
	}
	first.project = contextMapping(t, p, "project", source.project)
	exported := first.bundle(t)
	again, _, err := cp.Unpack(exported)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, link := range again.Links {
		for id, original := range removed {
			if link.ID != contextMapping(t, p, "entity-link", id) {
				continue
			}
			found++
			if link.State != "removed" || link.Origin != "import" || link.RemovedActor == nil || *link.RemovedActor != contextMapping(t, p, "actor", *original.RemovedActor) || link.RemovedAt == nil || !link.RemovedAt.Equal(*original.RemovedAt) || link.RemovalReason == nil || *link.RemovalReason != reason || !link.CreatedAt.Equal(original.CreatedAt) || !link.UpdatedAt.Equal(original.UpdatedAt) || link.Actor != contextMapping(t, p, "actor", original.Actor) || link.EventID != contextMapping(t, p, "event", original.EventID) || len(link.Lineage) != 1 {
				t.Fatal("removed relation lost source/history", link.Relation)
			}
		}
	}
	if found != len(removed) {
		t.Fatal("removed relations lost", found)
	}
	second := contextDatabase(t)
	q := second.plan(t, exported)
	if _, err = second.service.Apply(second.ctx, second.principal, second.owner.Token, exported, q, q.PlanDigest); err != nil {
		t.Fatal(err)
	}
	second.project = contextMapping(t, q, "project", first.project)
	third, _, err := cp.Unpack(second.bundle(t))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, link := range third.Links {
		if link.State == "removed" {
			n++
			if link.RemovedAt == nil || !link.RemovedAt.Equal(removedAt) || link.RemovalReason == nil || *link.RemovalReason != reason || len(link.Lineage) != 2 {
				t.Fatal("second hop lost removal")
			}
		}
	}
	if n != len(removed) {
		t.Fatal("second hop reactivated/lost relations")
	}
	ticket := contextMapping(t, q, "ticket", contextMapping(t, p, "ticket", source.ticket))
	view, err := second.core.GetNexusView(second.ctx, second.principal, entityref.Ref{Type: "ticket", ID: ticket})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Relations) != 0 {
		t.Fatal("removed relations exposed as active", view.Relations)
	}
	if contextCount(t, first, "outbox_deliveries") != 0 || contextCount(t, second, "outbox_deliveries") != 0 {
		t.Fatal("historical import emitted deliveries")
	}
}

func TestContextRepresentativeCapacity(t *testing.T) {
	source := contextDatabase(t)
	source.seed(t)
	start := time.Now()
	for n := 0; n < 1000; n++ {
		_, err := source.core.CreateMessage(source.ctx, contextInvocation(source.member), goldenpath.CreateMessageInput{ChannelID: source.channel, ClientOperationID: fmt.Sprintf("capacity-message-%d", n), Body: strings.Repeat("Synthetic context. ", 60)})
		if err != nil {
			t.Fatal(err)
		}
	}
	creation := time.Since(start)
	start = time.Now()
	b := source.bundle(t)
	exportDuration := time.Since(start)
	d, _, err := cp.Unpack(b)
	if err != nil {
		t.Fatal(err)
	}
	size := len(cp.Encode(b.Manifest))
	for _, f := range b.Files {
		size += len(f.Content)
	}
	target := contextDatabase(t)
	start = time.Now()
	p := target.plan(t, b)
	prepareDuration := time.Since(start)
	start = time.Now()
	result, err := target.service.Apply(target.ctx, target.principal, target.owner.Token, b, p, p.PlanDigest)
	applyDuration := time.Since(start)
	if err != nil || result.Status != "applied" {
		t.Fatal("capacity apply", err)
	}
	start = time.Now()
	if contextCount(t, target, "messages") != 1001 || contextCount(t, target, "context_import_batches") != 1 || contextCount(t, target, "outbox_deliveries") != 0 {
		t.Fatal("capacity counts")
	}
	replay, err := target.service.Apply(target.ctx, target.principal, target.owner.Token, b, p, p.PlanDigest)
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("capacity replay", err)
	}
	var before, after string
	if err = target.pool.QueryRow(target.ctx, `SELECT jsonb_agg(to_jsonb(a) ORDER BY event_id)::text FROM radishnexus.activity_items a`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	projectionStart := time.Now()
	if _, err = target.store.RebuildActivityProjection(target.ctx); err != nil {
		t.Fatal(err)
	}
	projectionDuration := time.Since(projectionStart)
	if err = target.pool.QueryRow(target.ctx, `SELECT jsonb_agg(to_jsonb(a) ORDER BY event_id)::text FROM radishnexus.activity_items a`).Scan(&after); err != nil || before != after {
		t.Fatal("capacity projection drift", err)
	}
	target.project = contextMapping(t, p, "project", source.project)
	readback, _, err := cp.Unpack(target.bundle(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(readback.Objects) != len(d.Objects) || len(readback.History) != len(d.History) {
		t.Fatal("capacity reexport lost records")
	}
	t.Logf("representative: bytes=%d objects=%d actors=%d history=%d links=%d revisions=%d native_creation=%s export=%s prepare=%s apply_including_projection=%s rebuild=%s verify_including_replay_reexport=%s", size, len(d.Objects), len(d.Actors), len(d.History), len(d.Links), len(d.Revisions), creation, exportDuration, prepareDuration, applyDuration, projectionDuration, time.Since(start))
}
