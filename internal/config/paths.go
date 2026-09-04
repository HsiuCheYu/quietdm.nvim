package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// SocketPath returns the Unix socket path, honouring an explicit override.
//
// Default: $XDG_RUNTIME_DIR/quietdm/sock, falling back to /run/user/$UID and
// then to the system temp dir when neither exists (docs/design/03-ipc-protocol.md).
func (c Config) SocketPath() string {
	if c.Daemon.Socket != "" {
		return c.Daemon.Socket
	}
	return filepath.Join(runtimeDir(), "quietdm", "sock")
}

func runtimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir
	}
	candidate := fmt.Sprintf("/run/user/%d", os.Getuid())
	if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
		return candidate
	}
	return os.TempDir()
}

// StateDir returns $XDG_STATE_HOME/quietdm, where the store keeps its files.
func StateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "quietdm")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "quietdm")
	}
	return filepath.Join(home, ".local", "state", "quietdm")
}

// DefaultConfigPath returns $XDG_CONFIG_HOME/quietdm/config.toml.
func DefaultConfigPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "quietdm", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "quietdm", "config.toml")
}
