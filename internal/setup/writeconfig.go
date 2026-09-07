package setup

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"maunium.net/go/mautrix/id"
)

//go:embed templates/config.toml.tmpl
var configTemplate string

type configData struct {
	Homeserver string
	UserID     string
	TokenFile  string
	Aliases    map[string]string
}

// renderConfig fills in templates/config.toml.tmpl. It keeps the hand-written
// comments examples/matrix.toml carries — a plain TOML encoder would drop
// them all.
func renderConfig(homeserver string, userID id.UserID, tokenFile string, aliases map[string]string) (string, error) {
	tmpl, err := template.New("config.toml").Parse(configTemplate)
	if err != nil {
		return "", fmt.Errorf("parse config template: %w", err)
	}
	escapedAliases := make(map[string]string, len(aliases))
	for k, v := range aliases {
		escapedAliases[tomlEscape(k)] = tomlEscape(v)
	}
	data := configData{
		Homeserver: tomlEscape(homeserver),
		UserID:     tomlEscape(string(userID)),
		TokenFile:  tomlEscape(tokenFile),
		Aliases:    escapedAliases,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render config template: %w", err)
	}
	return buf.String(), nil
}

func tomlEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

// writeTokenFile writes token to path with mode 0600, matching what
// internal/config/token.go requires before it will read a token file back.
func writeTokenFile(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	// os.WriteFile leaves an existing file's mode alone; force it so a stale
	// wider-permission file left over from a previous run does not linger.
	return os.Chmod(path, 0o600)
}

// writeConfigFile writes content to path. If path already exists and force
// is false, it writes to "<path>.generated" instead and leaves the original
// untouched — it may carry hand-added [aliases] or [sanitize] entries worth
// keeping — and returns that sidecar path so the caller can tell the user to
// merge it by hand.
func writeConfigFile(path string, force bool, content string) (writtenPath string, err error) {
	if !force {
		if _, statErr := os.Stat(path); statErr == nil {
			sidecar := path + ".generated"
			if err := os.WriteFile(sidecar, []byte(content), 0o644); err != nil {
				return "", fmt.Errorf("write %s: %w", sidecar, err)
			}
			return sidecar, nil
		} else if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("stat %s: %w", path, statErr)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}
