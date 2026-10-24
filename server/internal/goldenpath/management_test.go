package goldenpath

import (
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"strings"
	"testing"
)

func TestManagementClosedCommandContract(t *testing.T) {
	p := authz.Principal{Kind: authz.PrincipalUser, ID: "usr_actor", WorkspaceID: "wrk_main"}
	base := ManagementProposal{ScopeKind: "project", ScopeID: "prj_main", Action: "role", TargetUserID: "usr_member", Role: "admin"}
	if e := ValidateManagementProposal(p, base); e != nil {
		t.Fatal(e)
	}
	for _, in := range []ManagementProposal{
		{ScopeKind: "workspace", ScopeID: "wrk_other", Action: "suspend", TargetUserID: "usr_member"},
		{ScopeKind: "workspace", ScopeID: "wrk_main", Action: "role", TargetUserID: "usr_member", Role: "owner"},
		{ScopeKind: "project", ScopeID: "prj_main", Action: "add", TargetUserID: "usr_member", Role: "admin"},
		{ScopeKind: "thread", ScopeID: "thr_main", Action: "role", TargetUserID: "usr_member", Role: "admin"},
		{ScopeKind: "channel", ScopeID: "chn_main", Action: "leave", TargetUserID: "usr_member"},
		{ScopeKind: "channel", ScopeID: "chn_main", Action: "handoff", TargetUserID: "usr_member", ActorAfterRole: "admin"},
		{ScopeKind: "workspace", ScopeID: "wrk_main", Action: "suspend", TargetUserID: "usr_actor"},
	} {
		if e := ValidateManagementProposal(p, in); e == nil {
			t.Fatalf("accepted unsupported proposal %+v", in)
		}
	}
	input := ManagementInput{ManagementProposal: base, ClientOperationID: "12345678-1234-4234-8234-123456789abc", PlanDigest: strings.Repeat("a", 64), Confirmed: true}
	if e := ValidateManagementInput(p, input); e != nil {
		t.Fatal(e)
	}
	for _, digest := range []string{"", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		in := input
		in.PlanDigest = digest
		if e := ValidateManagementInput(p, in); e == nil {
			t.Fatal("invalid digest accepted")
		}
	}
	input.Confirmed = false
	if e := ValidateManagementInput(p, input); e == nil {
		t.Fatal("missing confirmation accepted")
	}
}
func TestManagementPlanBindsWorkspaceAndGeneration(t *testing.T) {
	p := ManagementPlan{ManagementProposal: ManagementProposal{ScopeKind: "project", ScopeID: "prj_main", Action: "role", TargetUserID: "usr_member", Role: "admin"}, ScopeRevision: "1", ActorLifecycleRevision: "1"}
	digest, e := p.SHA256("wrk_main")
	if e != nil {
		t.Fatal(e)
	}
	p.Digest = "previous"
	same, _ := p.SHA256("wrk_main")
	if same != digest {
		t.Fatal("digest recursively included itself")
	}
	different, _ := p.SHA256("wrk_other")
	if different == digest {
		t.Fatal("workspace not bound")
	}
	p.ActorLifecycleRevision = "2"
	different, _ = p.SHA256("wrk_main")
	if different == digest {
		t.Fatal("lifecycle not bound")
	}
}
