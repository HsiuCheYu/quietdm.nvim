package transport

import (
	"context"
	"testing"
	"time"

	"github.com/HsiuCheYu/quietdm.nvim/internal/config"
)

func testMock(t *testing.T, cfg config.Mock) (*Mock, <-chan Event, context.CancelFunc) {
	t.Helper()
	m := NewMock(cfg)
	// Replace the clock so the script runs instantly.
	m.sleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
	ctx, cancel := context.WithCancel(context.Background())
	events, err := m.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return m, events, cancel
}

func recv(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("event channel closed early")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
	}
	return Event{}
}

func TestMockReplaysScript(t *testing.T) {
	cfg := config.Mock{
		Rooms: []config.MockRoom{{ID: "!r:localhost", Display: "m.chen"}},
		Msgs: []config.MockMessage{
			{Room: "!r:localhost", Sender: "@mia:localhost", Body: "one"},
			{Room: "!r:localhost", Sender: "@mia:localhost", Body: "two"},
		},
	}
	_, events, cancel := testMock(t, cfg)
	defer cancel()

	if ev := recv(t, events); ev.Type != EventConnected || !ev.Connected {
		t.Fatalf("first event = %+v, want connected", ev)
	}
	for _, want := range []string{"one", "two"} {
		ev := recv(t, events)
		if ev.Type != EventMessage || ev.Message.Body != want {
			t.Fatalf("got %+v, want body %q", ev, want)
		}
		if ev.Message.Event == "" {
			t.Error("every message needs an event ID for de-duplication")
		}
	}
}

func TestMockStaysUpAfterTheScriptEnds(t *testing.T) {
	cfg := config.Mock{
		Rooms: []config.MockRoom{{ID: "!r:localhost"}},
		Msgs:  []config.MockMessage{{Room: "!r:localhost", Body: "only"}},
	}
	_, events, cancel := testMock(t, cfg)
	defer cancel()
	recv(t, events) // connected
	recv(t, events) // the one message

	// The channel must stay open: a transport with nothing left to say is not
	// a disconnected transport, and the daemon must not exit.
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("channel closed when the script ended")
		}
		t.Fatalf("unexpected extra event %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMockSendEchoesBack(t *testing.T) {
	cfg := config.Mock{Rooms: []config.MockRoom{{ID: "!r:localhost"}}}
	m, events, cancel := testMock(t, cfg)
	defer cancel()
	recv(t, events) // connected

	id, err := m.Send(context.Background(), "!r:localhost", "hi")
	if err != nil {
		t.Fatal(err)
	}
	ev := recv(t, events)
	if ev.Message.Event != id || !ev.Message.Own || ev.Message.Body != "hi" {
		t.Fatalf("echo = %+v", ev.Message)
	}
}

func TestMockRejectsUnknownRoom(t *testing.T) {
	m := NewMock(config.Mock{Rooms: []config.MockRoom{{ID: "!r:localhost"}}})
	if _, err := m.Send(context.Background(), "!nope", "hi"); err == nil {
		t.Fatal("want an error for an unknown room")
	}
	if err := m.MarkRead(context.Background(), "!nope", ""); err == nil {
		t.Fatal("want an error for an unknown room")
	}
}
