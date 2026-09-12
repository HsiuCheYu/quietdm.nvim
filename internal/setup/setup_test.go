package setup

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func TestMaterializeStack_WritesHostUIDAndGID(t *testing.T) {
	dir := t.TempDir()

	if _, err := materializeStack(dir); err != nil {
		t.Fatalf("materializeStack: %v", err)
	}

	env, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	uid, ok := parseEnvVar(string(env), "QUIETDM_UID")
	if !ok || uid != strconv.Itoa(os.Getuid()) {
		t.Errorf("QUIETDM_UID = %q, %v; want %d, true", uid, ok, os.Getuid())
	}
	gid, ok := parseEnvVar(string(env), "QUIETDM_GID")
	if !ok || gid != strconv.Itoa(os.Getgid()) {
		t.Errorf("QUIETDM_GID = %q, %v; want %d, true", gid, ok, os.Getgid())
	}
}

func TestMaterializeStack_PreservesExplicitUIDAndGID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("POSTGRES_PASSWORD=x\nQUIETDM_UID=1234\nQUIETDM_GID=5678\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := materializeStack(dir); err != nil {
		t.Fatalf("materializeStack: %v", err)
	}

	env, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if uid, _ := parseEnvVar(string(env), "QUIETDM_UID"); uid != "1234" {
		t.Errorf("QUIETDM_UID = %q, want unchanged 1234", uid)
	}
	if gid, _ := parseEnvVar(string(env), "QUIETDM_GID"); gid != "5678" {
		t.Errorf("QUIETDM_GID = %q, want unchanged 5678", gid)
	}
}

func TestMaterializeStack_AppendsToEnvWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("POSTGRES_PASSWORD=kept"), 0o600); err != nil {
		t.Fatal(err)
	}

	pw, err := materializeStack(dir)
	if err != nil {
		t.Fatalf("materializeStack: %v", err)
	}
	if pw != "kept" {
		t.Errorf("password = %q, want kept", pw)
	}

	env, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if got, _ := parseEnvVar(string(env), "POSTGRES_PASSWORD"); got != "kept" {
		t.Errorf("POSTGRES_PASSWORD = %q, want kept (the appended line glued onto it?)", got)
	}
	if uid, ok := parseEnvVar(string(env), "QUIETDM_UID"); !ok || uid != strconv.Itoa(os.Getuid()) {
		t.Errorf("QUIETDM_UID = %q, %v; want %d, true", uid, ok, os.Getuid())
	}
}

func TestMaterializeStack_ReplacesEmptyUIDInPlace(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("POSTGRES_PASSWORD=x\nQUIETDM_UID=\nQUIETDM_GID=\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if _, err := materializeStack(dir); err != nil {
			t.Fatalf("materializeStack: %v", err)
		}
	}

	env, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if uid, _ := parseEnvVar(string(env), "QUIETDM_UID"); uid != strconv.Itoa(os.Getuid()) {
		t.Errorf("QUIETDM_UID = %q, want %d", uid, os.Getuid())
	}
	if n := strings.Count(string(env), "QUIETDM_UID="); n != 1 {
		t.Errorf("QUIETDM_UID assigned %d times, want 1:\n%s", n, env)
	}
	if n := strings.Count(string(env), "QUIETDM_GID="); n != 1 {
		t.Errorf("QUIETDM_GID assigned %d times, want 1:\n%s", n, env)
	}
}
