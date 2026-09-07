package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/HsiuCheYu/quietdm.nvim/internal/config"
	"github.com/HsiuCheYu/quietdm.nvim/internal/setup"
)

// runSetup implements `quietdmd setup`: it automates docs/self-host.md and
// docs/matrix-setup.md end to end, apart from the one secret only the user
// has (their Instagram login).
func runSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	dockerDir := fs.String("docker-dir", "", "where to put the self-hosting compose stack (default: "+config.SelfHostDir()+")")
	configPath := fs.String("config", config.DefaultConfigPath(), "path to write config.toml to")
	tokenPath := fs.String("token-file", "", "path to write the daemon's access token to")
	serverName := fs.String("server-name", "quietdm.local", "Synapse server name (the domain part of every MXID; arbitrary, since this homeserver never federates)")
	force := fs.Bool("force", false, "overwrite an existing config.toml instead of writing a .generated sidecar")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: quietdmd setup [flags]")
		fmt.Fprintln(os.Stderr, "\nAutomates docs/self-host.md and docs/matrix-setup.md: stands up the self-hosted")
		fmt.Fprintln(os.Stderr, "Synapse + Instagram bridge stack, connects the daemon to it, and walks you")
		fmt.Fprintln(os.Stderr, "through the one step only you can do — logging into Instagram.")
		fmt.Fprintln(os.Stderr, "\nflags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := setup.Options{
		DockerDir:  *dockerDir,
		ServerName: *serverName,
		ConfigPath: *configPath,
		TokenPath:  *tokenPath,
		Force:      *force,
	}
	return setup.Run(ctx, opts)
}
