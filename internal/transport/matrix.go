package transport

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
)

// Backoff bounds for the reconnect loop. A homeserver that is down comes back
// on its own schedule, so there is no point hammering it; but the user must not
// wait minutes for messages once it does.
const (
	defaultMinBackoff = 2 * time.Second
	defaultMaxBackoff = time.Minute
)

// pickleKeySize is the length of the key that encrypts the Olm/Megolm sessions
// at rest, as recommended by mautrix-go.
const pickleKeySize = 32

// MatrixOptions configures the Matrix transport.
type MatrixOptions struct {
	Homeserver string
	UserID     string
	// DeviceID may be empty: the homeserver knows which device the access
	// token belongs to, and /whoami will say so.
	DeviceID string
	Token    string
	// Encrypt turns on Olm/Megolm. Without it the daemon cannot read a single
	// message in an encrypted room, which is every room a bridge creates.
	Encrypt bool
	// SessionDB holds the sync position and, when Encrypt is set, the crypto
	// store. It sits next to the message store with the same permissions.
	SessionDB string
	// PickleKeyFile holds the key that encrypts the crypto store at rest. It is
	// created on first use if it does not exist.
	PickleKeyFile string
	// Verbose lets mautrix-go's own logging through to stderr.
	Verbose bool
	Log     *slog.Logger

	// Backoff bounds, swappable so tests do not wait in real time.
	MinBackoff time.Duration
	MaxBackoff time.Duration
}

// Matrix talks to a Matrix homeserver. The bridge to Instagram or Messenger
// sits on the far side of the homeserver, so as far as this code is concerned
// there is only ever Matrix (docs/design/02-architecture.md).
type Matrix struct {
	opts MatrixOptions
	log  *slog.Logger

	cli    *mautrix.Client
	syncer *quietSyncer
	crypto *cryptohelper.CryptoHelper
	db     *dbutil.Database

	sink *eventSink

	closeOnce sync.Once

	// sawConnected records that the link came up during the current attempt,
	// so the reconnect backoff starts over rather than climbing forever.
	sawConnected atomic.Bool

	mu        sync.Mutex
	connected bool
	rooms     []Room
}

// NewMatrix builds the transport. It performs no network access: the daemon
// must be able to start before the homeserver is reachable.
func NewMatrix(opts MatrixOptions) (*Matrix, error) {
	if opts.Homeserver == "" {
		return nil, errors.New("matrix: no homeserver configured")
	}
	if opts.UserID == "" {
		return nil, errors.New("matrix: no user_id configured")
	}
	if opts.Token == "" {
		return nil, errors.New("matrix: no access token (set $QUIETDM_TOKEN or token_file)")
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.MinBackoff <= 0 {
		opts.MinBackoff = defaultMinBackoff
	}
	if opts.MaxBackoff < opts.MinBackoff {
		opts.MaxBackoff = max(defaultMaxBackoff, opts.MinBackoff)
	}

	cli, err := mautrix.NewClient(opts.Homeserver, id.UserID(opts.UserID), opts.Token)
	if err != nil {
		return nil, fmt.Errorf("matrix: %w", err)
	}
	cli.DeviceID = id.DeviceID(opts.DeviceID)
	cli.Log = zerolog.Nop()
	if opts.Verbose {
		cli.Log = zerolog.New(zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
			w.Out = os.Stderr
		})).With().Timestamp().Logger()
	}

	m := &Matrix{
		opts: opts,
		log:  opts.Log,
		cli:  cli,
		sink: newEventSink(16),
	}

	db, err := openSessionDB(opts.SessionDB)
	if err != nil {
		return nil, err
	}
	m.db = db

	store, err := newSyncStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	// Installing our own sync store before the crypto helper runs keeps the
	// sync position ours whether or not encryption is on: the helper only
	// takes over a store that is nil or in-memory.
	cli.Store = store

	m.syncer = newQuietSyncer(m)
	cli.Syncer = m.syncer
	return m, nil
}

// Start begins delivering events. It does not wait for the homeserver: systemd
// will happily start the daemon and the homeserver in the same second, and a
// daemon that dies because the network is not up yet is a daemon the user has
// to babysit.
func (m *Matrix) Start(ctx context.Context) (<-chan Event, error) {
	go m.run(ctx)
	return m.sink.events(), nil
}

// run brings the connection up and keeps it up, backing off between attempts.
// A connection problem never reaches the screen; it only moves the connected
// flag (invariant I3).
func (m *Matrix) run(ctx context.Context) {
	defer m.sink.close()
	backoff := m.opts.MinBackoff
	for ctx.Err() == nil {
		err := m.connect(ctx)
		if ctx.Err() != nil {
			return
		}
		m.setConnected(ctx, false)
		if errors.Is(err, mautrix.MUnknownToken) {
			// A rejected token will be rejected again a second later. Stop,
			// and let the daemon keep serving with connected = false.
			m.log.Error("matrix: access token rejected, giving up", "err", err)
			return
		}
		if m.sawConnected.Swap(false) {
			backoff = m.opts.MinBackoff
		}
		m.log.Debug("matrix: link down, retrying", "err", err, "in", backoff)
		if !sleepCtx(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, m.opts.MaxBackoff)
	}
}

