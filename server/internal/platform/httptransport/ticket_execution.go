package httptransport

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const (
	executionBase        = "/api/v1/workspaces/{workspace_id}"
	executionMessage     = executionBase + "/channels/{channel_id}/messages/{message_id}"
	messageTickets       = executionMessage + "/tickets"
	threadReplies        = executionBase + "/threads/{thread_id}/messages"
	threadTickets        = executionBase + "/threads/{thread_id}/tickets"
	ticketDiscussions    = executionBase + "/tickets/{ticket_id}/discussions"
	ticketDiscussionLink = executionBase + "/tickets/{ticket_id}/discussion-links/{link_id}"
)

var executionRoutes = map[string]string{
	executionMessage:     "GET",
	messageTickets:       "GET, POST",
	threadReplies:        "GET",
	threadTickets:        "GET",
	ticketDiscussions:    "GET, POST",
	ticketDiscussionLink: "DELETE",
}

func RegisterTicketExecutionRoutes(mux *http.ServeMux, handler http.Handler) {
	for path := range executionRoutes {
		mux.Handle(path, handler)
	}
}

func NewTicketExecutionHandler(sessions MessagingSessionService, app *goldenpath.TicketExecutionService, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	handle := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Allow", executionRoutes[strings.TrimPrefix(r.Pattern, "GET ")])
			writeIdentityError(w, r, ErrMethodNotAllowed)
			return
		}
		p, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), r.Method != "GET", sessions, session, proxy)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		if p.WorkspaceID != r.PathValue("workspace_id") {
			writeIdentityError(w, r, errors.New("Ticket execution identity scope mismatch"))
			return
		}
		input := goldenpath.TicketExecutionInput{ChannelID: r.PathValue("channel_id"), MessageID: r.PathValue("message_id"), TicketID: r.PathValue("ticket_id"), ThreadID: r.PathValue("thread_id"), LinkID: r.PathValue("link_id")}
		source := entityref.Ref{Type: "ticket", ID: input.TicketID}
		if input.MessageID != "" {
			if !validScopedID(input.ChannelID, "chn_") {
				writeIdentityError(w, r, authz.ErrInvalid)
				return
			}
			source = entityref.Ref{Type: "message", ID: input.MessageID}
		}
		if input.ThreadID != "" {
			source = entityref.Ref{Type: "thread", ID: input.ThreadID}
		}
		if entityref.M0Registry().Validate(source) != nil {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		var data any
		status := http.StatusOK
		switch {
		case r.Method != "GET":
			data, status, err = writeExecutionCommand(w, r, p, app, input)
		case r.Pattern == "GET "+executionMessage:
			if r.URL.RawQuery != "" {
				err = authz.ErrInvalid
				break
			}
			var m goldenpath.Message
			m, err = app.ReadExecutionMessage(r.Context(), p, input.ChannelID, input.MessageID)
			if err == nil {
				data, err = publicMessageProjection(input.ChannelID, goldenpath.MessageProjection{ID: m.ID, ChannelID: m.ChannelID, ThreadID: m.ThreadID, AuthorID: m.AuthorID, Body: m.Body, CreatedAt: m.CreatedAt})
			}
		case r.Pattern == "GET "+threadReplies:
			data, err = readExecutionReplies(r, p, app, source.ID)
		default:
			data, err = readExecutionList(r, p, app, source, input.ChannelID)
		}
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		writeIdentityJSON(w, r, status, map[string]any{"data": data})
	}
	for path, methods := range executionRoutes {
		for _, method := range strings.Split(methods, ", ") {
			mux.Handle(method+" "+path, privateNoStore(http.HandlerFunc(handle)))
		}
		mux.Handle(path, privateNoStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", methods)
			writeIdentityError(w, r, ErrMethodNotAllowed)
		})))
	}
	return mux
}

