// Package store persists conversation state for the daemon.
//
// M1 ships the in-memory implementation only; SQLite arrives with M2 (see
// docs/design/05-roadmap.md). Both satisfy the same interface so the session
// layer never learns which one it is talking to.
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
