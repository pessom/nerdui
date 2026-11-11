// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Command nerdui is a web UI for nerdctl/containerd.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/pessom/nerdui/internal/config"
	"github.com/pessom/nerdui/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nerdui:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return newRootCmd(os.Stdout, os.Stderr, os.LookupEnv).ExecuteContext(ctx)
}

type lookupEnv func(string) (string, bool)

// newRootCmd builds the command tree. Running `nerdui` without a subcommand starts the
// server, same as `nerdui serve`.
func newRootCmd(stdout, stderr io.Writer, env lookupEnv) *cobra.Command {
	cfg := config.Default()
	root := &cobra.Command{
		Use:   "nerdui",
		Short: "Web UI for nerdctl / containerd",
		Long: "nerdui is a web UI for managing containers, stacks, images, volumes and networks\n" +
			"on a containerd host through nerdctl.\n\n" +
			"Every flag can also be set with an environment variable NERDUI_<FLAG>, e.g.\n" +
			"--data-dir is NERDUI_DATA_DIR. Command-line flags take precedence.",
		Version:       version.Get().String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return config.ApplyEnv(cmd.Flags(), env)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context(), cfg, stderr)
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetVersionTemplate("{{.Version}}\n")
	config.BindServeFlags(root.Flags(), &cfg)

	root.AddCommand(newServeCmd(stderr), newVersionCmd(), newAdminCmd())
	return root
}

func newServeCmd(stderr io.Writer) *cobra.Command {
	cfg := config.Default()
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the nerdui server (default command)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context(), cfg, stderr)
		},
	}
	config.BindServeFlags(cmd.Flags(), &cfg)
	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, license and source information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version.Get().String())
			return err
		},
	}
}