func writeExecutionCommand(w http.ResponseWriter, r *http.Request, p authz.Principal, app *goldenpath.TicketExecutionService, in goldenpath.TicketExecutionInput) (any, int, error) {
	if r.URL.RawQuery != "" {
		return nil, 0, authz.ErrInvalid
	}
	fields := []string{"client_operation_id", "confirmed"}
	kind := goldenpath.TicketDiscussionUnlink
	switch {
	case in.MessageID != "":
		kind = goldenpath.TicketFromMessage
		fields = append(fields, "title")
	case r.Method == "POST":
		kind = goldenpath.TicketDiscussionLink
		fields = append(fields, "thread_id")
	}
	raw, err := decodeStrictObject(w, r, fields, 8*1024)
	if err != nil {
		return nil, 0, err
	}
	for name, target := range map[string]any{"client_operation_id": &in.ClientOperationID, "confirmed": &in.Confirmed, "title": &in.Title, "thread_id": &in.ThreadID} {
		if value, exists := raw[name]; exists && (string(value) == "null" || json.Unmarshal(value, target) != nil) {
			return nil, 0, authz.ErrInvalid
		}
	}
	result, err := app.Write(r.Context(), webInvocation(p, r), kind, in)
	if err != nil {
		return nil, 0, err
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	if kind == goldenpath.TicketFromMessage {
		source, e := publicExecutionMessage(result.Source)
		if e != nil {
			return nil, 0, e
		}
		ticket, e := publicTicket(result.Ticket)
		if e != nil {
			return nil, 0, e
		}
		return map[string]any{"ticket": ticket, "source_message": source}, status, nil
	}
	if !validScopedID(result.LinkID, "lnk_") || (kind == goldenpath.TicketDiscussionUnlink && (result.Created || in.LinkID != result.LinkID)) {
		return nil, 0, errors.New("invalid Ticket discussion result")
	}
	return map[string]any{"link": entityRefDTO{Type: "entity-link", ID: result.LinkID}, "applied": true}, status, nil
}

type executionMessageDTO struct {
	Ref     entityRefDTO  `json:"ref"`
	Channel entityRefDTO  `json:"channel"`
	Thread  *entityRefDTO `json:"thread"`
}

func publicExecutionMessage(location goldenpath.MessageLocation) (executionMessageDTO, error) {
	if !validScopedID(location.MessageID, "msg_") || !validScopedID(location.ChannelID, "chn_") {
		return executionMessageDTO{}, errors.New("invalid Message Ticket location")
	}
	out := executionMessageDTO{Ref: entityRefDTO{Type: "message", ID: location.MessageID}, Channel: entityRefDTO{Type: "channel", ID: location.ChannelID}}
	if location.ThreadID != nil {
		if !validScopedID(*location.ThreadID, "thr_") {
			return out, errors.New("invalid Message Ticket Thread")
		}
		out.Thread = &entityRefDTO{Type: "thread", ID: *location.ThreadID}
	}
	return out, nil
}

func readExecutionList(r *http.Request, p authz.Principal, app *goldenpath.TicketExecutionService, source entityref.Ref, channel string) (any, error) {
	scope := discoveryCursor{Version: 1, WorkspaceID: p.WorkspaceID, ActorID: p.ID, Kind: source.Type + "-execution", ProjectID: source.ID}
	in, err := parseDiscoveryQuery(r.URL.RawQuery, scope)
	if err != nil {
		return nil, err
	}
	page, err := app.ListTicketExecution(r.Context(), p, source, channel, in)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(page.Rows))
	last := in.AfterID
	for _, row := range page.Rows {
		if row.Ref.ID <= last || entityref.M0Registry().Validate(row.Ref) != nil || strings.TrimSpace(row.Title) == "" {
			return nil, errors.New("invalid Ticket execution page")
		}
		last = row.Ref.ID
		item := map[string]any{"ref": entityRefDTO{Type: row.Ref.Type, ID: row.Ref.ID}, "title": row.Title}
		if source.Type == "ticket" {
			if row.Ref.Type != "thread" || !validScopedID(row.ChannelID, "chn_") {
				return nil, errors.New("invalid discussion Thread")
			}
			item["channel"] = entityRefDTO{Type: "channel", ID: row.ChannelID}
		} else {
			if row.Ref.Type != "ticket" || !goldenpath.ValidTicketStatus(row.Status) || !validScopedID(row.ProjectID, "prj_") {
				return nil, errors.New("invalid discussion Ticket")
			}
			item["status"] = row.Status
			item["project"] = entityRefDTO{Type: "project", ID: row.ProjectID}
		}
		if source.Type != "message" {
			if !validScopedID(row.LinkID, "lnk_") {
				return nil, errors.New("invalid discussion reference")
			}
			item["link_id"], item["can_unlink"] = row.LinkID, row.CanUnlink
		}
		items = append(items, item)
	}
	var next *string
	if len(items) > in.Limit || (page.NextID != "" && (len(items) != in.Limit || page.NextID != last)) {
		return nil, errors.New("invalid Ticket execution pagination")
	}
	if page.NextID != "" {
		scope.AfterID = page.NextID
		raw, e := json.Marshal(scope)
		if e != nil {
			return nil, e
		}
		token := base64.RawURLEncoding.EncodeToString(raw)
		next = &token
	}
	canKey := "can_link"
	if source.Type == "message" {
		canKey = "can_create"
	}
	return map[string]any{"items": items, "next_cursor": next, "capabilities": map[string]bool{canKey: page.CanWrite}}, nil
}

func readExecutionReplies(r *http.Request, p authz.Principal, app *goldenpath.TicketExecutionService, thread string) (any, error) {
	if len(r.URL.RawQuery) > 2048 {
		return nil, authz.ErrInvalid
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, authz.ErrInvalid
	}
	if values, exists := query["before"]; exists {
		if len(values) != 1 {
			return nil, authz.ErrInvalid
		}
		value, ok := strings.CutPrefix(values[0], thread+".")
		if !ok {
			return nil, authz.ErrInvalid
		}
		query.Set("before", value)
	}
	in, err := parseMessagePageQuery(query.Encode(), "")
	if err != nil {
		return nil, err
	}
	page, channel, canReply, err := app.ListExecutionReplies(r.Context(), p, thread, in)
	if err != nil {
		return nil, err
	}
	for _, m := range page.Messages {
		if m.ThreadID == nil || *m.ThreadID != thread {
			return nil, errors.New("invalid Thread reply")
		}
	}
	dto, err := publicMessagePage(channel, in.Limit, page)
	if err != nil {
		return nil, err
	}
	if dto.OlderCursor != nil {
		value := thread + "." + *dto.OlderCursor
		dto.OlderCursor = &value
	}
	return struct {
		messagePageDTO
		Channel  entityRefDTO `json:"channel"`
		CanReply bool         `json:"can_reply"`
	}{dto, entityRefDTO{Type: "channel", ID: channel}, canReply}, nil
}
