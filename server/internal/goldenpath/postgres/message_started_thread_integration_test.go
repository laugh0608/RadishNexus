//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func TestMessageStartedThreadCurrentAccess(t *testing.T) {
	f := newMembershipFixture(t)
	member := f.principalFor(f.members[0])
	lookup := func(actor authz.Principal, channel, message string, want *goldenpath.SubjectProjection, wantErr error) {
		t.Helper()
		got, err := f.core.GetMessageStartedThread(f.ctx, actor, channel, message)
		if !errors.Is(err, wantErr) || (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Fatalf("lookup(%s, %s, %s) = %#v, %v; want %#v, %v", actor.ID, channel, message, got, err, want, wantErr)
		}
	}
	parent := &goldenpath.SubjectProjection{State: goldenpath.ProjectionVisible,
		Ref: entityref.Ref{Type: "thread", ID: f.thread}, Title: "Private responsibility"}
	lookup(f.principal, f.channel, f.message, parent, nil)
	lookup(f.principal, "chn_elsewhere", f.message, nil, authz.ErrNotFound)
	lookup(f.principal, f.channel, "msg_missing", nil, authz.ErrNotFound)
	foreign := f.principal
	foreign.WorkspaceID = "wrk_elsewhere"
	lookup(foreign, f.channel, f.message, nil, authz.ErrNotFound)
	lookup(member, f.channel, f.message, nil, authz.ErrNotFound)

	// A reply belongs to its parent and can independently start a different Thread.
	reply := deliveryMessage(t, f.contextFixture, f.channel, f.thread, "nested-source")
	lookup(f.principal, f.channel, reply, nil, nil)
	child, err := f.core.StartThreadFromMessage(f.ctx, contextInvocation(f.principal), goldenpath.StartThreadFromMessageInput{
		ChannelID: f.channel, MessageID: reply, Title: "Nested discussion", Visibility: "restricted",
	})
	if err != nil {
		t.Fatal(err)
	}
	nested := &goldenpath.SubjectProjection{State: goldenpath.ProjectionVisible,
		Ref: entityref.Ref{Type: "thread", ID: child.ID}, Title: child.Title}
	lookup(f.principal, f.channel, reply, nested, nil)

	f.mustChange(t, f.owner, proposal("channel", f.channel, "add", member.ID, ""))
	f.mustChange(t, f.owner, proposal("project", f.project, "role", member.ID, "viewer"))
	unused := deliveryMessage(t, f.contextFixture, f.channel, "", "no-discussion")
	lookup(member, f.channel, unused, nil, nil)
	lookup(member, f.channel, f.message, nil, nil)
	f.mustChange(t, f.owner, proposal("thread", child.ID, "add", member.ID, ""))
	lookup(member, f.channel, reply, nil, authz.ErrNotFound)
	f.mustChange(t, f.owner, proposal("thread", f.thread, "add", member.ID, ""))
	lookup(member, f.channel, f.message, parent, nil)
	lookup(member, f.channel, reply, nested, nil)
	f.mustChange(t, f.owner, proposal("thread", f.thread, "remove", member.ID, ""))
	lookup(member, f.channel, f.message, nil, nil)
	lookup(member, f.channel, reply, nil, authz.ErrNotFound)
	f.mustChange(t, f.owner, proposal("thread", child.ID, "remove", member.ID, ""))
	f.mustChange(t, f.owner, proposal("channel", f.channel, "remove", member.ID, ""))
	lookup(member, f.channel, f.message, nil, authz.ErrNotFound)

	sharedChannel := f.configure(t, f.principal, goldenpath.ConfigurationInput{
		Kind: "channel.create", ScopeID: f.project, Name: "Project discussion", Visibility: "project", MemberUserIDs: []string{},
	}).Object.ID
	sharedThread, sharedMessage := deliveryDiscussion(t, f.contextFixture, sharedChannel, "shared")
	shared := &goldenpath.SubjectProjection{State: goldenpath.ProjectionVisible,
		Ref: entityref.Ref{Type: "thread", ID: sharedThread}, Title: "Synthetic delivery Thread shared"}
	lookup(member, sharedChannel, sharedMessage, shared, nil)
	f.mustChange(t, f.owner, proposal("project", f.project, "remove", member.ID, ""))
	lookup(member, sharedChannel, sharedMessage, nil, authz.ErrNotFound)
	contextSQL(t, f.contextFixture, `UPDATE radishnexus.projects SET status='archived' WHERE workspace_id=$1 AND id=$2`, f.workspace, f.project)
	lookup(f.principal, sharedChannel, sharedMessage, shared, nil)
	lookup(f.principal, f.channel, f.message, parent, nil)
}
