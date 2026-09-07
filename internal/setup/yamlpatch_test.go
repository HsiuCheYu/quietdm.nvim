package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// fixture resembles a slice of a real Synapse-generated homeserver.yaml: it
// carries comments and unrelated keys that a patch must leave untouched.
const fixtureHomeserverYAML = `# This is a placeholder comment above an unrelated setting.
server_name: "quietdm.local"

# Configure the postgres database
database:
  name: sqlite3
  args:
    database: /data/homeserver.db

# The public-facing base URL for this server.
public_baseurl: http://localhost:8008/

enable_registration: true
`

func TestPatchYAMLFile_PreservesCommentsAndUntouchedKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "homeserver.yaml")
	if err := os.WriteFile(path, []byte(fixtureHomeserverYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	err := PatchYAMLFile(path, func(root *yaml.Node) error {
		if err := SetPath(root, Str("psycopg2"), "database", "name"); err != nil {
			return err
		}
		if err := SetPath(root, StrList("/appservices/registration.yaml"), "app_service_config_files"); err != nil {
			return err
		}
		return SetPath(root, Bool(false), "enable_registration")
	})
	if err != nil {
		t.Fatalf("PatchYAMLFile: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	if !strings.Contains(got, "This is a placeholder comment") {
		t.Errorf("comment was dropped:\n%s", got)
	}
	if !strings.Contains(got, "The public-facing base URL") {
		t.Errorf("second comment was dropped:\n%s", got)
	}
	if !strings.Contains(got, `public_baseurl: http://localhost:8008/`) {
		t.Errorf("untouched key was altered:\n%s", got)
	}
	if !strings.Contains(got, "name: psycopg2") {
		t.Errorf("database.name was not patched:\n%s", got)
	}
	if !strings.Contains(got, "enable_registration: false") {
		t.Errorf("enable_registration was not patched:\n%s", got)
	}
	if !strings.Contains(got, "/appservices/registration.yaml") {
		t.Errorf("app_service_config_files was not set:\n%s", got)
	}
}

func TestSetPath_CreatesIntermediateMappings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("top: yes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := PatchYAMLFile(path, func(root *yaml.Node) error {
		return SetPath(root, Str("admin"), "bridge", "permissions", "@me:quietdm.local")
	})
	if err != nil {
		t.Fatalf("PatchYAMLFile: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "@me:quietdm.local") || !strings.Contains(got, "admin") {
		t.Errorf("nested key not created as expected:\n%s", got)
	}
}

func TestGetStringAndGetBool_RoundTripThroughFindKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "encryption:\n  allow: true\n  default: false\nregistration_shared_secret: abc123\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var secret string
	var allow, missingOK bool
	err := PatchYAMLFile(path, func(root *yaml.Node) error {
		secret, _ = GetString(root, "registration_shared_secret")
		allow, _ = GetBool(root, "encryption", "allow")
		_, missingOK = GetString(root, "no", "such", "key")
		return nil
	})
	if err != nil {
		t.Fatalf("PatchYAMLFile: %v", err)
	}
	if secret != "abc123" {
		t.Errorf("secret = %q, want abc123", secret)
	}
	if !allow {
		t.Errorf("allow = false, want true")
	}
	if missingOK {
		t.Errorf("missing key reported present")
	}
}
