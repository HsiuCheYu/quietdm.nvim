package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOptions_SetDefaults_FillsEverythingWhenEmpty(t *testing.T) {
	var opts Options
	opts.setDefaults()

	if opts.DockerDir == "" {
		t.Error("DockerDir left empty")
	}
	if opts.ServerName != "quietdm.local" {
		t.Errorf("ServerName = %q, want quietdm.local", opts.ServerName)
	}
	if opts.ConfigPath == "" {
		t.Error("ConfigPath left empty")
	}
	if opts.TokenPath == "" {
		t.Error("TokenPath left empty")
	}
	if opts.Stdout == nil || opts.Stderr == nil || opts.Stdin == nil {
		t.Error("I/O streams left nil")
	}
}

func TestOptions_SetDefaults_PreservesExplicitValues(t *testing.T) {
	opts := Options{DockerDir: "/tmp/custom", ServerName: "example.org"}
	opts.setDefaults()

	if opts.DockerDir != "/tmp/custom" {
		t.Errorf("DockerDir = %q, want /tmp/custom", opts.DockerDir)
	}
	if opts.ServerName != "example.org" {
		t.Errorf("ServerName = %q, want example.org", opts.ServerName)
	}
}

func TestReadExistingToken(t *testing.T) {
	dir := t.TempDir()

	if _, ok := readExistingToken(filepath.Join(dir, "missing")); ok {
		t.Error("missing file reported a token")
	}

	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readExistingToken(empty); ok {
		t.Error("blank file reported a token")
	}

	withToken := filepath.Join(dir, "token")
	if err := os.WriteFile(withToken, []byte("syt_abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := readExistingToken(withToken)
	if !ok || got != "syt_abc123" {
		t.Errorf("readExistingToken = %q, %v; want syt_abc123, true", got, ok)
	}
}

func TestMaterializeStack_ReusesExistingPostgresPassword(t *testing.T) {
	dir := t.TempDir()

	pw1, err := materializeStack(dir)
	if err != nil {
		t.Fatalf("materializeStack (1st run): %v", err)
	}
	if pw1 == "" {
		t.Fatal("no password generated")
	}

	pw2, err := materializeStack(dir)
	if err != nil {
		t.Fatalf("materializeStack (2nd run): %v", err)
	}
	if pw2 != pw1 {
		t.Errorf("password changed across runs: %q != %q", pw1, pw2)
	}

	for _, sub := range []string{"postgres", "synapse", "bridge"} {
		if fi, err := os.Stat(filepath.Join(dir, "data", sub)); err != nil || !fi.IsDir() {
			t.Errorf("data/%s missing or not a directory", sub)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "compose.yaml")); err != nil {
		t.Errorf("compose.yaml missing: %v", err)
	}
}
