package transport

import (
	"context"
	"sync"
)

// eventSink is the channel a transport delivers events on, plus the bookkeeping
// that makes closing it safe.
//
// Closing an event channel while another goroutine is still sending on it
// panics, and both transports have a goroutine that can be late to the party:
// the mock echoes a reply from whichever goroutine handled the IPC command, and
// the crypto layer can finish decrypting a message seconds after the sync loop
// has given up. Shutdown is exactly when those two meet.
type eventSink struct {
	ch      chan Event
	stopped chan struct{}
	once    sync.Once
	// mu is held for reading by every send and for writing by close, so the
	// channel is only ever closed once no send is in flight.
	mu sync.RWMutex
}

func newEventSink(buffer int) *eventSink {
	return &eventSink{ch: make(chan Event, buffer), stopped: make(chan struct{})}
}

// events is the receiving end, for the session layer.
func (s *eventSink) events() <-chan Event { return s.ch }

// send delivers ev and reports whether it got through. It gives up once the
// sink is closed or ctx ends.
func (s *eventSink) send(ctx context.Context, ev Event) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	select {
	case <-s.stopped:
		return false
	default:
	}
	select {
	case s.ch <- ev:
		return true
	case <-s.stopped:
		return false
	case <-ctx.Done():
		return false
	}
}

// close ends the sink. Closing stopped first releases anything blocked on a
// full channel, so taking the write lock cannot deadlock against it.
func (s *eventSink) close() {
	s.once.Do(func() {
		close(s.stopped)
		s.mu.Lock()
		defer s.mu.Unlock()
		close(s.ch)
	})
}
