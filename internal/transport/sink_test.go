package transport

import (
	"context"
	"sync"
	"testing"
)

// Closing an event channel while somebody is still sending on it panics, and
// shutdown is exactly when that happens: an in-flight :QuietdmReply meets a
// cancelled context. The buffer is deliberately smaller than the number of
// senders so most of them are blocked when the close lands.
func TestEventSinkSurvivesAConcurrentClose(t *testing.T) {
	for range 100 {
		s := newEventSink(1)
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.send(context.Background(), Event{Type: EventConnected, Connected: true})
			}()
		}
		go s.close()
		wg.Wait()
	}
}

func TestEventSinkAfterClose(t *testing.T) {
	s := newEventSink(1)
	s.close()
	s.close() // closing twice is not a panic
	if s.send(context.Background(), Event{Type: EventConnected}) {
		t.Error("a closed sink must not accept events")
	}
	if _, ok := <-s.events(); ok {
		t.Error("the receiving end must see a closed channel")
	}
}

func TestEventSinkGivesUpWithTheContext(t *testing.T) {
	s := newEventSink(0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.send(ctx, Event{Type: EventConnected}) {
		t.Error("a cancelled context must not block forever")
	}
}
