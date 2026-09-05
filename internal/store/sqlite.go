package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	// The pure-Go SQLite driver: no cgo, so the daemon still cross-compiles
	// into a single static binary for the release builds.
	_ "modernc.org/sqlite"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
)

// schema keeps the arrival order in an explicit sequence rather than relying on
// timestamps: two messages can share a millisecond, and a homeserver may hand
// out a timestamp that runs backwards.
const schema = `
CREATE TABLE IF NOT EXISTS messages (
	seq     INTEGER PRIMARY KEY AUTOINCREMENT,
	room    TEXT    NOT NULL,
	event   TEXT    NOT NULL,
	sender  TEXT    NOT NULL,
	display TEXT    NOT NULL,
	body    TEXT    NOT NULL,
	ts      INTEGER NOT NULL,
	own     INTEGER NOT NULL,
	kind    TEXT    NOT NULL,
	UNIQUE(room, event)
);
CREATE INDEX IF NOT EXISTS messages_room_seq ON messages(room, seq);
CREATE TABLE IF NOT EXISTS read_markers (
	room  TEXT PRIMARY KEY,
	event TEXT NOT NULL
);
`

// SQLite is a file-backed Store: history survives a daemon restart, which the
// higher exposure levels need. Refetching context from the homeserver would
// make the user wait while staring at the screen, and staring is exactly the
// behaviour this project exists to avoid (docs/design/02-architecture.md).
//
// The file lives in the daemon's XDG state directory with 0600 permissions.
// It holds plain text: the covert model protects the shape of a message on
// screen, never its content at rest. Anyone who does not want a chat log on
// disk should choose the in-memory store instead.
type SQLite struct {
	db       *sql.DB
	capacity int
}

// OpenSQLite opens (and creates, if needed) the state database at path, keeping
// at most capacity messages per room. A capacity <= 0 selects DefaultCapacity.
func OpenSQLite(path string, capacity int) (*SQLite, error) {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("state dir: %w", err)
		}
	}
	// Create the file here rather than letting SQLite do it, so that it never
	// exists with wider permissions — not even for the instant between the
	// driver creating it and a chmod.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("state db: %w", err)
	}
	f.Close()

	// The journal mode is left at the default (DELETE) on purpose: WAL would
	// leave -wal and -shm files sitting next to the database, and a daemon
	// with one writer gains nothing from them.
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}
	// One connection: the daemon is the only user and serialising writes is
	// cheaper than handling SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("state db schema: %w", err)
	}
	return &SQLite{db: db, capacity: capacity}, nil
}

// AppendMessage adds a message, dropping the oldest one once the room is full.
// Duplicate event IDs are ignored: the transport may replay on reconnect.
func (s *SQLite) AppendMessage(ctx context.Context, m model.Message) error {
	res, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO messages (room, event, sender, display, body, ts, own, kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Room, m.Event, m.Sender, m.Display, m.Body, m.TS, m.Own, m.Kind)
	if err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return nil // a duplicate; nothing to trim
	}
	// Delete everything older than the capacity-th newest message. When the
	// room holds fewer than that, the subquery is NULL and the comparison
	// matches nothing.
	_, err = s.db.ExecContext(ctx, `
		DELETE FROM messages WHERE room = ? AND seq < (
			SELECT seq FROM messages WHERE room = ? ORDER BY seq DESC LIMIT 1 OFFSET ?
		)`, m.Room, m.Room, s.capacity-1)
	if err != nil {
		return fmt.Errorf("trim room: %w", err)
	}
	return nil
}

// RecentMessages returns the last limit messages, oldest first.
func (s *SQLite) RecentMessages(ctx context.Context, roomID string, limit int) ([]model.Message, error) {
	if limit <= 0 {
		limit = -1 // SQLite spells "no limit" as a negative one
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT room, event, sender, display, body, ts, own, kind
		FROM messages WHERE room = ? ORDER BY seq DESC LIMIT ?`, roomID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent messages: %w", err)
	}
	defer rows.Close()

	var out []model.Message
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(&m.Room, &m.Event, &m.Sender, &m.Display, &m.Body, &m.TS, &m.Own, &m.Kind); err != nil {
			return nil, fmt.Errorf("recent messages: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recent messages: %w", err)
	}
	// The query walks backwards from the newest so that LIMIT picks the tail;
	// callers want them the other way round.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// UnreadCount counts the messages after the read marker that the user did not
// send. A marker pointing at an event the store no longer holds counts every
// message it still has.
func (s *SQLite) UnreadCount(ctx context.Context, roomID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM messages
		WHERE room = ? AND own = 0 AND seq > COALESCE((
			SELECT m.seq FROM messages m
			JOIN read_markers r ON r.room = m.room AND r.event = m.event
			WHERE m.room = ?
		), 0)`, roomID, roomID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("unread count: %w", err)
	}
	return n, nil
}

// SetReadMarker advances the read position. An empty eventID marks the whole
// room as read; in a room with no messages it does nothing.
func (s *SQLite) SetReadMarker(ctx context.Context, roomID, eventID string) error {
	if eventID == "" {
		err := s.db.QueryRowContext(ctx,
			`SELECT event FROM messages WHERE room = ? ORDER BY seq DESC LIMIT 1`,
			roomID).Scan(&eventID)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return fmt.Errorf("set read marker: %w", err)
		}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO read_markers (room, event) VALUES (?, ?)
		ON CONFLICT(room) DO UPDATE SET event = excluded.event`, roomID, eventID)
	if err != nil {
		return fmt.Errorf("set read marker: %w", err)
	}
	return nil
}

// Close closes the database handle.
func (s *SQLite) Close() error { return s.db.Close() }
