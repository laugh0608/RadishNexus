// Package realtime owns the bounded, process-local wake and replay state used
// by the formal Message SSE transport. Canonical Message data and permissions
// remain in PostgreSQL and are deliberately not cached here.
package realtime

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

const MaxCursorBytes = 512

var (
	ErrResyncRequired = errors.New("realtime resync required")
	ErrCapacity       = errors.New("realtime connection capacity reached")
	ErrClosed         = errors.New("realtime hub closed")
)

type Config struct {
	Generation             string
	ReplayLimit            int
	ConnectionLimit        int
	UserConnectionLimit    int
	ChannelConnectionLimit int
}

func DefaultConfig() (Config, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return Config{}, fmt.Errorf("generate realtime process generation: %w", err)
	}
	return Config{
		Generation:             base64.RawURLEncoding.EncodeToString(random),
		ReplayLimit:            1024,
		ConnectionLimit:        256,
		UserConnectionLimit:    4,
		ChannelConnectionLimit: 64,
	}, nil
}

type MessageNotification struct {
	WorkspaceID string
	ChannelID   string
	MessageID   string
}

type Event struct {
	Cursor    string
	MessageID string
}

type channelKey struct {
	workspaceID string
	channelID   string
}

type userKey struct {
	workspaceID string
	userID      string
}

type record struct {
	position  uint64
	messageID string
}

type channelState struct {
	position      uint64
	records       []record
	subscriptions map[*Subscription]struct{}
}

type Hub struct {
	mu                 sync.Mutex
	config             Config
	channels           map[channelKey]*channelState
	userConnections    map[userKey]int
	channelConnections map[channelKey]int
	connectionCount    int
	closed             bool
}

func NewHub(config Config) (*Hub, error) {
	if config.Generation == "" || len(config.Generation) > 128 ||
		config.ReplayLimit < 1 || config.ConnectionLimit < 1 ||
		config.UserConnectionLimit < 1 || config.ChannelConnectionLimit < 1 ||
		config.UserConnectionLimit > config.ConnectionLimit ||
		config.ChannelConnectionLimit > config.ConnectionLimit {
		return nil, fmt.Errorf("invalid realtime hub configuration")
	}
	return &Hub{
		config:             config,
		channels:           make(map[channelKey]*channelState),
		userConnections:    make(map[userKey]int),
		channelConnections: make(map[channelKey]int),
	}, nil
}

// NotifyMessageCreated records only a stable Message ID after its authoritative
// transaction has committed. Invalid internal notifications fail closed.
func (hub *Hub) NotifyMessageCreated(notification MessageNotification) {
	if notification.WorkspaceID == "" || notification.ChannelID == "" || notification.MessageID == "" {
		return
	}
	key := channelKey{workspaceID: notification.WorkspaceID, channelID: notification.ChannelID}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.closed {
		return
	}
	state := hub.channel(key)
	state.position++
	state.records = append(state.records, record{position: state.position, messageID: notification.MessageID})
	if overflow := len(state.records) - hub.config.ReplayLimit; overflow > 0 {
		copy(state.records, state.records[overflow:])
		state.records = state.records[:hub.config.ReplayLimit]
	}
	wake(state)
}

// NotifyChannelAccessChanged only wakes subscribers. The HTTP transport must
// still re-read current Session and application authorization facts.
func (hub *Hub) NotifyChannelAccessChanged(workspaceID string, channelID string) {
	key := channelKey{workspaceID: workspaceID, channelID: channelID}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if state := hub.channels[key]; state != nil {
		wake(state)
	}
}

func (hub *Hub) Subscribe(
	workspaceID string,
	channelID string,
	userID string,
	lastEventID string,
) (*Subscription, string, error) {
	if workspaceID == "" || channelID == "" || userID == "" || len(lastEventID) > MaxCursorBytes {
		return nil, "", ErrResyncRequired
	}
	key := channelKey{workspaceID: workspaceID, channelID: channelID}
	user := userKey{workspaceID: workspaceID, userID: userID}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.closed {
		return nil, "", ErrClosed
	}
	state := hub.channel(key)
	position := state.position
	if lastEventID != "" {
		decoded, err := hub.decodeCursor(key, lastEventID)
		if err != nil || decoded > state.position || replayUnavailable(state, decoded) {
			return nil, "", ErrResyncRequired
		}
		position = decoded
	}
	if hub.connectionCount >= hub.config.ConnectionLimit ||
		hub.userConnections[user] >= hub.config.UserConnectionLimit ||
		hub.channelConnections[key] >= hub.config.ChannelConnectionLimit {
		return nil, "", ErrCapacity
	}
	subscription := &Subscription{
		hub:      hub,
		key:      key,
		user:     user,
		position: position,
		wake:     make(chan struct{}, 1),
	}
	state.subscriptions[subscription] = struct{}{}
	hub.connectionCount++
	hub.userConnections[user]++
	hub.channelConnections[key]++
	ready, err := hub.encodeCursor(key, position)
	if err != nil {
		delete(state.subscriptions, subscription)
		hub.releaseCounts(user, key)
		return nil, "", err
	}
	return subscription, ready, nil
}

