package store

import (
	"context"
	"sync"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
)

// DefaultCapacity is how many messages per room the in-memory store keeps.
// L2/L3 only ever ask for the tail, so an unbounded log would waste memory for
// no benefit.
const DefaultCapacity = 500

// Memory is a bounded in-memory Store. It is safe for concurrent use.
type Memory struct {
	mu       sync.RWMutex
	capacity int
	rooms    map[string]*roomLog
}

type roomLog struct {
	messages []model.Message
	// readMarker is the event ID of the last message the user has seen.
	// Unread is derived from it rather than stored, so a mark_read arriving
	// out of order can never drive the count negative.
	readMarker string
}

// NewMemory returns an in-memory store keeping at most capacity messages per
// room. A capacity <= 0 selects DefaultCapacity.
func NewMemory(capacity int) *Memory {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Memory{capacity: capacity, rooms: make(map[string]*roomLog)}
}

func (m *Memory) log(roomID string) *roomLog {
	r, ok := m.rooms[roomID]
	if !ok {
		r = &roomLog{}
		m.rooms[roomID] = r
	}
	return r
}

// AppendMessage adds a message, dropping the oldest one once the room is full.
// Duplicate event IDs are ignored: the transport may replay on reconnect.
func (m *Memory) AppendMessage(_ context.Context, msg model.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.log(msg.Room)
	for _, existing := range r.messages {
		if existing.Event == msg.Event {
			return nil
		}
	}
	r.messages = append(r.messages, msg)
	if over := len(r.messages) - m.capacity; over > 0 {
		r.messages = append(r.messages[:0], r.messages[over:]...)
	}
	return nil
}

// RecentMessages returns the last limit messages, oldest first.
func (m *Memory) RecentMessages(_ context.Context, roomID string, limit int) ([]model.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, nil
	}
	if limit <= 0 || limit > len(r.messages) {
		limit = len(r.messages)
	}
	out := make([]model.Message, limit)
	copy(out, r.messages[len(r.messages)-limit:])
	return out, nil
}

// UnreadCount counts the messages after the read marker that the user did not
// send. A marker pointing at an event the store no longer holds counts every
// message it still has.
func (m *Memory) UnreadCount(_ context.Context, roomID string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return 0, nil
	}
	return countUnread(r), nil
}

func countUnread(r *roomLog) int {
	start := 0
	if r.readMarker != "" {
		for i, msg := range r.messages {
			if msg.Event == r.readMarker {
				start = i + 1
				break
			}
		}
	}
	n := 0
	for _, msg := range r.messages[start:] {
		if !msg.Own {
			n++
		}
	}
	return n
}

// SetReadMarker advances the read position. An empty eventID marks the whole
// room as read.
func (m *Memory) SetReadMarker(_ context.Context, roomID, eventID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.log(roomID)
	if eventID == "" {
		if len(r.messages) > 0 {
			r.readMarker = r.messages[len(r.messages)-1].Event
		}
		return nil
	}
	r.readMarker = eventID
	return nil
}

// Close releases nothing; it exists so callers can treat every Store alike.
func (m *Memory) Close() error { return nil }
