// Package transport abstracts where messages come from, so that Matrix is only
// one implementation among others (docs/design/02-architecture.md).
package transport

import (
	"context"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
)

// EventType distinguishes the things a transport can report.
type EventType string

const (
	// EventMessage carries an incoming or echoed-back message.
	EventMessage EventType = "message"
	// EventConnected reports a change in the link to the backing network.
	// The frontend must never render a connection problem (invariant I3), so
	// this only ever moves a flag.
	EventConnected EventType = "connected"
)

// Event is one thing that happened on the backing network.
type Event struct {
	Type      EventType
	Message   model.Message
	Connected bool
}

// Room is a conversation the transport knows about.
type Room struct {
	ID      string
	Display string
}

// Transport delivers messages from a backing chat network and accepts outgoing
// messages. Implementations must be safe for concurrent use.
type Transport interface {
	// Start begins receiving. Incoming events are pushed to the returned
	// channel until ctx is cancelled, at which point the channel is closed.
	Start(ctx context.Context) (<-chan Event, error)

	// Send delivers a message to a room and returns the assigned event ID.
	Send(ctx context.Context, roomID, body string) (string, error)

	// MarkRead advances the read receipt for a room.
	MarkRead(ctx context.Context, roomID, eventID string) error

	// Rooms lists the conversations currently available.
	Rooms(ctx context.Context) ([]Room, error)

	Close() error
}
