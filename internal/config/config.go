// Package config loads the daemon's TOML configuration.
//
// Everything has a working default: running quietdmd with no config file at
// all starts the mock transport with a small scripted conversation, which is
// exactly what M1 needs (see docs/design/05-roadmap.md).
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the whole daemon configuration.
type Config struct {
	Daemon    Daemon            `toml:"daemon"`
	Transport Transport         `toml:"transport"`
	Matrix    Matrix            `toml:"matrix"`
	Aliases   map[string]string `toml:"aliases"`
	Sanitize  Sanitize          `toml:"sanitize"`
	Mock      Mock              `toml:"mock"`
}

// Daemon holds process-level settings.
type Daemon struct {
	// Socket overrides the default $XDG_RUNTIME_DIR/quietdm/sock path.
	Socket string `toml:"socket"`
	// HistoryCapacity is how many messages per room the store keeps.
	HistoryCapacity int `toml:"history_capacity"`
	// Store is "sqlite" (the default: history survives a restart) or
	// "memory" (nothing ever reaches the disk).
	Store string `toml:"store"`
	// StateDB overrides the default $XDG_STATE_HOME/quietdm/state.db path.
	StateDB string `toml:"state_db"`
}

// Transport selects where messages come from.
type Transport struct {
	// Kind is "mock" (M1) or "matrix" (M2).
	Kind string `toml:"kind"`
}

// Matrix holds homeserver settings. The access token is deliberately absent:
// it comes from $QUIETDM_TOKEN or from TokenFile, never from this file.
type Matrix struct {
	Homeserver string `toml:"homeserver"`
	UserID     string `toml:"user_id"`
	// DeviceID may be left empty: the homeserver knows which device issued
	// the access token, and the daemon asks it at startup.
	DeviceID  string `toml:"device_id"`
	TokenFile string `toml:"token_file"`
	// Encrypt turns on Olm/Megolm and defaults to true. Every room a bridge
	// creates is encrypted, so turning this off means reading nothing.
	Encrypt bool `toml:"encrypt"`
	// SessionDB overrides $XDG_STATE_HOME/quietdm/matrix.db, which holds the
	// sync position and the crypto store.
	SessionDB string `toml:"session_db"`
	// PickleKeyFile overrides $XDG_STATE_HOME/quietdm/pickle.key.
	PickleKeyFile string `toml:"pickle_key_file"`
}

// Sanitize controls the text filter applied before broadcasting.
type Sanitize struct {
	StripEmoji bool `toml:"strip_emoji"`
	// MaxBody caps a message body in bytes. This is a protocol safety net,
	// not presentation truncation — how much fits on screen is the renderer's
	// call, so the daemon never truncates for display.
	MaxBody int `toml:"max_body"`
}

// Mock configures the scripted transport used for M1.
type Mock struct {
	// Loop replays the script from the start once it runs out.
	Loop  bool          `toml:"loop"`
	Rooms []MockRoom    `toml:"room"`
	Msgs  []MockMessage `toml:"message"`
}

// MockRoom is one scripted conversation.
type MockRoom struct {
	ID      string `toml:"id"`
	Display string `toml:"display"`
}

// MockMessage is one scripted message.
type MockMessage struct {
	Room   string `toml:"room"`
	Sender string `toml:"sender"`
	Body   string `toml:"body"`
	Kind   string `toml:"kind"`
	Own    bool   `toml:"own"`
	// After is the delay before this message fires, measured from the
	// previous one (Go duration syntax, e.g. "20s").
	After string `toml:"after"`
}

// Delay parses After, defaulting to 30s when unset or malformed.
func (m MockMessage) Delay() time.Duration {
	if m.After == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(m.After)
	if err != nil || d < 0 {
		return 30 * time.Second
	}
	return d
}

// Default returns the configuration used when no file exists.
func Default() Config {
	return Config{
		Daemon:    Daemon{HistoryCapacity: 500, Store: "sqlite"},
		Transport: Transport{Kind: "mock"},
		Matrix:    Matrix{Encrypt: true},
		Aliases:   map[string]string{},
		Sanitize:  Sanitize{StripEmoji: true, MaxBody: 8192},
		Mock:      defaultMock(),
	}
}

