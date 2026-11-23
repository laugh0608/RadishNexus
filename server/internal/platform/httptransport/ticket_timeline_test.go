package httptransport

import (
	"errors"
	"net/url"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestTicketTimelineCursorScope(t *testing.T) {
	scope := discoveryCursor{Version: 1, WorkspaceID: "wrk_one", ActorID: "usr_one", TicketID: "tkt_one", Kind: ticketTimelineKind}
	input, bound, err := parseTicketTimelineQuery("include=execution_context", scope)
	if err != nil || input.Limit != 25 || !input.Execution {
		t.Fatal("first page", input, err)
	}
	cursor, err := nextDiscoveryCursor(bound, "evt_visible")
	if err != nil {
		t.Fatal(err)
	}
	raw := "include=execution_context&limit=50&after=" + url.QueryEscape(*cursor)
	input, _, err = parseTicketTimelineQuery(raw, scope)
	if err != nil || input.AfterEventID != "evt_visible" || input.Limit != 50 {
		t.Fatal("matching continuation", input, err)
	}
	foreign := scope
	foreign.WorkspaceID = "wrk_other"
	if _, _, err = parseTicketTimelineQuery(raw, foreign); !errors.Is(err, authz.ErrInvalid) {
		t.Fatal("cursor accepted in another Workspace", err)
	}
}