// connect performs the setup that needs a reachable homeserver and then syncs
// until something goes wrong. Both halves are retried by the caller.
func (m *Matrix) connect(ctx context.Context) error {
	if m.cli.DeviceID == "" {
		// The device ID has to match the one the access token was issued to,
		// or the homeserver hands the crypto layer keys for the wrong device.
		whoami, err := m.cli.Whoami(ctx)
		if err != nil {
			return fmt.Errorf("matrix: whoami: %w", err)
		}
		m.cli.DeviceID = whoami.DeviceID
		m.log.Debug("matrix device discovered", "device", whoami.DeviceID)
	}
	if m.opts.Encrypt && m.crypto == nil {
		if err := m.startCrypto(ctx); err != nil {
			return err
		}
	}
	return m.cli.SyncWithContext(ctx)
}

// startCrypto wires up Olm/Megolm. The keys live in the same file as the sync
// position, with the same 0600 permissions as everything else the daemon
// writes (docs/design/02-architecture.md section 7).
func (m *Matrix) startCrypto(ctx context.Context) error {
	key, err := loadOrCreatePickleKey(m.opts.PickleKeyFile)
	if err != nil {
		return err
	}
	helper, err := cryptohelper.NewCryptoHelper(m.cli, key, m.db)
	if err != nil {
		return fmt.Errorf("matrix: crypto: %w", err)
	}
	if err := helper.Init(ctx); err != nil {
		return fmt.Errorf("matrix: crypto: %w", err)
	}
	m.crypto = helper
	// Setting this is what makes outgoing messages encrypted in rooms that
	// expect it; without it the daemon would quietly send plaintext.
	m.cli.Crypto = helper
	return nil
}

// Send delivers a message. The homeserver echoes it back through /sync, so the
// frontends learn about it the same way they learn about anything else.
func (m *Matrix) Send(ctx context.Context, roomID, body string) (string, error) {
	resp, err := m.cli.SendMessageEvent(ctx, id.RoomID(roomID), event.EventMessage,
		&event.MessageEventContent{MsgType: event.MsgText, Body: body})
	if err != nil {
		return "", fmt.Errorf("matrix: send: %w", err)
	}
	return string(resp.EventID), nil
}

// MarkRead advances the read receipt on the homeserver, so the conversation
// looks the same from the other side as it would from a phone.
func (m *Matrix) MarkRead(ctx context.Context, roomID, eventID string) error {
	if eventID == "" {
		return nil
	}
	if err := m.cli.MarkRead(ctx, id.RoomID(roomID), id.EventID(eventID)); err != nil {
		return fmt.Errorf("matrix: mark read: %w", err)
	}
	return nil
}

// Rooms lists the joined conversations. An unreachable homeserver is not an
// error here: the daemon starts anyway, and a room the user actually talks in
// arrives with its first message. Failing instead would take the whole daemon
// down for a homeserver that is thirty seconds late.
func (m *Matrix) Rooms(ctx context.Context) ([]Room, error) {
	resp, err := m.cli.JoinedRooms(ctx)
	if err != nil {
		m.log.Debug("matrix: cannot list rooms yet", "err", err)
		return m.cachedRooms(), nil
	}
	out := make([]Room, 0, len(resp.JoinedRooms))
	for _, roomID := range resp.JoinedRooms {
		out = append(out, Room{ID: string(roomID), Display: m.roomDisplay(ctx, roomID)})
	}
	m.mu.Lock()
	m.rooms = out
	m.mu.Unlock()
	return out, nil
}

func (m *Matrix) cachedRooms() []Room {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Room(nil), m.rooms...)
}

// roomDisplay names a conversation. For the one-to-one rooms this tool is built
// around, the room is the contact, so it is named after them as a raw user ID —
// the session layer then applies the alias map and falls back to the localpart,
// which already reads like a git author name.
//
// A group room is named after its m.room.name, and an unnamed one falls back to
// its room ID. Groups do not fit in a single line of blame text anyway; see
// docs/design/05-roadmap.md.
func (m *Matrix) roomDisplay(ctx context.Context, roomID id.RoomID) string {
	members, err := m.cli.JoinedMembers(ctx, roomID)
	if err == nil && len(members.Joined) <= 2 {
		for uid := range members.Joined {
			if uid != m.cli.UserID {
				return string(uid)
			}
		}
	}
	var name event.RoomNameEventContent
	if err := m.cli.StateEvent(ctx, roomID, event.StateRoomName, "", &name); err == nil && name.Name != "" {
		return name.Name
	}
	return string(roomID)
}