// defaultMock is a short scripted conversation, enough to exercise every
// exposure level without writing a config file first.
func defaultMock() Mock {
	room := "!demo:localhost"
	return Mock{
		Loop:  true,
		Rooms: []MockRoom{{ID: room, Display: "m.chen"}},
		Msgs: []MockMessage{
			{Room: room, Sender: "@mia:localhost", Body: "晚上要吃什麼", After: "20s"},
			{Room: room, Sender: "@mia:localhost", Body: "還是老樣子那間拉麵？", After: "45s"},
			{Room: room, Sender: "@mia:localhost", Body: "七點我先去排隊", After: "60s"},
		},
	}
}

// Load reads path and fills in defaults for anything it omits. A missing file
// is not an error: the defaults are a usable configuration.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	// Decode into a fresh value so that a partially-specified [mock] section
	// replaces the built-in script instead of merging with it.
	var file Config
	md, err := toml.Decode(string(data), &file)
	if err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	out := merge(cfg, file)
	// Booleans need the metadata: a false in the file is indistinguishable
	// from an absent key once decoded.
	if md.IsDefined("sanitize", "strip_emoji") {
		out.Sanitize.StripEmoji = file.Sanitize.StripEmoji
	}
	if md.IsDefined("mock", "loop") {
		out.Mock.Loop = file.Mock.Loop
	}
	if md.IsDefined("matrix", "encrypt") {
		out.Matrix.Encrypt = file.Matrix.Encrypt
	}
	return out, nil
}

// mergeMatrix overlays the file's [matrix] section onto the defaults. Encrypt
// is handled by the caller, which has the metadata needed to tell a false in
// the file from an absent key.
func mergeMatrix(base, file Matrix) Matrix {
	out := base
	if file.Homeserver != "" {
		out.Homeserver = file.Homeserver
	}
	if file.UserID != "" {
		out.UserID = file.UserID
	}
	if file.DeviceID != "" {
		out.DeviceID = file.DeviceID
	}
	if file.TokenFile != "" {
		out.TokenFile = file.TokenFile
	}
	if file.SessionDB != "" {
		out.SessionDB = file.SessionDB
	}
	if file.PickleKeyFile != "" {
		out.PickleKeyFile = file.PickleKeyFile
	}
	return out
}

func merge(base, file Config) Config {
	out := base
	if file.Daemon.Socket != "" {
		out.Daemon.Socket = file.Daemon.Socket
	}
	if file.Daemon.HistoryCapacity > 0 {
		out.Daemon.HistoryCapacity = file.Daemon.HistoryCapacity
	}
	if file.Daemon.Store != "" {
		out.Daemon.Store = file.Daemon.Store
	}
	if file.Daemon.StateDB != "" {
		out.Daemon.StateDB = file.Daemon.StateDB
	}
	if file.Transport.Kind != "" {
		out.Transport.Kind = file.Transport.Kind
	}
	out.Matrix = mergeMatrix(out.Matrix, file.Matrix)
	if len(file.Aliases) > 0 {
		out.Aliases = file.Aliases
	}
	if file.Sanitize.MaxBody > 0 {
		out.Sanitize.MaxBody = file.Sanitize.MaxBody
	}
	if len(file.Mock.Rooms) > 0 || len(file.Mock.Msgs) > 0 {
		out.Mock = file.Mock
	}
	return out
}

// Validate reports configuration that cannot work.
func (c Config) Validate() error {
	switch c.Daemon.Store {
	case "sqlite", "memory":
	default:
		return fmt.Errorf("unknown store %q", c.Daemon.Store)
	}
	switch c.Transport.Kind {
	case "mock":
		// A [mock] section in the file replaces the built-in script wholesale,
		// so a config with messages but no rooms leaves the transport with an
		// empty room list — and every reply comes back "unknown_room" until
		// the first message happens to arrive. Say so at startup instead.
		declared := make(map[string]bool, len(c.Mock.Rooms))
		for _, r := range c.Mock.Rooms {
			if r.ID == "" {
				return fmt.Errorf("mock room %q has no id", r.Display)
			}
			declared[r.ID] = true
		}
		for _, m := range c.Mock.Msgs {
			if m.Room == "" {
				return fmt.Errorf("mock message %q has no room", m.Body)
			}
			if !declared[m.Room] {
				return fmt.Errorf("mock message %q names room %q, which has no [[mock.room]]", m.Body, m.Room)
			}
		}
	case "matrix":
		if c.Matrix.Homeserver == "" {
			return fmt.Errorf("transport matrix needs [matrix] homeserver")
		}
		if c.Matrix.UserID == "" {
			return fmt.Errorf("transport matrix needs [matrix] user_id")
		}
	default:
		return fmt.Errorf("unknown transport %q", c.Transport.Kind)
	}
	return nil
}
