package transport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/HsiuCheYu/quietdm.nvim/internal/config"
	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
)

// ErrUnknownRoom is returned when a command names a room the transport does
// not know. The daemon turns it into an "unknown_room" protocol error.
var ErrUnknownRoom = errors.New("unknown room")

// Mock replays a scripted conversation from the configuration file. It exists
// so the covert model can be developed and demonstrated without standing up a
// homeserver at all (docs/design/05-roadmap.md, M1).
type Mock struct {
	script []config.MockMessage
	loop   bool

	mu    sync.RWMutex
	rooms []Room

	seq  atomic.Int64
	sink *eventSink

	// now and sleep are swappable so tests do not wait in real time.
	now   func() time.Time
	sleep func(context.Context, time.Duration) bool
}

// NewMock builds a mock transport from the [mock] configuration section.
func NewMock(cfg config.Mock) *Mock {
	rooms := make([]Room, 0, len(cfg.Rooms))
	for _, r := range cfg.Rooms {
		rooms = append(rooms, Room{ID: r.ID, Display: r.Display})
	}
	return &Mock{
		script: cfg.Msgs,
		loop:   cfg.Loop,
		rooms:  rooms,
		sink:   newEventSink(16),
		now:    time.Now,
		sleep:  sleepCtx,
	}
}

// sleepCtx waits for d and reports whether it completed rather than being
// cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Start begins replaying the script.
func (m *Mock) Start(ctx context.Context) (<-chan Event, error) {
	go m.run(ctx)
	return m.sink.events(), nil
}

func (m *Mock) run(ctx context.Context) {
	defer m.sink.close()
	m.sink.send(ctx, Event{Type: EventConnected, Connected: true})
	for {
		for _, s := range m.script {
			if !m.sleep(ctx, s.Delay()) {
				return
			}
			kind := s.Kind
			if kind == "" {
				kind = model.KindText
			}
			msg := model.Message{
				Room:    s.Room,
				Event:   m.nextEventID(),
				Sender:  s.Sender,
				Display: s.Sender,
				Body:    s.Body,
				TS:      m.now().Unix(),
				Own:     s.Own,
				Kind:    kind,
			}
			if !m.sink.send(ctx, Event{Type: EventMessage, Message: msg}) {
				return
			}
		}
		if !m.loop || len(m.script) == 0 {
			// The script is spent, but the daemon lives on: a transport with
			// nothing left to say is not a disconnected transport.
			select {
			case <-ctx.Done():
			case <-m.sink.stopped:
			}
			return
		}
	}
}

func (m *Mock) nextEventID() string {
	return fmt.Sprintf("$mock%d", m.seq.Add(1))
}

// Send echoes the message back through the event channel, the way a real
// homeserver echoes an event the user sent from another device.
func (m *Mock) Send(ctx context.Context, roomID, body string) (string, error) {
	if !m.knows(roomID) {
		return "", fmt.Errorf("%w: %s", ErrUnknownRoom, roomID)
	}
	id := m.nextEventID()
	msg := model.Message{
		Room:    roomID,
		Event:   id,
		Sender:  "@me:localhost",
		Display: "you",
		Body:    body,
		TS:      m.now().Unix(),
		Own:     true,
		Kind:    model.KindText,
	}
	if !m.sink.send(ctx, Event{Type: EventMessage, Message: msg}) {
		return "", ctx.Err()
	}
	return id, nil
}

// MarkRead is a no-op: there is nobody on the other end of a mock room.
func (m *Mock) MarkRead(_ context.Context, roomID, _ string) error {
	if !m.knows(roomID) {
		return fmt.Errorf("%w: %s", ErrUnknownRoom, roomID)
	}
	return nil
}

// Rooms lists the scripted conversations.
func (m *Mock) Rooms(_ context.Context) ([]Room, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Room, len(m.rooms))
	copy(out, m.rooms)
	return out, nil
}

func (m *Mock) knows(roomID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.rooms {
		if r.ID == roomID {
			return true
		}
	}
	return false
}

// Close stops any in-flight send. The event channel is closed by run once the
// context is cancelled.
func (m *Mock) Close() error {
	m.sink.close()
	return nil
}
