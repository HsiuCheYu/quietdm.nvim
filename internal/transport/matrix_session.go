package transport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"

	// The same pure-Go driver the message store uses.
	_ "modernc.org/sqlite"
)

// openSessionDB opens the database that holds the sync position and, when
// encryption is on, the Olm/Megolm sessions. Same directory and same 0600
// permissions as the message store: it holds decryption keys, so it is the last
// file on the machine that should be world-readable.
func openSessionDB(path string) (*dbutil.Database, error) {
	if path == "" {
		return nil, errors.New("matrix: no session database configured")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("matrix: session dir: %w", err)
		}
	}
	// Create the file here rather than letting SQLite do it, so it never
	// exists with wider permissions even for an instant.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("matrix: session db: %w", err)
	}
	f.Close()

	// Foreign keys and WAL are what mautrix-go's own schemas expect, and
	// _txlock=immediate keeps a write transaction from starting as a reader
	// and having to upgrade halfway through.
	raw, err := sql.Open("sqlite", path+
		"?_txlock=immediate"+
		"&_pragma=busy_timeout(5000)"+
		"&_pragma=foreign_keys(1)"+
		"&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("matrix: session db: %w", err)
	}
	raw.SetMaxOpenConns(1)
	db, err := dbutil.NewWithDB(raw, "sqlite3")
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("matrix: session db: %w", err)
	}
	return db, nil
}

// syncStore remembers where /sync left off, so a restarted daemon picks up the
// messages it missed instead of replaying the whole conversation.
type syncStore struct {
	db *sql.DB
}

var _ mautrix.SyncStore = (*syncStore)(nil)

func newSyncStore(db *dbutil.Database) (*syncStore, error) {
	_, err := db.RawDB.Exec(`
		CREATE TABLE IF NOT EXISTS quietdm_sync (
			user_id    TEXT PRIMARY KEY,
			filter_id  TEXT NOT NULL DEFAULT '',
			next_batch TEXT NOT NULL DEFAULT ''
		)`)
	if err != nil {
		return nil, fmt.Errorf("matrix: sync store: %w", err)
	}
	return &syncStore{db: db.RawDB}, nil
}

func (s *syncStore) SaveFilterID(ctx context.Context, userID id.UserID, filterID string) error {
	return s.set(ctx, userID, "filter_id", filterID)
}

func (s *syncStore) SaveNextBatch(ctx context.Context, userID id.UserID, nextBatch string) error {
	return s.set(ctx, userID, "next_batch", nextBatch)
}

func (s *syncStore) LoadFilterID(ctx context.Context, userID id.UserID) (string, error) {
	return s.get(ctx, userID, "filter_id")
}

func (s *syncStore) LoadNextBatch(ctx context.Context, userID id.UserID) (string, error) {
	return s.get(ctx, userID, "next_batch")
}

// set writes one column. The column name is never user input — it comes from
// the four methods above — so interpolating it is safe here.
func (s *syncStore) set(ctx context.Context, userID id.UserID, column, value string) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO quietdm_sync (user_id, %[1]s) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET %[1]s = excluded.%[1]s`, column),
		string(userID), value)
	if err != nil {
		return fmt.Errorf("matrix: save %s: %w", column, err)
	}
	return nil
}

func (s *syncStore) get(ctx context.Context, userID id.UserID, column string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM quietdm_sync WHERE user_id = ?`, column),
		string(userID)).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("matrix: load %s: %w", column, err)
	}
	return value, nil
}
