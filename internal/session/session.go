// Package session sits between the transport and the IPC hub: it normalises
// messages, applies contact aliases, keeps the unread counters, and decides
// what gets broadcast to the frontends.
package session

import (
	"context"
	"strings"
	"sync"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
	"github.com/HsiuCheYu/quietdm.nvim/internal/sanitize"
	"github.com/HsiuCheYu/quietdm.nvim/internal/store"
	"github.com/HsiuCheYu/quietdm.nvim/internal/transport"
)

// Broadcaster receives the events every connected frontend should see.
// The IPC hub implements it; tests use a stub.
type Broadcaster interface {
	BroadcastMessage(m model.Message)
	BroadcastRoom(r model.Room)
}

// Options configures a Session.
type Options struct {
	// Aliases maps a raw user ID to the name shown on screen. Anything not
	// listed falls back to the localpart, which already looks like a git
	// author name.
	Aliases map[string]string
	// Sanitize controls emoji stripping and the body size cap.
	Sanitize sanitize.Options
}

// Session owns the daemon's view of every conversation.
type Session struct {
	tr    transport.Transport
	st    store.Store
	opts  Options
	bcast Broadcaster

	mu        sync.RWMutex
	rooms     map[string]*roomState
	order     []string
	connected bool
}

type roomState struct {
	display string
	lastTS  int64
}

// New builds a session. bcast may be nil while wiring up; use SetBroadcaster.
func New(tr transport.Transport, st store.Store, opts Options) *Session {
	return &Session{
		tr:    tr,
		st:    st,
		opts:  opts,
		rooms: make(map[string]*roomState),
	}
}

// SetBroadcaster installs the sink for broadcast events.
func (s *Session) SetBroadcaster(b Broadcaster) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bcast = b
}

func (s *Session) broadcaster() Broadcaster {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bcast
}

// Run pumps transport events until ctx is cancelled or the transport stops.
func (s *Session) Run(ctx context.Context) error {
	rooms, err := s.tr.Rooms(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for _, r := range rooms {
		s.ensureRoomLocked(r.ID, s.alias(r.Display))
	}
	s.mu.Unlock()

	events, err := s.tr.Start(ctx)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			s.handle(ctx, ev)
		}
	}
}

func (s *Session) handle(ctx context.Context, ev transport.Event) {
	switch ev.Type {
	case transport.EventConnected:
		s.mu.Lock()
		s.connected = ev.Connected
		s.mu.Unlock()
	case transport.EventMessage:
		s.ingest(ctx, ev.Message)
	}
}

// ingest normalises a message, stores it, and broadcasts both the message and
// the resulting room state.
func (s *Session) ingest(ctx context.Context, msg model.Message) {
	msg.Display = s.alias(msg.Sender)
	if msg.Kind == "" {
		msg.Kind = model.KindText
	}
	msg = sanitize.Message(msg, s.opts.Sanitize)

	if err := s.st.AppendMessage(ctx, msg); err != nil {
		return
	}

	s.mu.Lock()
	rs := s.ensureRoomLocked(msg.Room, msg.Display)
	if msg.TS > rs.lastTS {
		rs.lastTS = msg.TS
	}
	s.mu.Unlock()

	// A message the user sent themselves is already read.
	if msg.Own {
		_ = s.st.SetReadMarker(ctx, msg.Room, msg.Event)
	}

	if b := s.broadcaster(); b != nil {
		b.BroadcastMessage(msg)
		if r, err := s.roomView(ctx, msg.Room); err == nil {
			b.BroadcastRoom(r)
		}
	}
}

// ensureRoomLocked returns the room state, creating it if this is the first
// time the room is seen. Callers must hold s.mu.
func (s *Session) ensureRoomLocked(roomID, display string) *roomState {
	rs, ok := s.rooms[roomID]
	if !ok {
		rs = &roomState{display: display}
		s.rooms[roomID] = rs
		s.order = append(s.order, roomID)
		return rs
	}
	// Keep the first display name: for a one-to-one room it is the contact,
	// and later messages from the user themselves must not rename the room.
	if rs.display == "" {
		rs.display = display
	}
	return rs
}

// alias maps a raw user ID to the name that appears on screen.
func (s *Session) alias(id string) string {
	if id == "" {
		return ""
	}
	if name, ok := s.opts.Aliases[id]; ok && name != "" {
		return name
	}
	return localpart(id)
}

// localpart turns "@mia:example.org" into "mia". A bare name is returned
// unchanged, which is what the mock transport's room display names need.
func localpart(id string) string {
	s := strings.TrimPrefix(id, "@")
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return s
}

// Send delivers a message. The transport echoes it back, so the frontends
// learn about it through the normal message broadcast.
func (s *Session) Send(ctx context.Context, roomID, body string) (string, error) {
	if !s.Knows(roomID) {
		return "", transport.ErrUnknownRoom
	}
	return s.tr.Send(ctx, roomID, body)
}

// MarkRead advances the read marker and tells every frontend about the new
// unread count.
func (s *Session) MarkRead(ctx context.Context, roomID, eventID string) error {
	if err := s.st.SetReadMarker(ctx, roomID, eventID); err != nil {
		return err
	}
	if err := s.tr.MarkRead(ctx, roomID, eventID); err != nil {
		return err
	}
	if b := s.broadcaster(); b != nil {
		if r, err := s.roomView(ctx, roomID); err == nil {
			b.BroadcastRoom(r)
		}
	}
	return nil
}

// History returns the tail of a conversation, oldest first.
func (s *Session) History(ctx context.Context, roomID string, limit int) ([]model.Message, error) {
	if !s.Knows(roomID) {
		return nil, transport.ErrUnknownRoom
	}
	return s.st.RecentMessages(ctx, roomID, limit)
}

// Rooms returns every known conversation with its current unread count.
func (s *Session) Rooms(ctx context.Context) ([]model.Room, error) {
	s.mu.RLock()
	ids := make([]string, len(s.order))
	copy(ids, s.order)
	s.mu.RUnlock()

	out := make([]model.Room, 0, len(ids))
	for _, id := range ids {
		r, err := s.roomView(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Session) roomView(ctx context.Context, roomID string) (model.Room, error) {
	s.mu.RLock()
	rs, ok := s.rooms[roomID]
	var display string
	var lastTS int64
	if ok {
		display, lastTS = rs.display, rs.lastTS
	}
	s.mu.RUnlock()
	if !ok {
		return model.Room{}, transport.ErrUnknownRoom
	}
	unread, err := s.st.UnreadCount(ctx, roomID)
	if err != nil {
		return model.Room{}, err
	}
	return model.Room{Room: roomID, Display: display, Unread: unread, LastTS: lastTS}, nil
}

// Knows reports whether the room exists. Commands naming anything else are
// rejected: the daemon never accepts an arbitrary room ID from a frontend.
func (s *Session) Knows(roomID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.rooms[roomID]
	return ok
}

// Connected reports whether the transport currently has a working link.
func (s *Session) Connected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}
