// Package model holds the value types shared by every daemon layer.
//
// Keeping them in one dependency-free package lets transport, store, session
// and ipc talk about the same message without importing each other.
package model

// Message kinds. Anything that is not Text carries a placeholder body such as
// "[圖片]" — multimedia is never rendered (see docs/design/00-overview.md).
const (
	KindText    = "text"
	KindImage   = "image"
	KindAudio   = "audio"
	KindSticker = "sticker"
	KindOther   = "other"
)

// Message is one chat message, already normalised by the session layer.
//
// Body is plain text with emoji stripped, but NOT truncated: truncation is a
// presentation decision and different renderers have different widths.
type Message struct {
	Room    string `json:"room"`
	Event   string `json:"event"`
	Sender  string `json:"sender"`
	Display string `json:"display"`
	Body    string `json:"body"`
	TS      int64  `json:"ts"`
	Own     bool   `json:"own"`
	Kind    string `json:"kind"`
}

// Room is the state of one conversation as the frontend sees it.
type Room struct {
	Room    string `json:"room"`
	Display string `json:"display"`
	Unread  int    `json:"unread"`
	LastTS  int64  `json:"last_ts"`
}
