// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/backup"
	"github.com/pessom/nerdui/internal/config"
	"github.com/pessom/nerdui/internal/store"
)

func newAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administrative commands that operate on the data directory (run on the host)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newResetPasswordCmd(), newResetMFACmd(), newRestoreCmd())
	return cmd
}

func newResetMFACmd() *cobra.Command {
	cfg := config.Default()
	cmd := &cobra.Command{
		Use:   "reset-mfa <username>",
		Short: "Turn off a user's two-factor authentication and revoke their sessions",
		Long: "Turn off two-factor authentication of a local user, delete their backup codes and\n" +
			"revoke all of their sessions, e.g. when the only administrator lost the phone and the\n" +
			"backup codes. If two-factor authentication is required, the user sets it up again at\n" +
			"the next sign-in. Requires direct access to the data directory (usually root).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.Validate(); err != nil {
				return err
			}
			if _, err := os.Stat(cfg.DataDir); err != nil {
				return fmt.Errorf("data dir: %w", err)
			}
			st, err := store.Open(cmd.Context(), cfg.DataDir)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = st.Close() }()
			if err := auth.ResetMFAOffline(cmd.Context(), st, args[0], time.Now()); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Two-factor authentication of %q turned off; all of their sessions were revoked.\n", args[0])
			return err
		},
	}
	config.BindDataDirFlag(cmd.Flags(), &cfg)
	return cmd
}

func newResetPasswordCmd() *cobra.Command {
	cfg := config.Default()
	var fromStdin bool
	cmd := &cobra.Command{
		Use:   "reset-password <username>",
		Short: "Set a new password for a local user and revoke their sessions",
		Long: "Set a new password for a local user, enable the account if it was disabled and\n" +
			"revoke all of its sessions.\n" +
			"Requires direct access to the data directory (usually root); use it when the admin\n" +
			"password is lost. The password is read from the terminal, or from the first line of\n" +
			"standard input with --password-stdin.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.Validate(); err != nil {
				return err
			}
			if _, err := os.Stat(cfg.DataDir); err != nil {
				return fmt.Errorf("data dir: %w", err)
			}
			password, err := readNewPassword(cmd.InOrStdin(), cmd.ErrOrStderr(), fromStdin)
			if err != nil {
				return err
			}
			st, err := store.Open(cmd.Context(), cfg.DataDir)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = st.Close() }()
			if err := auth.ResetPassword(cmd.Context(), st, args[0], password, time.Now()); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Password for %q changed; all of their sessions were revoked.\n", args[0])
			return err
		},
	}
	config.BindDataDirFlag(cmd.Flags(), &cfg)
	cmd.Flags().BoolVar(&fromStdin, "password-stdin", false, "read the new password from the first line of stdin")
	return cmd
}

// readNewPassword reads the password from stdin (--password-stdin) or prompts twice on a
// terminal without echo.
func readNewPassword(in io.Reader, prompt io.Writer, fromStdin bool) (string, error) {
	var password string
	if fromStdin {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read password: %w", err)
		}
		password = strings.TrimRight(line, "\r\n")
	} else {
		f, ok := in.(*os.File)
		if !ok || !term.IsTerminal(int(f.Fd())) { //nolint:gosec // fd fits in int
			return "", errors.New("stdin is not a terminal; use --password-stdin")
		}
		read := func(label string) (string, error) {
			_, _ = fmt.Fprint(prompt, label)
			b, err := term.ReadPassword(int(f.Fd())) //nolint:gosec // fd fits in int
			_, _ = fmt.Fprintln(prompt)
			return string(b), err
		}
		p1, err := read("New password: ")
		if err != nil {
			return "", err
		}
		p2, err := read("Repeat password: ")
		if err != nil {
			return "", err
		}
		if p1 != p2 {
			return "", errors.New("passwords do not match")
		}
		password = p1
	}
	if err := auth.ValidatePassword(password); err != nil {
		return "", err
	}
	return password, nil
}

func newRestoreCmd() *cobra.Command {
	cfg := config.Default()
	var fromStdin, force bool
	cmd := &cobra.Command{
		Use:   "restore <backup-file>",
		Short: "Restore nerdui's database, stacks and Git stacks from a backup",
		Long: "Restore a backup made under Settings → Backup (or a scheduled backup): the database\n" +
			"with users, settings and credentials, the managed stacks and the Git stacks. Stack paths\n" +
			"move to this host's --data-dir and --stacks-dir. Stop nerdui first\n" +
			"(systemctl stop nerdui). An existing database, stacks directory or Git directory is\n" +
			"only replaced with --force; the previous ones are kept as <name>.before-restore-<time>.\n" +
			"The password is read from the terminal, or from the first line of standard input with\n" +
			"--password-stdin.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.Validate(); err != nil {
				return err
			}
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close() //nolint:errcheck // read-only
			password, err := readPassword(cmd.InOrStdin(), cmd.ErrOrStderr(), fromStdin, "Backup password: ")
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			m, err := backup.Restore(cmd.Context(), bufio.NewReader(f), password, backup.Target{
				DataDir: cfg.DataDir, StacksDir: cfg.EffectiveStacksDir(), Force: force,
				Logf: func(format string, a ...any) { _, _ = fmt.Fprintf(out, format+"\n", a...) },
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(out, "Restored the backup of %s (nerdui %s) into %s. Start nerdui: systemctl start nerdui\n",
				m.Created.Local().Format(time.DateTime), m.Nerdui, cfg.DataDir)
			return err
		},
	}
	config.BindDataDirFlag(cmd.Flags(), &cfg)
	cmd.Flags().StringVar(&cfg.StacksDir, "stacks-dir", cfg.StacksDir, "directory of managed stacks (default <data-dir>/stacks)")
	cmd.Flags().BoolVar(&fromStdin, "password-stdin", false, "read the backup password from the first line of stdin")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing database, stacks and Git stacks")
	return cmd
}

// readPassword reads a password once from stdin (fromStdin) or from the terminal without echo.
func readPassword(in io.Reader, prompt io.Writer, fromStdin bool, label string) (string, error) {
	if fromStdin {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read password: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	f, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) { //nolint:gosec // fd fits in int
		return "", errors.New("stdin is not a terminal; use --password-stdin")
	}
	_, _ = fmt.Fprint(prompt, label)
	b, err := term.ReadPassword(int(f.Fd())) //nolint:gosec // fd fits in int
	_, _ = fmt.Fprintln(prompt)
	return string(b), err
}
