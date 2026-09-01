// Package messagingrealtime is a disposable experiment for message command
// idempotency, resumable delivery, and authorization convergence.
package messagingrealtime

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

const MaxMessageBodyBytes = 16 * 1024

var (
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("forbidden")
	ErrConflict     = errors.New("conflict")
	ErrInvalid      = errors.New("invalid input")
	ErrCursorResync = errors.New("cursor requires resync")
)

type AccessDecision struct {
	Read bool
	Post bool
}

type accessKey struct {
	userID    string
	channelID string
}

// AccessGate stands in for the formal Workspace + Project + Channel policy.
// Its change signal is deliberately separate from event delivery so an idle
// subscription converges after permission revocation.
type AccessGate struct {
	mu          sync.RWMutex
	decisions   map[accessKey]AccessDecision
	channelSubs map[string]map[chan struct{}]struct{}
}

func NewAccessGate() *AccessGate {
	return &AccessGate{
		decisions:   make(map[accessKey]AccessDecision),
		channelSubs: make(map[string]map[chan struct{}]struct{}),
	}
}

func (gate *AccessGate) Set(userID, channelID string, decision AccessDecision) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.decisions[accessKey{userID: userID, channelID: channelID}] = decision
	for subscriber := range gate.channelSubs[channelID] {
		notify(subscriber)
	}
}

func (gate *AccessGate) Decide(userID, channelID string) AccessDecision {
	gate.mu.RLock()
	defer gate.mu.RUnlock()
	return gate.decisions[accessKey{userID: userID, channelID: channelID}]
}

func (gate *AccessGate) Watch(channelID string) (<-chan struct{}, func()) {
	updates := make(chan struct{}, 1)
	gate.mu.Lock()
	if gate.channelSubs[channelID] == nil {
		gate.channelSubs[channelID] = make(map[chan struct{}]struct{})
	}
	gate.channelSubs[channelID][updates] = struct{}{}
	gate.mu.Unlock()

	var once sync.Once
	return updates, func() {
		once.Do(func() {
			gate.mu.Lock()
			delete(gate.channelSubs[channelID], updates)
			if len(gate.channelSubs[channelID]) == 0 {
				delete(gate.channelSubs, channelID)
			}
			gate.mu.Unlock()
		})
	}
}

type Message struct {
	ID                string `json:"id"`
	ChannelID         string `json:"channel_id"`
	AuthorID          string `json:"author_id"`
	ClientOperationID string `json:"-"`
	Body              string `json:"body"`
	Cursor            string `json:"-"`
}

type PublishInput struct {
	ChannelID         string
	AuthorID          string
	ClientOperationID string
	Body              string
}

type MessageEvent struct {
	Cursor   string  `json:"-"`
	Type     string  `json:"type"`
	Message  Message `json:"message"`
	position uint64
}

type operationKey struct {
	channelID string
	authorID  string
	operation string
}

type channelState struct {
	nextPosition uint64
	events       []MessageEvent
	subscribers  map[chan struct{}]struct{}
}

// Hub models one process and a bounded replay projection. Messages remain the
// authority; stream events are only resumable delivery hints.
type Hub struct {
	mu          sync.Mutex
	generation  string
	maxReplay   int
	access      *AccessGate
	channels    map[string]*channelState
	operations  map[operationKey]Message
	nextMessage uint64
}

func NewHub(generation string, maxReplay int, access *AccessGate) (*Hub, error) {
	if !validOpaqueASCII(generation, 64) || maxReplay < 1 || access == nil {
		return nil, fmt.Errorf("%w: generation, replay limit, and access gate are required", ErrInvalid)
	}
	return &Hub{
		generation: generation,
		maxReplay:  maxReplay,
		access:     access,
		channels:   make(map[string]*channelState),
		operations: make(map[operationKey]Message),
	}, nil
}

