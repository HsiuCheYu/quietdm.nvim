package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
	"github.com/HsiuCheYu/quietdm.nvim/internal/sanitize"
	"github.com/HsiuCheYu/quietdm.nvim/internal/store"
	"github.com/HsiuCheYu/quietdm.nvim/internal/transport"
)

// fakeTransport lets a test push events at will.
type fakeTransport struct {
	events chan transport.Event
	rooms  []transport.Room
	sent   []string
	marked []string
}

func (f *fakeTransport) Start(context.Context) (<-chan transport.Event, error) {
	return f.events, nil
}

func (f *fakeTransport) Send(_ context.Context, roomID, body string) (string, error) {
	f.sent = append(f.sent, roomID+":"+body)
	return "$sent", nil
}

func (f *fakeTransport) MarkRead(_ context.Context, roomID, eventID string) error {
	f.marked = append(f.marked, roomID+":"+eventID)
	return nil
}

func (f *fakeTransport) Rooms(context.Context) ([]transport.Room, error) { return f.rooms, nil }

func (f *fakeTransport) Close() error { return nil }

// recorder collects everything broadcast to the frontends.
type recorder struct {
	mu       sync.Mutex
	messages []model.Message
	rooms    []model.Room
}

func (r *recorder) BroadcastMessage(m model.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, m)
}

func (r *recorder) BroadcastRoom(room model.Room) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rooms = append(r.rooms, room)
}

func (r *recorder) lastRoom() model.Room {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.rooms) == 0 {
		return model.Room{}
	}
	return r.rooms[len(r.rooms)-1]
}

func newSession(t *testing.T) (*Session, *fakeTransport, *recorder, context.CancelFunc) {
	t.Helper()
	tr := &fakeTransport{
		events: make(chan transport.Event, 8),
		rooms:  []transport.Room{{ID: "!r:localhost", Display: "m.chen"}},
	}
	rec := &recorder{}
	s := New(tr, store.NewMemory(0), Options{
		Aliases:  map[string]string{"@mia:localhost": "m.chen"},
		Sanitize: sanitize.Options{StripEmoji: true},
	})
	s.SetBroadcaster(rec)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	// Wait until the room list has been picked up.
	waitFor(t, func() bool { return s.Knows("!r:localhost") })
	return s, tr, rec, cancel
}

// waitFor polls until cond holds; the session processes events on its own
// goroutine, so a test cannot simply read back straight away.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

func incoming(body string) transport.Event {
	return transport.Event{
		Type: transport.EventMessage,
		Message: model.Message{
			Room: "!r:localhost", Event: "$1", Sender: "@mia:localhost",
			Body: body, TS: 100, Kind: model.KindText,
		},
	}
}

func TestIngestAppliesAliasAndSanitiser(t *testing.T) {
	s, tr, rec, _ := newSession(t)
	tr.events <- incoming("晚上要吃什麼 😂")
	waitFor(t, func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.messages) == 1
	})

	got := rec.messages[0]
	if got.Display != "m.chen" {
		t.Errorf("display = %q, want the alias", got.Display)
	}
	if got.Body != "晚上要吃什麼 :D" {
		t.Errorf("body = %q, want the emoji folded to ASCII", got.Body)
	}
	if rec.lastRoom().Unread != 1 {
		t.Errorf("unread = %d, want 1", rec.lastRoom().Unread)
	}

	msgs, err := s.History(context.Background(), "!r:localhost", 10)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("history = %v, %v", msgs, err)
	}
}

func TestAliasFallsBackToLocalpart(t *testing.T) {
	_, tr, rec, _ := newSession(t)
	ev := incoming("hi")
	ev.Message.Sender = "@alex:localhost"
	tr.events <- ev
	waitFor(t, func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.messages) == 1
	})
	if rec.messages[0].Display != "alex" {
		t.Errorf("display = %q, want alex", rec.messages[0].Display)
	}
}

func TestMarkReadClearsUnreadAndReachesTheTransport(t *testing.T) {
	s, tr, rec, _ := newSession(t)
	tr.events <- incoming("hi")
	waitFor(t, func() bool { return rec.lastRoom().Unread == 1 })

	if err := s.MarkRead(context.Background(), "!r:localhost", "$1"); err != nil {
		t.Fatal(err)
	}
	if rec.lastRoom().Unread != 0 {
		t.Errorf("unread = %d, want 0", rec.lastRoom().Unread)
	}
	if len(tr.marked) != 1 {
		t.Errorf("the read receipt never reached the transport")
	}
}

func TestOwnMessagesAreNeverUnread(t *testing.T) {
	_, tr, rec, _ := newSession(t)
	ev := incoming("my own reply")
	ev.Message.Own = true
	ev.Message.Sender = "@me:localhost"
	tr.events <- ev
	waitFor(t, func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.messages) == 1
	})
	if rec.lastRoom().Unread != 0 {
		t.Errorf("unread = %d, want 0", rec.lastRoom().Unread)
	}
}

func TestUnknownRoomsAreRejected(t *testing.T) {
	s, _, _, _ := newSession(t)
	if _, err := s.Send(context.Background(), "!nope", "hi"); !errors.Is(err, transport.ErrUnknownRoom) {
		t.Errorf("Send error = %v, want ErrUnknownRoom", err)
	}
	if _, err := s.History(context.Background(), "!nope", 10); !errors.Is(err, transport.ErrUnknownRoom) {
		t.Errorf("History error = %v, want ErrUnknownRoom", err)
	}
}

func TestRoomsListIncludesUnread(t *testing.T) {
	s, tr, rec, _ := newSession(t)
	tr.events <- incoming("hi")
	waitFor(t, func() bool { return rec.lastRoom().Unread == 1 })

	rooms, err := s.Rooms(context.Background())
	if err != nil || len(rooms) != 1 {
		t.Fatalf("rooms = %v, %v", rooms, err)
	}
	if rooms[0].Display != "m.chen" || rooms[0].Unread != 1 || rooms[0].LastTS != 100 {
		t.Errorf("room = %+v", rooms[0])
	}
}
