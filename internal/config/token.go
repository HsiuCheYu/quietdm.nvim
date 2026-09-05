package config

import (
	"fmt"
	"os"
	"strings"
)

// TokenEnv is the environment variable the access token is read from.
const TokenEnv = "QUIETDM_TOKEN"

// MatrixToken returns the homeserver access token.
//
// It never comes from the config file: a config file gets copied into
// dotfile repositories, pasted into issues and read over shoulders, and an
// access token is the one thing here that grants somebody else the whole
// account (docs/design/02-architecture.md section 7).
func (c Config) MatrixToken() (string, error) {
	if token := strings.TrimSpace(os.Getenv(TokenEnv)); token != "" {
		return token, nil
	}
	if c.Matrix.TokenFile == "" {
		return "", fmt.Errorf("no access token: set $%s or [matrix] token_file", TokenEnv)
	}
	fi, err := os.Stat(c.Matrix.TokenFile)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	// Refusing here is deliberate. A token file anyone on the machine can read
	// is not a smaller problem than a missing one, and silently carrying on
	// would teach the user that the permissions do not matter.
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return "", fmt.Errorf("token file %s is mode %04o; run chmod 600 on it", c.Matrix.TokenFile, perm)
	}
	data, err := os.ReadFile(c.Matrix.TokenFile)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("token file %s is empty", c.Matrix.TokenFile)
	}
	return token, nil
}
