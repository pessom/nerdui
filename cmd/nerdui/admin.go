// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/pessom/nerdui/internal/config"
)

// errNotImplemented marks commands whose backing feature is not implemented yet.
var errNotImplemented = errors.New("not implemented yet")

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
	cmd := &cobra.Command{
		Use:   "reset-password <username>",
		Short: "Set a new password for a local user and revoke their sessions",
		Long: "Set a new password for a local user and revoke all of their sessions.\n" +
			"Requires direct access to the data directory; use it when the admin password is lost.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := cfg.Validate(); err != nil {
				return err
			}
			// Local users and sessions are implemented in #121 / #74.
			return errors.Join(errNotImplemented, errors.New("local users are added in issue #121"))
		},
	}
	config.BindDataDirFlag(cmd.Flags(), &cfg)
	return cmd
}