func (hub *Hub) Shutdown() {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.closed {
		return
	}
	hub.closed = true
	for _, state := range hub.channels {
		wake(state)
	}
}

func (hub *Hub) channel(key channelKey) *channelState {
	state := hub.channels[key]
	if state == nil {
		state = &channelState{subscriptions: make(map[*Subscription]struct{})}
		hub.channels[key] = state
	}
	return state
}

func (hub *Hub) releaseCounts(user userKey, key channelKey) {
	hub.connectionCount--
	hub.userConnections[user]--
	if hub.userConnections[user] == 0 {
		delete(hub.userConnections, user)
	}
	hub.channelConnections[key]--
	if hub.channelConnections[key] == 0 {
		delete(hub.channelConnections, key)
	}
}

func wake(state *channelState) {
	for subscription := range state.subscriptions {
		select {
		case subscription.wake <- struct{}{}:
		default:
		}
	}
}

func replayUnavailable(state *channelState, position uint64) bool {
	return position < state.position &&
		(len(state.records) == 0 || position+1 < state.records[0].position)
}

type Subscription struct {
	hub      *Hub
	key      channelKey
	user     userKey
	position uint64
	wake     chan struct{}
	closed   bool
}

func (subscription *Subscription) Wake() <-chan struct{} {
	return subscription.wake
}

func (subscription *Subscription) Drain() ([]Event, error) {
	hub := subscription.hub
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if subscription.closed || hub.closed {
		return nil, ErrClosed
	}
	state := hub.channel(subscription.key)
	if replayUnavailable(state, subscription.position) {
		return nil, ErrResyncRequired
	}
	events := make([]Event, 0, int(state.position-subscription.position))
	for _, item := range state.records {
		if item.position <= subscription.position {
			continue
		}
		cursor, err := hub.encodeCursor(subscription.key, item.position)
		if err != nil {
			return nil, err
		}
		events = append(events, Event{Cursor: cursor, MessageID: item.messageID})
	}
	subscription.position = state.position
	return events, nil
}

func (subscription *Subscription) Close() {
	hub := subscription.hub
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if subscription.closed {
		return
	}
	subscription.closed = true
	if state := hub.channels[subscription.key]; state != nil {
		delete(state.subscriptions, subscription)
	}
	hub.releaseCounts(subscription.user, subscription.key)
}

type cursor struct {
	Version    int    `json:"v"`
	Generation string `json:"g"`
	Scope      string `json:"s"`
	Position   uint64 `json:"p"`
}

func (hub *Hub) encodeCursor(key channelKey, position uint64) (string, error) {
	body, err := json.Marshal(cursor{
		Version:    1,
		Generation: hub.config.Generation,
		Scope:      cursorScope(key),
		Position:   position,
	})
	if err != nil {
		return "", fmt.Errorf("marshal realtime cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(body), nil
}

func (hub *Hub) decodeCursor(key channelKey, value string) (uint64, error) {
	invalid := func() (uint64, error) { return 0, ErrResyncRequired }
	if value == "" || len(value) > MaxCursorBytes {
		return invalid()
	}
	body, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return invalid()
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	var decoded cursor
	if err := decoder.Decode(&decoded); err != nil {
		return invalid()
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return invalid()
	}
	if decoded.Version != 1 || decoded.Generation != hub.config.Generation ||
		decoded.Scope != cursorScope(key) {
		return invalid()
	}
	canonical, err := hub.encodeCursor(key, decoded.Position)
	if err != nil || canonical != value {
		return invalid()
	}
	return decoded.Position, nil
}

func cursorScope(key channelKey) string {
	digest := sha256.Sum256([]byte(key.workspaceID + "\x00" + key.channelID))
	return base64.RawURLEncoding.EncodeToString(digest[:16])
}
