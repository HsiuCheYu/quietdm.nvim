// Package ipc implements the NDJSON protocol between the daemon and the
// Neovim frontends (docs/design/03-ipc-protocol.md).
package ipc

import "github.com/HsiuCheYu/quietdm.nvim/internal/model"

// ProtoVersion is the protocol version this daemon speaks.
const ProtoVersion = 1

// MaxLine is the hard limit on one NDJSON line. Anything longer is a protocol
// error and costs the client its connection.
const MaxLine = 64 * 1024

// Command types (client -> daemon).
const (
	CmdHello    = "hello"
	CmdSend     = "send"
	CmdMarkRead = "mark_read"
	CmdHistory  = "history"
	CmdRooms    = "rooms"
)

// Event types (daemon -> client).
const (
	EvReady   = "ready"
	EvMessage = "message"
	EvRoom    = "room"
	EvRooms   = "rooms"
	EvHistory = "history"
	EvAck     = "ack"
	EvError   = "error"
)

// Error codes carried by an error event.
const (
	ErrBadProto    = "bad_proto"
	ErrBadRequest  = "bad_request"
	ErrUnknownRoom = "unknown_room"
	ErrOffline     = "offline"
	ErrInternal    = "internal"
)

// History limits.
const (
	DefaultHistoryLimit = 20
	MaxHistoryLimit     = 200
)

// Command is the union of every client message. Unused fields stay zero.
type Command struct {
	T      string `json:"t"`
	ID     string `json:"id,omitempty"`
	Proto  int    `json:"proto,omitempty"`
	Client string `json:"client,omitempty"`
	Room   string `json:"room,omitempty"`
	Body   string `json:"body,omitempty"`
	Event  string `json:"event,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// Ready answers hello.
type Ready struct {
	T         string `json:"t"`
	ID        string `json:"id,omitempty"`
	Proto     int    `json:"proto"`
	Daemon    string `json:"daemon"`
	Connected bool   `json:"connected"`
}

// Message is the only event the daemon pushes without being asked.
type Message struct {
	T string `json:"t"`
	model.Message
}

// Room reports a changed unread count.
type Room struct {
	T string `json:"t"`
	model.Room
}

// Rooms answers the rooms command.
type Rooms struct {
	T     string       `json:"t"`
	ID    string       `json:"id,omitempty"`
	Rooms []model.Room `json:"rooms"`
}

// History answers the history command, oldest message first.
type History struct {
	T        string          `json:"t"`
	ID       string          `json:"id,omitempty"`
	Room     string          `json:"room"`
	Messages []model.Message `json:"messages"`
}

// Ack reports that a command succeeded.
type Ack struct {
	T     string `json:"t"`
	ID    string `json:"id,omitempty"`
	Event string `json:"event,omitempty"`
}

// Error reports that a command failed. The frontend must never render one of
// these (invariant I3); send failures are the single exception.
type Error struct {
	T    string `json:"t"`
	ID   string `json:"id,omitempty"`
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

// NewReady and friends keep the "t" field out of every call site.
func NewReady(id string, connected bool, daemon string) Ready {
	return Ready{T: EvReady, ID: id, Proto: ProtoVersion, Daemon: daemon, Connected: connected}
}

func NewMessage(m model.Message) Message { return Message{T: EvMessage, Message: m} }

func NewRoom(r model.Room) Room { return Room{T: EvRoom, Room: r} }

func NewRooms(id string, rooms []model.Room) Rooms {
	if rooms == nil {
		rooms = []model.Room{}
	}
	return Rooms{T: EvRooms, ID: id, Rooms: rooms}
}

func NewHistory(id, room string, msgs []model.Message) History {
	if msgs == nil {
		msgs = []model.Message{}
	}
	return History{T: EvHistory, ID: id, Room: room, Messages: msgs}
}

func NewAck(id, event string) Ack { return Ack{T: EvAck, ID: id, Event: event} }

func NewError(id, code, msg string) Error {
	return Error{T: EvError, ID: id, Code: code, Msg: msg}
}
