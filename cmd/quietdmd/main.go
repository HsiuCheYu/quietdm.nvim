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
	if err := cfg.Validate(); err != nil {
		return err
	}

	tr, err := newTransport(cfg)
	if err != nil {
		return err
	}
	defer tr.Close()

	st := store.NewMemory(cfg.Daemon.HistoryCapacity)
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
	log.Info("listening", "socket", srv.Addr(), "transport", cfg.Transport.Kind)

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

func newTransport(cfg config.Config) (transport.Transport, error) {
	switch cfg.Transport.Kind {
	case "mock":
		return transport.NewMock(cfg.Mock), nil
	default:
		// Validate already rejected anything else; this keeps the switch
		// honest if a new kind is added without wiring it up.
		return nil, fmt.Errorf("unsupported transport %q", cfg.Transport.Kind)
	}
}
