package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"

	"github.com/HsiuCheYu/quietdm.nvim/internal/config"
)

// Options configures a run of `quietdmd setup`.
type Options struct {
	// DockerDir is where the compose stack, its .env and data/ volumes live.
	DockerDir string
	// ServerName becomes the domain part of every MXID this wizard creates.
	// Fixed rather than prompted for: docs/self-host.md notes the value is
	// arbitrary, since this homeserver never federates.
	ServerName string
	// ConfigPath is where config.toml is written.
	ConfigPath string
	// TokenPath is where the daemon's own access token is written (0600).
	TokenPath string
	// Force overwrites an existing config.toml instead of leaving it alone.
	Force bool

	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
}

func (o *Options) setDefaults() {
	if o.DockerDir == "" {
		o.DockerDir = config.SelfHostDir()
	}
	if o.ServerName == "" {
		o.ServerName = "quietdm.local"
	}
	if o.ConfigPath == "" {
		o.ConfigPath = config.DefaultConfigPath()
	}
	if o.TokenPath == "" {
		o.TokenPath = filepath.Join(config.StateDir(), "token")
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
}

// homeserverURL is fixed: this compose stack only ever binds Synapse to
// 127.0.0.1:8008 (contrib/docker/compose.yaml), and self-host.md's own
// "唯一的 client 是同一台機器上的 quietdmd" design means there is nothing to
// point it at instead.
const homeserverURL = "http://localhost:8008"

// Run drives the whole self-hosting + IG-login walkthrough that
// docs/self-host.md and docs/matrix-setup.md otherwise have the user do by
// hand. Every phase below is idempotent, so interrupting it (Ctrl-C, a
// crashed container, a failed image pull) and running `quietdmd setup` again
// resumes rather than starts over.
func Run(ctx context.Context, opts Options) error {
	opts.setDefaults()
	step := func(format string, args ...any) {
		fmt.Fprintf(opts.Stdout, format+"\n", args...)
	}

	if err := preflight(ctx); err != nil {
		return err
	}

	step("[ok] 準備 %s", opts.DockerDir)
	dbPassword, err := materializeStack(opts.DockerDir)
	if err != nil {
		return err
	}

	step("[ok] 產生／確認 Synapse 設定")
	if err := ensureSynapseConfig(ctx, opts.DockerDir, opts.ServerName, dbPassword, opts.Stdout, opts.Stderr); err != nil {
		return err
	}
	step("[ok] 啟動 Postgres 與 Synapse")
	if err := startSynapse(ctx, opts.DockerDir, homeserverURL, opts.Stdout, opts.Stderr); err != nil {
		return err
	}

	adminMXID, token, err := ensureDaemonAccount(ctx, opts)
	if err != nil {
		return err
	}

	step("[ok] 產生／確認 bridge 設定")
	if err := ensureBridgeConfig(ctx, opts.DockerDir, opts.ServerName, adminMXID, opts.Stdout, opts.Stderr); err != nil {
		return err
	}
	step("[ok] 產生／確認 bridge 註冊檔")
	if err := ensureBridgeRegistration(ctx, opts.DockerDir, homeserverURL, opts.Stdout, opts.Stderr); err != nil {
		return err
	}
	step("[ok] 啟動 bridge")
	if err := startBridge(ctx, opts.DockerDir, opts.Stdout, opts.Stderr); err != nil {
		return err
	}

	cli, err := mautrix.NewClient(homeserverURL, adminMXID, token)
	if err != nil {
		return fmt.Errorf("matrix client: %w", err)
	}

	botID := id.NewUserID(bridgeBotLocalpart, opts.ServerName)
	step("[ok] 跟 bridge bot 開房間")
	roomID, err := findOrCreateBridgeDM(ctx, cli, botID)
	if err != nil {
		return err
	}
	if err := runBridgeLogin(ctx, cli, roomID, botID, opts.Stdout, opts.Stdin); err != nil {
		return err
	}

	aliases := aliasSkeleton(ctx, cli, botID)

	content, err := renderConfig(homeserverURL, adminMXID, opts.TokenPath, aliases)
	if err != nil {
		return err
	}
	writtenPath, err := writeConfigFile(opts.ConfigPath, opts.Force, content)
	if err != nil {
		return err
	}

	printSummary(opts, writtenPath, len(aliases))
	return nil
}

// ensureDaemonAccount reuses an existing, still-valid token if one is on
// disk, and only registers a fresh internal admin account and logs in when
// there is none. The account's username and password are never written
// anywhere, so a failure between registering and logging in cannot be
// resumed onto the same account — a re-run just registers a new one. That is
// harmless: the orphaned account has no rooms and is not reachable from
// outside quietdmd setup.
func ensureDaemonAccount(ctx context.Context, opts Options) (id.UserID, string, error) {
	if token, ok := readExistingToken(opts.TokenPath); ok {
		if cli, err := mautrix.NewClient(homeserverURL, "", token); err == nil {
			if whoami, err := cli.Whoami(ctx); err == nil {
				fmt.Fprintln(opts.Stdout, "[skip] 已經有可用的 access token，跳過建立帳號")
				return whoami.UserID, token, nil
			}
		}
	}

	fmt.Fprintln(opts.Stdout, "[ok] 建立內部使用的 Matrix 帳號")
	secret, err := registrationSharedSecret(opts.DockerDir)
	if err != nil {
		return "", "", err
	}
	_, password, adminMXID, err := registerAdmin(ctx, homeserverURL, opts.ServerName, secret)
	if err != nil {
		return "", "", err
	}

	fmt.Fprintln(opts.Stdout, "[ok] 登入拿 quietdmd 專用的 device token")
	cli, err := mautrix.NewClient(homeserverURL, "", "")
	if err != nil {
		return "", "", fmt.Errorf("matrix client: %w", err)
	}
	resp, err := cli.Login(ctx, &mautrix.ReqLogin{
		Type: mautrix.AuthTypePassword,
		Identifier: mautrix.UserIdentifier{
			Type: mautrix.IdentifierTypeUser,
			User: adminMXID.Localpart(),
		},
		Password:                 password,
		InitialDeviceDisplayName: "quietdm",
	})
	if err != nil {
		return "", "", fmt.Errorf("login as daemon account: %w", err)
	}
	if err := writeTokenFile(opts.TokenPath, resp.AccessToken); err != nil {
		return "", "", err
	}
	return resp.UserID, resp.AccessToken, nil
}

func readExistingToken(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	token := trimNewline(string(data))
	if token == "" {
		return "", false
	}
	return token, true
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func printSummary(opts Options, configPath string, aliasCount int) {
	fmt.Fprintln(opts.Stdout)
	fmt.Fprintln(opts.Stdout, "完成。")
	fmt.Fprintf(opts.Stdout, "  設定檔：%s\n", configPath)
	fmt.Fprintf(opts.Stdout, "  token 檔：%s\n", opts.TokenPath)
	if aliasCount > 0 {
		fmt.Fprintf(opts.Stdout, "  自動找到 %d 個對話，已預先填進 [aliases]\n", aliasCount)
	}
	fmt.Fprintln(opts.Stdout, "接下來：")
	fmt.Fprintf(opts.Stdout, "  quietdmd -config %s -v\n", configPath)
	fmt.Fprintln(opts.Stdout, "  或照 contrib/systemd/quietdmd.service 設成長期跑的服務。")
	fmt.Fprintln(opts.Stdout, "  nvim 那邊記得自己在 setup{} 裡選一個 panic_key —— 這個沒辦法幫你決定。")
}
