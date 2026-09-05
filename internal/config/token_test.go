package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeToken(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTokenFromEnvironmentWins(t *testing.T) {
	t.Setenv(TokenEnv, "  syt_from_env  ")
	cfg := Default()
	cfg.Matrix.TokenFile = writeToken(t, "syt_from_file", 0o600)
	got, err := cfg.MatrixToken()
	if err != nil {
		t.Fatal(err)
	}
	if got != "syt_from_env" {
		t.Errorf("token = %q", got)
	}
}

func TestTokenFromFile(t *testing.T) {
	t.Setenv(TokenEnv, "")
	cfg := Default()
	cfg.Matrix.TokenFile = writeToken(t, "syt_from_file\n", 0o600)
	got, err := cfg.MatrixToken()
	if err != nil {
		t.Fatal(err)
	}
	if got != "syt_from_file" {
		t.Errorf("token = %q", got)
	}
}

// A token file the whole machine can read is not a smaller problem than a
// missing one, and carrying on would teach the user that it does not matter.
func TestTokenFileMustNotBeReadableByOthers(t *testing.T) {
	t.Setenv(TokenEnv, "")
	cfg := Default()
	cfg.Matrix.TokenFile = writeToken(t, "syt_from_file", 0o644)
	if _, err := cfg.MatrixToken(); err == nil {
		t.Fatal("a world-readable token file must be refused")
	}
}

func TestTokenMissingEntirely(t *testing.T) {
	t.Setenv(TokenEnv, "")
	cfg := Default()
	if _, err := cfg.MatrixToken(); err == nil {
		t.Fatal("want an error naming both ways to provide a token")
	}
	cfg.Matrix.TokenFile = filepath.Join(t.TempDir(), "absent")
	if _, err := cfg.MatrixToken(); err == nil {
		t.Fatal("a missing token file must be an error")
	}
	cfg.Matrix.TokenFile = writeToken(t, "   \n", 0o600)
	if _, err := cfg.MatrixToken(); err == nil {
		t.Fatal("an empty token file must be an error")
	}
}
