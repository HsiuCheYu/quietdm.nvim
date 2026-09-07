package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"maunium.net/go/mautrix/id"
)

// ensureBridgeConfig runs the bridge container's first self-terminating pass
// (which writes data/bridge/config.yaml) if that file does not exist yet,
// then patches it the way docs/self-host.md "三、產生 bridge 設定與註冊檔"
// describes doing by hand.
func ensureBridgeConfig(ctx context.Context, dir, serverName string, adminMXID id.UserID, out, errOut io.Writer) error {
	cfgPath := filepath.Join(dir, "data", "bridge", "config.yaml")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := dockerCompose(ctx, dir, out, errOut, "up", "bridge"); err != nil {
			return fmt.Errorf("generate bridge config: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", cfgPath, err)
	}

	err := PatchYAMLFile(cfgPath, func(root *yaml.Node) error {
		if err := SetPath(root, Str("http://synapse:8008"), "homeserver", "address"); err != nil {
			return err
		}
		if err := SetPath(root, Str(serverName), "homeserver", "domain"); err != nil {
			return err
		}
		if err := SetPath(root, Str("0.0.0.0"), "appservice", "hostname"); err != nil {
			return err
		}
		if err := SetPath(root, Str("admin"), "bridge", "permissions", string(adminMXID)); err != nil {
			return err
		}
		if err := SetPath(root, Bool(true), "encryption", "allow"); err != nil {
			return err
		}
		return SetPath(root, Bool(true), "encryption", "default")
	})
	if err != nil {
		return fmt.Errorf("patch %s: %w", cfgPath, err)
	}
	return nil
}

// ensureBridgeRegistration runs the bridge's second self-terminating pass
// (which writes data/bridge/registration.yaml) if it hasn't already, then
// restarts Synapse so it picks the file up. Skipping that restart is exactly
// the "bridge 起來但 Synapse 說 appservice 不存在" pitfall docs/self-host.md
// warns about.
func ensureBridgeRegistration(ctx context.Context, dir, homeserverURL string, out, errOut io.Writer) error {
	regPath := filepath.Join(dir, "data", "bridge", "registration.yaml")
	if _, err := os.Stat(regPath); os.IsNotExist(err) {
		if err := dockerCompose(ctx, dir, out, errOut, "up", "bridge"); err != nil {
			return fmt.Errorf("generate bridge registration: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", regPath, err)
	}
	if err := dockerCompose(ctx, dir, out, errOut, "restart", "synapse"); err != nil {
		return err
	}
	return waitHTTPReady(ctx, homeserverURL+"/_matrix/client/versions", homeserverReadyTimeout)
}

// startBridge brings the bridge up for real — its third and final pass.
func startBridge(ctx context.Context, dir string, out, errOut io.Writer) error {
	return dockerCompose(ctx, dir, out, errOut, "up", "-d", "bridge")
}
