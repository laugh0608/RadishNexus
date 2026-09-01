package messagingrealtime

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrentRetryCreatesOneMessage(t *testing.T) {
	gate := NewAccessGate()
	gate.Set(testWriter, testChannel, AccessDecision{Read: true, Post: true})
	hub, err := NewHub("process-a", 16, gate)
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}

	const retries = 64
	var created atomic.Int64
	ids := make(chan string, retries)
	errors := make(chan error, retries)
	var wait sync.WaitGroup
	for index := 0; index < retries; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			message, wasCreated, publishErr := hub.Publish(PublishInput{
				ChannelID:         testChannel,
				AuthorID:          testWriter,
				ClientOperationID: "same-send",
				Body:              "same body",
			})
			if publishErr != nil {
				errors <- publishErr
				return
			}
			if wasCreated {
				created.Add(1)
			}
			ids <- message.ID
		}()
	}
	wait.Wait()
	close(ids)
	close(errors)

	for publishErr := range errors {
		t.Errorf("concurrent retry: %v", publishErr)
	}
	if got := created.Load(); got != 1 {
		t.Fatalf("created count = %d; want 1", got)
	}
	firstID := ""
	for id := range ids {
		if firstID == "" {
			firstID = id
		}
		if id != firstID {
			t.Fatalf("retry returned message ID %q; want %q", id, firstID)
		}
	}
}
