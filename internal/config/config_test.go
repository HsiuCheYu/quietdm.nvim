package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("a missing config must not be an error: %v", err)
	}
	if cfg.Transport.Kind != "mock" || !cfg.Sanitize.StripEmoji {
		t.Errorf("cfg = %+v", cfg)
	}
	if len(cfg.Mock.Msgs) == 0 {
		t.Error("the built-in script should give the user something to look at")
	}
}

func TestLoadMergesOverDefaults(t *testing.T) {
	path := write(t, `
[daemon]
history_capacity = 42

[aliases]
"@mia:localhost" = "m.chen"

[sanitize]
strip_emoji = false
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Daemon.HistoryCapacity != 42 {
		t.Errorf("history_capacity = %d", cfg.Daemon.HistoryCapacity)
	}
	if cfg.Aliases["@mia:localhost"] != "m.chen" {
		t.Errorf("aliases = %v", cfg.Aliases)
	}
	// A false in the file must survive the merge.
	if cfg.Sanitize.StripEmoji {
		t.Error("strip_emoji = false was ignored")
	}
	// Untouched sections keep their defaults.
	if cfg.Transport.Kind != "mock" {
		t.Errorf("transport = %q", cfg.Transport.Kind)
	}
}

func TestLoadReplacesTheMockScript(t *testing.T) {
	path := write(t, `
[mock]
loop = false

[[mock.room]]
id = "!r:localhost"
display = "m.chen"

[[mock.message]]
room = "!r:localhost"
sender = "@mia:localhost"
body = "hi"
after = "2s"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Mock.Msgs) != 1 || cfg.Mock.Msgs[0].Body != "hi" {
		t.Fatalf("mock = %+v", cfg.Mock)
	}
	if cfg.Mock.Loop {
		t.Error("loop = false was ignored")
	}
	if got := cfg.Mock.Msgs[0].Delay(); got != 2*time.Second {
		t.Errorf("delay = %v", got)
	}
}

func TestDelayFallsBackOnGarbage(t *testing.T) {
	if got := (MockMessage{After: "soon"}).Delay(); got != 30*time.Second {
		t.Errorf("delay = %v", got)
	}
	if got := (MockMessage{}).Delay(); got != 30*time.Second {
		t.Errorf("delay = %v", got)
	}
}

func TestValidate(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("the defaults must validate: %v", err)
	}
	cfg.Transport.Kind = "matrix"
	if err := cfg.Validate(); err == nil {
		t.Error("matrix is not implemented until M2 and should say so")
	}
	cfg.Transport.Kind = "carrier-pigeon"
	if err := cfg.Validate(); err == nil {
		t.Error("an unknown transport must be rejected")
	}
	cfg = Default()
	cfg.Mock.Msgs = []MockMessage{{Body: "no room"}}
	if err := cfg.Validate(); err == nil {
		t.Error("a mock message without a room must be rejected")
	}
}

func TestSocketPath(t *testing.T) {
	cfg := Default()
	cfg.Daemon.Socket = "/tmp/explicit.sock"
	if got := cfg.SocketPath(); got != "/tmp/explicit.sock" {
		t.Errorf("got %q", got)
	}

	t.Setenv("XDG_RUNTIME_DIR", "/run/user/test")
	cfg.Daemon.Socket = ""
	if got := cfg.SocketPath(); got != "/run/user/test/quietdm/sock" {
		t.Errorf("got %q", got)
	}
}

func TestStateDirFollowsXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/state")
	if got := StateDir(); got != "/tmp/state/quietdm" {
		t.Errorf("got %q", got)
	}
}
