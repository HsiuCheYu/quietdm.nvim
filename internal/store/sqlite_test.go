package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
)

func TestSQLiteSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")

	s, err := OpenSQLite(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, msg("$1", false)); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, msg("$2", false)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetReadMarker(ctx, room, "$1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = OpenSQLite(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	got, err := s.RecentMessages(ctx, room, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Event != "$1" || got[1].Event != "$2" {
		t.Fatalf("history did not survive: %v", got)
	}
	// The read marker has to survive too, or every restart would report the
	// whole conversation as unread.
	if n, _ := s.UnreadCount(ctx, room); n != 1 {
		t.Fatalf("unread = %d, want 1", n)
	}
}

// Every field the frontend renders must come back exactly as it went in.
func TestSQLiteRoundTripsEveryField(t *testing.T) {
	ctx := context.Background()
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	want := model.Message{
		Room:    room,
		Event:   "$1",
		Sender:  "@mia:localhost",
		Display: "m.chen",
		Body:    "晚上要吃什麼",
		TS:      1757000000000,
		Own:     true,
		Kind:    model.KindText,
	}
	if err := s.AppendMessage(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.RecentMessages(ctx, room, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// The database holds plain chat text, so nobody else on the machine gets to
// read it (docs/design/02-architecture.md section 7).
func TestSQLitePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "state.db")
	s, err := OpenSQLite(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("state db mode = %o, want 600", perm)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("state dir mode = %o, want 700", perm)
	}
}

func TestSQLiteRejectsAnUnusableDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSQLite(filepath.Join(file, "state.db"), 0); err == nil {
		t.Fatal("opening a database under a regular file should fail")
	}
}
