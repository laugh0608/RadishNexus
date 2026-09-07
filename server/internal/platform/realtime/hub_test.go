package realtime

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestHubPublishesReplaysAndRejectsInvalidCursors(t *testing.T) {
	t.Parallel()
	hub := testHub(t, Config{
		Generation: "test-generation", ReplayLimit: 2,
		ConnectionLimit: 8, UserConnectionLimit: 4, ChannelConnectionLimit: 4,
	})
	subscription, ready, err := hub.Subscribe("wrk_main", "chn_main", "usr_one", "")
	if err != nil || ready == "" {
		t.Fatalf("Subscribe() = %q, %v", ready, err)
	}
	defer subscription.Close()

	hub.NotifyMessageCreated(MessageNotification{WorkspaceID: "wrk_main", ChannelID: "chn_main", MessageID: "msg_one"})
	hub.NotifyMessageCreated(MessageNotification{WorkspaceID: "wrk_main", ChannelID: "chn_main", MessageID: "msg_two"})
	events, err := subscription.Drain()
	if err != nil || len(events) != 2 || events[0].MessageID != "msg_one" || events[1].MessageID != "msg_two" {
		t.Fatalf("Drain() = %#v, %v", events, err)
	}

	replay, _, err := hub.Subscribe("wrk_main", "chn_main", "usr_two", events[0].Cursor)
	if err != nil {
		t.Fatalf("replay Subscribe() error = %v", err)
	}
	defer replay.Close()
	replayed, err := replay.Drain()
	if err != nil || len(replayed) != 1 || replayed[0].MessageID != "msg_two" {
		t.Fatalf("replay Drain() = %#v, %v", replayed, err)
	}

	for name, cursor := range map[string]string{
		"malformed":     "not-a-cursor",
		"other channel": events[1].Cursor,
		"future": func() string {
			value, encodeErr := hub.encodeCursor(channelKey{workspaceID: "wrk_main", channelID: "chn_main"}, 99)
			if encodeErr != nil {
				t.Fatalf("encode future cursor: %v", encodeErr)
			}
			return value
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			channel := "chn_main"
			if name == "other channel" {
				channel = "chn_other"
			}
			_, _, err := hub.Subscribe("wrk_main", channel, "usr_three", cursor)
			if !errors.Is(err, ErrResyncRequired) {
				t.Fatalf("Subscribe() error = %v, want resync", err)
			}
		})
	}
	otherGeneration := testHub(t, Config{
		Generation: "other-generation", ReplayLimit: 2,
		ConnectionLimit: 8, UserConnectionLimit: 4, ChannelConnectionLimit: 4,
	})
	otherSubscription, otherCursor, err := otherGeneration.Subscribe("wrk_main", "chn_main", "usr_other", "")
	if err != nil {
		t.Fatalf("other generation Subscribe() error = %v", err)
	}
	defer otherSubscription.Close()
	if _, _, err := hub.Subscribe("wrk_main", "chn_main", "usr_three", otherCursor); !errors.Is(err, ErrResyncRequired) {
		t.Fatalf("other generation cursor error = %v, want resync", err)
	}

	hub.NotifyMessageCreated(MessageNotification{WorkspaceID: "wrk_main", ChannelID: "chn_main", MessageID: "msg_three"})
	_, _, err = hub.Subscribe("wrk_main", "chn_main", "usr_four", ready)
	if !errors.Is(err, ErrResyncRequired) {
		t.Fatalf("stale Subscribe() error = %v, want resync", err)
	}
}

func TestHubDoesNotBlockSlowSubscriberAndSignalsAccessAndShutdown(t *testing.T) {
	t.Parallel()
	hub := testHub(t, Config{
		Generation: "test-generation", ReplayLimit: 8,
		ConnectionLimit: 8, UserConnectionLimit: 4, ChannelConnectionLimit: 4,
	})
	subscription, _, err := hub.Subscribe("wrk_main", "chn_main", "usr_one", "")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer subscription.Close()

	finished := make(chan struct{})
	go func() {
		for index := 0; index < 1000; index++ {
			hub.NotifyMessageCreated(MessageNotification{
				WorkspaceID: "wrk_main", ChannelID: "chn_main", MessageID: "msg_" + strconv.Itoa(index),
			})
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber blocked publishing")
	}
	if _, err := subscription.Drain(); !errors.Is(err, ErrResyncRequired) {
		t.Fatalf("slow Drain() error = %v, want resync", err)
	}

	select {
	case <-subscription.Wake():
	default:
	}
	hub.NotifyChannelAccessChanged("wrk_main", "chn_main")
	select {
	case <-subscription.Wake():
	case <-time.After(time.Second):
		t.Fatal("access change did not wake subscriber")
	}
	hub.Shutdown()
	select {
	case <-subscription.Wake():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not wake subscriber")
	}
	if _, err := subscription.Drain(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Drain() after shutdown error = %v", err)
	}
}

func TestHubEnforcesAndReleasesConnectionLimits(t *testing.T) {
	t.Parallel()
	hub := testHub(t, Config{
		Generation: "test-generation", ReplayLimit: 8,
		ConnectionLimit: 3, UserConnectionLimit: 2, ChannelConnectionLimit: 2,
	})
	first, _, err := hub.Subscribe("wrk_main", "chn_main", "usr_one", "")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := hub.Subscribe("wrk_main", "chn_other", "usr_one", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hub.Subscribe("wrk_main", "chn_third", "usr_one", ""); !errors.Is(err, ErrCapacity) {
		t.Fatalf("per-user limit error = %v", err)
	}
	third, _, err := hub.Subscribe("wrk_main", "chn_main", "usr_two", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hub.Subscribe("wrk_main", "chn_main", "usr_three", ""); !errors.Is(err, ErrCapacity) {
		t.Fatalf("channel/global limit error = %v", err)
	}
	second.Close()
	fourth, _, err := hub.Subscribe("wrk_main", "chn_third", "usr_three", "")
	if err != nil {
		t.Fatalf("released slot Subscribe() error = %v", err)
	}
	first.Close()
	third.Close()
	fourth.Close()
}

func testHub(t *testing.T, config Config) *Hub {
	t.Helper()
	hub, err := NewHub(config)
	if err != nil {
		t.Fatalf("NewHub() error = %v", err)
	}
	return hub
}
