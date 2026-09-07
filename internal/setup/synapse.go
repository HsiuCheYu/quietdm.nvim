package setup

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	dockerstack "github.com/HsiuCheYu/quietdm.nvim/contrib/docker"
	"gopkg.in/yaml.v3"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/synapseadmin"
)

const homeserverReadyTimeout = 60 * time.Second

// materializeStack writes compose.yaml and, if it does not exist yet, a
// freshly generated .env into dir, and creates the data/ volume directories.
// It returns the Postgres password, generated once and reused on every later
// run so re-running `quietdmd setup` does not orphan the first database.
func materializeStack(dir string) (dbPassword string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	composePath := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composePath, dockerstack.ComposeYAML, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", composePath, err)
	}

	envPath := filepath.Join(dir, ".env")
	if existing, err := os.ReadFile(envPath); err == nil {
		if pw, ok := parseEnvVar(string(existing), "POSTGRES_PASSWORD"); ok && pw != "" {
			dbPassword = pw
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", envPath, err)
	}
	if dbPassword == "" {
		dbPassword, err = randomHex(16)
		if err != nil {
			return "", err
		}
		content := fmt.Sprintf("POSTGRES_PASSWORD=%s\n", dbPassword)
		if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
			return "", fmt.Errorf("write %s: %w", envPath, err)
		}
	}

	for _, sub := range []string{"postgres", "synapse", "bridge"} {
		if err := os.MkdirAll(filepath.Join(dir, "data", sub), 0o755); err != nil {
			return "", fmt.Errorf("create data/%s: %w", sub, err)
		}
	}
	return dbPassword, nil
}

// ensureSynapseConfig generates data/synapse/homeserver.yaml if it is not
// there yet, then patches it into the shape docs/self-host.md "一、產生
// Synapse 設定" describes by hand: Postgres instead of the default sqlite,
// the bridge's registration file wired in, and registration closed to
// everyone but this wizard.
func ensureSynapseConfig(ctx context.Context, dir, serverName, dbPassword string, out, errOut io.Writer) error {
	hsPath := filepath.Join(dir, "data", "synapse", "homeserver.yaml")
	if _, err := os.Stat(hsPath); os.IsNotExist(err) {
		cmd := []string{"run", "--rm",
			"-e", "SYNAPSE_SERVER_NAME=" + serverName,
			"-e", "SYNAPSE_REPORT_STATS=no",
			"synapse", "generate"}
		if err := dockerCompose(ctx, dir, out, errOut, cmd...); err != nil {
			return fmt.Errorf("generate synapse config: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", hsPath, err)
	}

	// Patching is idempotent (it only ever sets these keys to the same
	// values), so re-running quietdmd setup needs no separate "already
	// patched" check here.
	err := PatchYAMLFile(hsPath, func(root *yaml.Node) error {
		if err := SetPath(root, Str("psycopg2"), "database", "name"); err != nil {
			return err
		}
		args := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if err := SetPath(args, Str("synapse"), "user"); err != nil {
			return err
		}
		if err := SetPath(args, Str(dbPassword), "password"); err != nil {
			return err
		}
		if err := SetPath(args, Str("synapse"), "database"); err != nil {
			return err
		}
		if err := SetPath(args, Str("postgres"), "host"); err != nil {
			return err
		}
		if err := SetPath(args, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "5"}, "cp_min"); err != nil {
			return err
		}
		if err := SetPath(args, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "10"}, "cp_max"); err != nil {
			return err
		}
		if err := SetPath(root, args, "database", "args"); err != nil {
			return err
		}
		if err := SetPath(root, StrList("/appservices/registration.yaml"), "app_service_config_files"); err != nil {
			return err
		}
		return SetPath(root, Bool(false), "enable_registration")
	})
	if err != nil {
		return fmt.Errorf("patch %s: %w", hsPath, err)
	}
	return nil
}

// startSynapse brings up postgres and synapse and waits for the homeserver to
// answer, replacing "docker compose logs -f synapse # 等它說 listening".
func startSynapse(ctx context.Context, dir, homeserverURL string, out, errOut io.Writer) error {
	if err := dockerCompose(ctx, dir, out, errOut, "up", "-d", "postgres", "synapse"); err != nil {
		return err
	}
	if err := waitHTTPReady(ctx, homeserverURL+"/_matrix/client/versions", homeserverReadyTimeout); err != nil {
		return fmt.Errorf("%w (see docs/self-host.md \"已知會卡住的地方\")", err)
	}
	return nil
}

// registrationSharedSecret reads the secret Synapse's own `generate` step
// wrote into homeserver.yaml — the same one `register_new_matrix_user` needs,
// used here to call the admin HTTP API instead of shelling into the
// container interactively.
func registrationSharedSecret(dir string) (string, error) {
	hsPath := filepath.Join(dir, "data", "synapse", "homeserver.yaml")
	data, err := os.ReadFile(hsPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", hsPath, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parse %s: %w", hsPath, err)
	}
	secret, ok := GetString(doc.Content[0], "registration_shared_secret")
	if !ok || secret == "" {
		return "", fmt.Errorf("%s has no registration_shared_secret", hsPath)
	}
	return secret, nil
}

// registerAdmin creates the Matrix account quietdmd uses internally to talk
// to the homeserver. The username and password are random and never leave
// this process: nothing about this account is information the user needs to
// know or manage themselves.
func registerAdmin(ctx context.Context, homeserverURL, serverName, sharedSecret string) (username, password string, mxid id.UserID, err error) {
	cli, err := mautrix.NewClient(homeserverURL, "", "")
	if err != nil {
		return "", "", "", fmt.Errorf("matrix client: %w", err)
	}
	admin := synapseadmin.Client{Client: cli}

	username, err = randomUsername()
	if err != nil {
		return "", "", "", err
	}
	password, err = randomPassword()
	if err != nil {
		return "", "", "", err
	}

	_, err = admin.SharedSecretRegister(ctx, sharedSecret, synapseadmin.ReqSharedSecretRegister{
		Username:     username,
		Password:     password,
		Admin:        false,
		InhibitLogin: true,
	})
	if err != nil {
		return "", "", "", fmt.Errorf("register admin account: %w", err)
	}
	return username, password, id.NewUserID(username, serverName), nil
}

func randomUsername() (string, error) {
	suffix, err := randomHex(4)
	if err != nil {
		return "", err
	}
	return "qd-" + suffix, nil
}

func randomPassword() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func parseEnvVar(content, key string) (string, bool) {
	prefix := key + "="
	for _, line := range strings.Split(content, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			return v, true
		}
	}
	return "", false
}
