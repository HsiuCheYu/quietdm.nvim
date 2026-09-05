// Command quietdmd is the quietdm daemon: it holds the connection to the chat
// network and serves Neovim frontends over a Unix socket.
//
// It draws nothing, notifies nobody, and keeps running when every editor is
// closed (docs/design/02-architecture.md).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/HsiuCheYu/quietdm.nvim/internal/config"
	"github.com/HsiuCheYu/quietdm.nvim/internal/ipc"
	"github.com/HsiuCheYu/quietdm.nvim/internal/sanitize"
	"github.com/HsiuCheYu/quietdm.nvim/internal/session"
	"github.com/HsiuCheYu/quietdm.nvim/internal/store"
	"github.com/HsiuCheYu/quietdm.nvim/internal/transport"
)

// version is stamped by the release build; it only ever appears in logs and in
// the ready event.
var version = "dev"

func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "quietdmd:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath = flag.String("config", config.DefaultConfigPath(), "path to config.toml")
		socketPath = flag.String("socket", "", "override the IPC socket path")
		printSock  = flag.Bool("print-socket", false, "print the resolved socket path and exit")
		verbose    = flag.Bool("v", false, "log at debug level")
		showVer    = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("quietdmd", version)
		return nil
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *socketPath != "" {
		cfg.Daemon.Socket = *socketPath
	}
	// The frontend resolves this path on its own, with no way to ask. Printing
	// it is how the two implementations get compared (scripts/e2e.sh) and how a
	// user finds out where the daemon thinks its socket is.
	if *printSock {
		fmt.Println(cfg.SocketPath())
		return nil
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	tr, err := newTransport(cfg, log, *verbose)
	if err != nil {
		return err
	}
	defer tr.Close()

	st, err := newStore(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	sess := session.New(tr, st, session.Options{
		Aliases: cfg.Aliases,
		Sanitize: sanitize.Options{
			StripEmoji: cfg.Sanitize.StripEmoji,
			MaxBody:    cfg.Sanitize.MaxBody,
		},
	})

	srv := ipc.NewServer(cfg.SocketPath(), "quietdmd/"+version, sess, log)
	sess.SetBroadcaster(srv)
	if err := srv.Listen(); err != nil {
		return err
	}
	defer srv.Close()
	log.Info("listening", "socket", srv.Addr(), "transport", cfg.Transport.Kind, "store", cfg.Daemon.Store)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errs := make(chan error, 2)
	go func() { errs <- srv.Serve(ctx) }()
	go func() {
		// A transport that simply runs out of events (the mock script ends)
		// must not take the daemon down with it; only a real failure does.
		if err := sess.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errs <- err
			return
		}
		log.Info("transport stopped")
	}()

	select {
	case <-ctx.Done():
		log.Info("shutting down")
		return nil
	case err := <-errs:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	}
}

// newStore opens the configured store. The SQLite one keeps history across
// restarts, which the higher exposure levels need; "memory" is for anyone who
// would rather leave no chat log on disk.
func newStore(cfg config.Config) (store.Store, error) {
	switch cfg.Daemon.Store {
	case "memory":
		return store.NewMemory(cfg.Daemon.HistoryCapacity), nil
	case "sqlite":
		return store.OpenSQLite(cfg.StateDBPath(), cfg.Daemon.HistoryCapacity)
	default:
		return nil, fmt.Errorf("unsupported store %q", cfg.Daemon.Store)
	}
}

func newTransport(cfg config.Config, log *slog.Logger, verbose bool) (transport.Transport, error) {
	switch cfg.Transport.Kind {
	case "mock":
		return transport.NewMock(cfg.Mock), nil
	case "matrix":
		token, err := cfg.MatrixToken()
		if err != nil {
			return nil, err
		}
		return transport.NewMatrix(transport.MatrixOptions{
			Homeserver:    cfg.Matrix.Homeserver,
			UserID:        cfg.Matrix.UserID,
			DeviceID:      cfg.Matrix.DeviceID,
			Token:         token,
			Encrypt:       cfg.Matrix.Encrypt,
			SessionDB:     cfg.MatrixSessionDBPath(),
			PickleKeyFile: cfg.PickleKeyPath(),
			Verbose:       verbose,
			Log:           log,
		})
	default:
		// Validate already rejected anything else; this keeps the switch
		// honest if a new kind is added without wiring it up.
		return nil, fmt.Errorf("unsupported transport %q", cfg.Transport.Kind)
	}
}