// Close stops the client and releases the session database.
func (m *Matrix) Close() error {
	var err error
	m.closeOnce.Do(func() {
		if m.crypto != nil {
			err = m.crypto.Close()
		}
		if m.db != nil {
			if dbErr := m.db.Close(); err == nil {
				err = dbErr
			}
		}
	})
	return err
}

// onMessage turns a timeline event into the daemon's own message type.
func (m *Matrix) onMessage(ctx context.Context, evt *event.Event) {
	// The very first sync returns whatever the homeserver still holds. Those
	// messages are history, not news: announcing them would light up the
	// statusline for a conversation the user finished days ago.
	if since, _ := ctx.Value(mautrix.SyncTokenContextKey).(string); since == "" {
		return
	}
	content := evt.Content.AsMessage()
	if content == nil {
		return
	}
	m.sink.send(ctx, Event{Type: EventMessage, Message: model.Message{
		Room:   string(evt.RoomID),
		Event:  string(evt.ID),
		Sender: string(evt.Sender),
		Body:   content.Body,
		// Matrix counts in milliseconds; the IPC protocol counts in Unix
		// seconds (docs/design/03-ipc-protocol.md). Getting this wrong makes
		// every message read "剛剛" forever.
		TS:   evt.Timestamp / 1000,
		Own:  evt.Sender == m.cli.UserID,
		Kind: kindOf(evt, content),
	}})
}

// kindOf maps a Matrix message type onto the daemon's kinds. Everything that is
// not text ends up as a placeholder: an image cannot be disguised as code.
func kindOf(evt *event.Event, content *event.MessageEventContent) string {
	if evt.Type == event.EventSticker {
		return model.KindSticker
	}
	switch content.MsgType {
	case event.MsgText, event.MsgNotice, event.MsgEmote:
		return model.KindText
	case event.MsgImage:
		return model.KindImage
	case event.MsgAudio, event.MsgVideo:
		return model.KindAudio
	default:
		return model.KindOther
	}
}

func (m *Matrix) setConnected(ctx context.Context, state bool) {
	if state {
		m.sawConnected.Store(true)
	}
	m.mu.Lock()
	changed := m.connected != state
	m.connected = state
	m.mu.Unlock()
	if changed {
		m.sink.send(ctx, Event{Type: EventConnected, Connected: state})
	}
}

// quietSyncer is the default syncer plus the connection bookkeeping: it notices
// that the link is up, that it went away, and how long to wait before trying
// again.
type quietSyncer struct {
	*mautrix.DefaultSyncer
	m *Matrix

	mu      sync.Mutex
	backoff time.Duration
}

func newQuietSyncer(m *Matrix) *quietSyncer {
	s := &quietSyncer{
		DefaultSyncer: mautrix.NewDefaultSyncer(),
		m:             m,
		backoff:       m.opts.MinBackoff,
	}
	s.OnSync(func(ctx context.Context, resp *mautrix.RespSync, since string) bool {
		s.mu.Lock()
		s.backoff = s.m.opts.MinBackoff
		s.mu.Unlock()
		m.setConnected(ctx, true)
		return true
	})
	s.OnEventType(event.EventMessage, m.onMessage)
	s.OnEventType(event.EventSticker, m.onMessage)
	return s
}

// OnFailedSync reports the outage and asks mautrix to retry after a growing
// delay, rather than letting one failed request tear the whole sync down. A
// rejected access token is the exception: retrying that just fails again.
func (s *quietSyncer) OnFailedSync(res *mautrix.RespSync, err error) (time.Duration, error) {
	// mautrix does not hand a context to this hook; the sink's own stop signal
	// is what keeps the send from outliving the transport.
	s.m.setConnected(context.Background(), false)
	if errors.Is(err, mautrix.MUnknownToken) {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	wait := s.backoff
	s.backoff = min(s.backoff*2, s.m.opts.MaxBackoff)
	return wait, nil
}

// loadOrCreatePickleKey reads the crypto store's encryption key, creating a
// random one on first use.
//
// The key sits next to the database it protects, so it stops nothing from
// anyone who can already read the state directory. What it does do is keep the
// Olm sessions out of a stray backup of the database file alone.
func loadOrCreatePickleKey(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("matrix: no pickle key file configured")
	}
	key, err := os.ReadFile(path)
	if err == nil && len(key) == pickleKeySize {
		return key, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("matrix: pickle key: %w", err)
	}
	if err == nil {
		return nil, fmt.Errorf("matrix: pickle key %s is %d bytes, expected %d", path, len(key), pickleKeySize)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("matrix: pickle key: %w", err)
		}
	}
	key = make([]byte, pickleKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("matrix: pickle key: %w", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("matrix: pickle key: %w", err)
	}
	return key, nil
}
