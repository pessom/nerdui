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
	"github.com/pessom/nerdui/internal/config"
	"github.com/pessom/nerdui/internal/store"
)

func newAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administrative commands that operate on the data directory (run on the host)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newResetPasswordCmd())
	return cmd
}

func newResetPasswordCmd() *cobra.Command {
	cfg := config.Default()
	var fromStdin bool
	cmd := &cobra.Command{
		Use:   "reset-password <username>",
		Short: "Set a new password for a local user and revoke their sessions",
		Long: "Set a new password for a local user and revoke all of their sessions.\n" +
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
