package setup

import (
	"os"
	"path/filepath"
	"testing"

	"maunium.net/go/mautrix/id"

	"github.com/HsiuCheYu/quietdm.nvim/internal/config"
)

func TestRenderConfig_RoundTripsThroughConfigLoadAndValidate(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	if err := writeTokenFile(tokenPath, "syt_test_token"); err != nil {
		t.Fatalf("writeTokenFile: %v", err)
	}
	fi, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %04o, want 0600", perm)
	}

	content, err := renderConfig("http://localhost:8008", id.UserID("@qd-abcd1234:quietdm.local"), tokenPath, map[string]string{
		"@instagram_1234567:quietdm.local": "m.chen",
	})
	if err != nil {
		t.Fatalf("renderConfig: %v", err)
	}

	configPath := filepath.Join(dir, "config.toml")
	written, err := writeConfigFile(configPath, false, content)
	if err != nil {
		t.Fatalf("writeConfigFile: %v", err)
	}
	if written != configPath {
		t.Fatalf("writeConfigFile wrote %q, want %q (no pre-existing file)", written, configPath)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("cfg.Validate: %v", err)
	}
	if cfg.Transport.Kind != "matrix" {
		t.Errorf("Transport.Kind = %q, want matrix", cfg.Transport.Kind)
	}
	if cfg.Matrix.Homeserver != "http://localhost:8008" {
		t.Errorf("Matrix.Homeserver = %q", cfg.Matrix.Homeserver)
	}
	if cfg.Matrix.UserID != "@qd-abcd1234:quietdm.local" {
		t.Errorf("Matrix.UserID = %q", cfg.Matrix.UserID)
	}
	if cfg.Matrix.TokenFile != tokenPath {
		t.Errorf("Matrix.TokenFile = %q, want %q", cfg.Matrix.TokenFile, tokenPath)
	}
	if got := cfg.Aliases["@instagram_1234567:quietdm.local"]; got != "m.chen" {
		t.Errorf("Aliases entry = %q, want m.chen", got)
	}

	token, err := cfg.MatrixToken()
	if err != nil {
		t.Fatalf("cfg.MatrixToken: %v", err)
	}
	if token != "syt_test_token" {
		t.Errorf("token = %q, want syt_test_token", token)
	}
}

func TestWriteConfigFile_DoesNotOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("# hand-edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	written, err := writeConfigFile(path, false, "# generated\n")
	if err != nil {
		t.Fatalf("writeConfigFile: %v", err)
	}
	if written != path+".generated" {
		t.Fatalf("written = %q, want sidecar path", written)
	}

	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != "# hand-edited\n" {
		t.Errorf("original config.toml was modified: %q", original)
	}

	sidecar, err := os.ReadFile(path + ".generated")
	if err != nil {
		t.Fatal(err)
	}
	if string(sidecar) != "# generated\n" {
		t.Errorf("sidecar content = %q", sidecar)
	}
}

func TestWriteConfigFile_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	written, err := writeConfigFile(path, true, "# new\n")
	if err != nil {
		t.Fatalf("writeConfigFile: %v", err)
	}
	if written != path {
		t.Fatalf("written = %q, want %q", written, path)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "# new\n" {
		t.Errorf("content = %q, want # new", out)
	}
}
