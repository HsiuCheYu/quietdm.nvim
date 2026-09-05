// Package store persists conversation state for the daemon.
//
// Two implementations: SQLite, which keeps history across restarts, and an
// in-memory one for anyone who would rather leave no chat log on disk. Both
// satisfy the same interface, so the session layer never learns which one it
// is talking to.
package store

import (
	"context"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
)

// Store persists conversation state. Persistent implementations keep their
// files under the daemon's XDG state directory with 0600 permissions.
type Store interface {
	AppendMessage(ctx context.Context, m model.Message) error
	RecentMessages(ctx context.Context, roomID string, limit int) ([]model.Message, error)
	UnreadCount(ctx context.Context, roomID string) (int, error)
	SetReadMarker(ctx context.Context, roomID, eventID string) error
	Close() error
}
