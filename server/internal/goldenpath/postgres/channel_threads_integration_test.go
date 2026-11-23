//go:build integration

package postgres_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestChannelThreadsCurrentAccessAndPagination(t *testing.T) {
	f := newMembershipFixture(t)
	service := goldenpath.NewDiscoveryService(f.store)
	reader := f.principalFor(f.members[0])
	list := func(channel, after string, limit int, wantErr error, next string, ids ...string) {
		t.Helper()
		page, err := service.ListChannelThreads(f.ctx, reader, channel, goldenpath.DiscoveryPageInput{AfterID: after, Limit: limit})
		if !errors.Is(err, wantErr) || page.NextID != next || len(page.Items) != len(ids) {
			t.Fatalf("list(%s,%s,%d) = %+v, %v; expected %v, cursor %q, error %v", channel, after, limit, page, err, ids, next, wantErr)
		}
		for i, id := range ids {
			row := page.Items[i]
			if row.Ref.Type != "thread" || row.Ref.ID != id || row.Title == "" {
				t.Fatalf("row %d = %+v; want %s", i, row, id)
			}
		}
	}
	start := func(parent, op string) string {
		t.Helper()
		message := deliveryMessage(t, f.contextFixture, f.channel, parent, op)
		core := goldenpath.NewService(f.store, &fixedIDs{values: []string{"thr_" + op, "lnk_" + op, "evt_" + op}}, goldenpath.SystemClock{})
		target, err := core.StartThreadFromMessage(f.ctx, contextInvocation(f.principal), goldenpath.StartThreadFromMessageInput{
			ChannelID: f.channel, MessageID: message, Title: op, Visibility: "restricted",
		})
		if err != nil {
			t.Fatal(err)
		}
		return target.ID
	}
	change := func(kind, id, action string) {
		t.Helper()
		f.mustChange(t, f.owner, proposal(kind, id, action, reader.ID, ""))
	}

	list(f.channel, "", 1, authz.ErrNotFound, "")
	change("channel", f.channel, "add")
	f.mustChange(t, f.owner, proposal("project", f.project, "role", reader.ID, "viewer"))
	child := start(f.thread, "private-reply-source")
	change("thread", child, "add")
	list(f.channel, "", 1, nil, "")
	change("thread", f.thread, "add")
	family := []string{f.thread, child}
	slices.Sort(family)
	list(f.channel, "", 2, nil, "", family...)
	change("thread", child, "remove")
	list(f.channel, "", 1, nil, "", f.thread)
	change("thread", f.thread, "remove")

	// Place unreadable targets before, between and after the two readable rows.
	ids := make([]string, 5)
	for i, suffix := range []string{"a", "Z", "B", "b", "z"} {
		ids[i] = start("", "page_"+suffix)
	}
	slices.Sort(ids)
	for _, i := range []int{1, 3} {
		change("thread", ids[i], "add")
	}
	list(f.channel, "", 1, nil, ids[1], ids[1])
	list(f.channel, ids[1], 1, nil, "", ids[3])
	change("thread", ids[1], "remove")
	list(f.channel, ids[1], 1, nil, "", ids[3]) // revoked boundary remains usable
	list(f.channel, "", 1, nil, "", ids[3])
	change("thread", ids[3], "remove")
	list(f.channel, ids[1], 1, nil, "")
	change("channel", f.channel, "remove")
	list(f.channel, ids[1], 1, authz.ErrNotFound, "")

	shared := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: f.project, Name: "Shared channel", Visibility: "project", MemberUserIDs: []string{}}).Object.ID
	target, _ := deliveryDiscussion(t, f.contextFixture, shared, "project-thread")
	contextSQL(t, f.contextFixture, `UPDATE radishnexus.channels SET status='archived' WHERE workspace_id=$1 AND id=$2`, f.workspace, shared)
	list(shared, "", 25, nil, "", target)
	change("project", f.project, "remove")
	list(shared, "", 25, authz.ErrNotFound, "")
	list("chn_missing", "", 25, authz.ErrNotFound, "")
}
