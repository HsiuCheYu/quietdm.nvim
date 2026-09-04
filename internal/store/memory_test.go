package store

import (
	"context"
	"testing"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
)

const room = "!r:localhost"

func msg(id string, own bool) model.Message {
	return model.Message{Room: room, Event: id, Sender: "@mia:localhost", Body: id, Own: own}
}

func TestAppendIgnoresDuplicates(t *testing.T) {
	ctx := context.Background()
	s := NewMemory(0)
	for i := 0; i < 3; i++ {
		if err := s.AppendMessage(ctx, msg("$1", false)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.RecentMessages(ctx, room, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}

func TestCapacityDropsOldest(t *testing.T) {
	ctx := context.Background()
	s := NewMemory(3)
	for _, id := range []string{"$1", "$2", "$3", "$4"} {
		_ = s.AppendMessage(ctx, msg(id, false))
	}
	got, _ := s.RecentMessages(ctx, room, 10)
	if len(got) != 3 || got[0].Event != "$2" || got[2].Event != "$4" {
		t.Fatalf("got %v", got)
	}
}

func TestRecentReturnsTailOldestFirst(t *testing.T) {
	ctx := context.Background()
	s := NewMemory(0)
	for _, id := range []string{"$1", "$2", "$3"} {
		_ = s.AppendMessage(ctx, msg(id, false))
	}
	got, _ := s.RecentMessages(ctx, room, 2)
	if len(got) != 2 || got[0].Event != "$2" || got[1].Event != "$3" {
		t.Fatalf("got %v", got)
	}
}

func TestUnreadCounting(t *testing.T) {
	ctx := context.Background()
	s := NewMemory(0)
	_ = s.AppendMessage(ctx, msg("$1", false))
	_ = s.AppendMessage(ctx, msg("$2", false))
	_ = s.AppendMessage(ctx, msg("$3", true)) // the user's own reply

	if n, _ := s.UnreadCount(ctx, room); n != 2 {
		t.Fatalf("unread = %d, want 2", n)
	}
	if err := s.SetReadMarker(ctx, room, "$1"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.UnreadCount(ctx, room); n != 1 {
		t.Fatalf("after marker: unread = %d, want 1", n)
	}
	// An empty event ID marks the whole room read.
	_ = s.SetReadMarker(ctx, room, "")
	if n, _ := s.UnreadCount(ctx, room); n != 0 {
		t.Fatalf("after full mark: unread = %d, want 0", n)
	}
}

func TestUnknownRoomIsEmpty(t *testing.T) {
	ctx := context.Background()
	s := NewMemory(0)
	if got, _ := s.RecentMessages(ctx, "!nope", 10); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if n, _ := s.UnreadCount(ctx, "!nope"); n != 0 {
		t.Fatalf("unread = %d", n)
	}
}