func (hub *Hub) Publish(input PublishInput) (Message, bool, error) {
	if err := validatePublishInput(input); err != nil {
		return Message{}, false, err
	}
	decision := hub.access.Decide(input.AuthorID, input.ChannelID)
	if !decision.Read {
		return Message{}, false, ErrNotFound
	}
	if !decision.Post {
		return Message{}, false, ErrForbidden
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()
	key := operationKey{
		channelID: input.ChannelID,
		authorID:  input.AuthorID,
		operation: input.ClientOperationID,
	}
	if existing, found := hub.operations[key]; found {
		if existing.Body != input.Body {
			return Message{}, false, fmt.Errorf("%w: client operation payload changed", ErrConflict)
		}
		return existing, false, nil
	}

	state := hub.channel(input.ChannelID)
	state.nextPosition++
	hub.nextMessage++
	cursor := hub.cursor(state.nextPosition)
	message := Message{
		ID:                fmt.Sprintf("msg_%016x", hub.nextMessage),
		ChannelID:         input.ChannelID,
		AuthorID:          input.AuthorID,
		ClientOperationID: input.ClientOperationID,
		Body:              input.Body,
		Cursor:            cursor,
	}
	event := MessageEvent{
		Cursor:   cursor,
		Type:     "message.created",
		Message:  message,
		position: state.nextPosition,
	}
	state.events = append(state.events, event)
	if len(state.events) > hub.maxReplay {
		state.events = append([]MessageEvent(nil), state.events[len(state.events)-hub.maxReplay:]...)
	}
	hub.operations[key] = message
	for subscriber := range state.subscribers {
		notify(subscriber)
	}
	return message, true, nil
}

func (hub *Hub) CurrentCursor(channelID string) string {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return hub.cursor(hub.channel(channelID).nextPosition)
}

func (hub *Hub) Replay(userID, channelID, rawCursor string) ([]MessageEvent, error) {
	if !hub.access.Decide(userID, channelID).Read {
		return nil, ErrNotFound
	}
	position, err := hub.parseCursor(rawCursor)
	if err != nil {
		return nil, err
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()
	state := hub.channel(channelID)
	if position > state.nextPosition {
		return nil, fmt.Errorf("%w: cursor is ahead of the channel", ErrCursorResync)
	}
	if len(state.events) > 0 && position < state.events[0].position-1 {
		return nil, ErrCursorResync
	}
	events := make([]MessageEvent, 0, len(state.events))
	for _, event := range state.events {
		if event.position > position {
			events = append(events, event)
		}
	}
	return events, nil
}

func (hub *Hub) Watch(channelID string) (<-chan struct{}, func()) {
	updates := make(chan struct{}, 1)
	hub.mu.Lock()
	state := hub.channel(channelID)
	state.subscribers[updates] = struct{}{}
	hub.mu.Unlock()

	var once sync.Once
	return updates, func() {
		once.Do(func() {
			hub.mu.Lock()
			delete(hub.channel(channelID).subscribers, updates)
			hub.mu.Unlock()
		})
	}
}

func (hub *Hub) CanRead(userID, channelID string) bool {
	return hub.access.Decide(userID, channelID).Read
}

func (hub *Hub) WatchAccess(channelID string) (<-chan struct{}, func()) {
	return hub.access.Watch(channelID)
}

func (hub *Hub) channel(channelID string) *channelState {
	state := hub.channels[channelID]
	if state == nil {
		state = &channelState{subscribers: make(map[chan struct{}]struct{})}
		hub.channels[channelID] = state
	}
	return state
}

func (hub *Hub) cursor(position uint64) string {
	return hub.generation + ":" + strconv.FormatUint(position, 10)
}

func (hub *Hub) parseCursor(raw string) (uint64, error) {
	prefix := hub.generation + ":"
	if !strings.HasPrefix(raw, prefix) {
		return 0, ErrCursorResync
	}
	positionText := strings.TrimPrefix(raw, prefix)
	position, err := strconv.ParseUint(positionText, 10, 64)
	if err != nil || strconv.FormatUint(position, 10) != positionText {
		return 0, fmt.Errorf("%w: cursor is not canonical", ErrInvalid)
	}
	return position, nil
}

func validatePublishInput(input PublishInput) error {
	if !validOpaqueASCII(input.ChannelID, 128) || !strings.HasPrefix(input.ChannelID, "chn_") {
		return fmt.Errorf("%w: canonical channel ID is required", ErrInvalid)
	}
	if !validOpaqueASCII(input.AuthorID, 128) || !strings.HasPrefix(input.AuthorID, "usr_") {
		return fmt.Errorf("%w: canonical author ID is required", ErrInvalid)
	}
	if !validOpaqueASCII(input.ClientOperationID, 128) {
		return fmt.Errorf("%w: canonical client operation ID is required", ErrInvalid)
	}
	if !utf8.ValidString(input.Body) || strings.ContainsRune(input.Body, '\x00') ||
		strings.TrimSpace(input.Body) == "" || len(input.Body) > MaxMessageBodyBytes {
		return fmt.Errorf("%w: message body must be non-empty UTF-8 within %d bytes", ErrInvalid, MaxMessageBodyBytes)
	}
	return nil
}

func validOpaqueASCII(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e || strings.ContainsRune("/\\?#", rune(value[index])) {
			return false
		}
	}
	return true
}

func notify(target chan struct{}) {
	select {
	case target <- struct{}{}:
	default:
	}
}
