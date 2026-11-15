package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type dueDateApplication struct {
	input *goldenpath.TicketDueDateInput
}

func (a *dueDateApplication) ReadTicketDueDate(context.Context, authz.Principal, string) (goldenpath.TicketDueDate, error) {
	return goldenpath.TicketDueDate{DueDateRevision: 1, StatusRevision: 1, CanSetDueDate: true}, nil
}

func (a *dueDateApplication) SetTicketDueDate(_ context.Context, _ goldenpath.Invocation, in goldenpath.TicketDueDateInput) (goldenpath.TicketDueDateResult, error) {
	a.input = &in
	return goldenpath.TicketDueDateResult{Applied: true, DueDateRevision: in.BaseDueDateRevision + 1, DueDate: in.DueDate}, nil
}

func TestTicketDueDateHTTPValidationAndNull(t *testing.T) {
	app := &dueDateApplication{}
	policy, _ := NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := NewTrustedProxyPolicy("10.0.0.0/8")
	handler := WithRequestID(NewTicketDueDateHandler(validMessagingSessions(), app, policy, proxy))
	const path = "/api/v1/workspaces/wrk_main/tickets/tkt_one/due-date"
	const body = `{"client_operation_id":"due","base_due_date_revision":2,"base_status_revision":1,"due_date":null,"confirmed":true}`
	request := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, messagingWriteRequest(method, path, body))
		return w
	}
	if w := request("GET", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"due_date":null`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, invalid := range []string{`""`, `"0000-01-01"`, `"2031-02-29"`, `"2032-2-29"`, `"2032-02-29T00:00:00Z"`, `42`} {
		w := request("POST", strings.Replace(body, "null", invalid, 1))
		if w.Code != 400 || app.input != nil {
			t.Fatal(invalid, w.Code, w.Body.String())
		}
	}
	if w := request("POST", strings.Replace(body, `"due_date":null,`, "", 1)); w.Code != 400 || app.input != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", body); w.Code != 200 || app.input == nil || app.input.DueDate != nil || !strings.Contains(w.Body.String(), `"applied_due_date":null`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", strings.Replace(body, "null", `"2032-02-29"`, 1)); w.Code != 200 || app.input.DueDate == nil || *app.input.DueDate != "2032-02-29" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDueDateKeepsDefaultDiscoveryAndTimelineShape(t *testing.T) {
	ticket := entityref.Ref{Type: "ticket", ID: "tkt_one"}
	const scope = "/api/v1/workspaces/wrk_main"
	projectList := scope + "/projects/prj_one/tickets"
	reader := &fakeDiscoveryReader{page: goldenpath.DiscoveryPage{Items: []goldenpath.DiscoveryItem{
		{Ref: ticket, Title: "Work", Status: "open", Priority: "unset"},
	}}}
	get := func(handler http.Handler, path string, status int) string {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, messagingRequest("GET", path, ""))
		if w.Code != status {
			t.Fatal(path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	discovery := discoveryHandlerForTest(t, validMessagingSessions(), reader)
	for _, list := range []string{projectList, scope + "/my-tickets"} {
		if list != projectList {
			reader.page.Items[0].Project = &goldenpath.DiscoveryProject{ID: "prj_one", Name: "Project"}
		}
		if body := get(discovery, list, 200); strings.Contains(body, "due_date") || reader.input.IncludeDueDate {
			t.Fatal(body)
		}
		if body := get(discovery, list+"?include=due_date", 200); !strings.Contains(body, `"due_date":null`) || !reader.input.IncludeDueDate {
			t.Fatal(body)
		}
		get(discovery, list+"?include=due_date&include=due_date", 400)
	}

	now := time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC)
	actor := goldenpath.ActorRef{Kind: "user", ID: "usr_reader"}
	application := &fakeCollaborationApplication{view: goldenpath.NexusView{
		Current: goldenpath.CurrentProjection{
			Ref: ticket, GoverningProjectID: "prj_one", Title: "Work", Status: "open", StatusRevision: 1,
			CreatedAt: now, UpdatedAt: now.Add(time.Second), CreatedBy: actor,
		},
		TicketCapabilities: &goldenpath.TicketCapabilities{ChangeStatus: true},
		Relations: []goldenpath.RelationProjection{{
			State: goldenpath.ProjectionVisible, Direction: "outgoing", RelationType: "implements",
			Target: entityref.Ref{Type: "decision", ID: "dec_one"}, Title: "Work decision",
		}},
		Timeline: []goldenpath.TimelineItem{{
			EventID: "evt_due", ActivityType: goldenpath.TicketDueDateChanged,
			Actor: actor, OccurredAt: now.Add(time.Second), ProjectionVersion: goldenpath.ActivityProjectionVersion,
			SafeFacts: map[string]string{"from": "", "to": "2032-02-29", "from_revision": "1", "to_revision": "2"},
		}},
	}}
	handler := testCollaborationHandler(t, validMessagingSessions(), application)
	url := scope + "/tickets/tkt_one/nexus-view"
	if body := get(handler, url, 200); !strings.Contains(body, `"timeline":[]`) || strings.Contains(body, "due_date") {
		t.Fatal(body)
	}
	if body := get(handler, url+"?include=due_date", 200); !strings.Contains(body, `"due_date":{"from":null,"to":"2032-02-29","from_revision":1,"to_revision":2}`) {
		t.Fatal(body)
	}
	get(handler, url+"?include=due_date&include=due_date", 400)
	get(handler, scope+"/decisions/dec_one/nexus-view?include=due_date", 400)
}
