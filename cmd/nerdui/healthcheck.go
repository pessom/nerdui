// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/pessom/nerdui/internal/config"
)

const defaultHealthTimeout = 5 * time.Second

func newHealthcheckCmd() *cobra.Command {
	cfg := config.Default()
	timeout := defaultHealthTimeout
	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Probe the local nerdui server (exit code 0 when healthy)",
		Long: "Probe GET /healthz of the local nerdui server and exit with 0 when it answers 200,\n" +
			"otherwise 1. It reads the same --listen/--tls flags and NERDUI_* variables as serve,\n" +
			"so in a container it works without arguments. Wildcard listen addresses are probed via\n" +
			"loopback. The server certificate is not verified: this is a local probe of nerdui's own\n" +
			"server, whose certificate does not cover the loopback address.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := healthURL(cfg)
			if err != nil {
				return err
			}
			if err := probe(cmd.Context(), url, timeout); err != nil {
				return fmt.Errorf("unhealthy: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "ok")
			return err
		},
	}
	config.BindListenFlags(cmd.Flags(), &cfg)
	cmd.Flags().DurationVar(&timeout, "timeout", timeout, "probe timeout")
	return cmd
}

// healthURL derives the probe URL from the listen address; wildcard hosts become loopback.
func healthURL(cfg config.Config) (string, error) {
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return "", fmt.Errorf("--listen %q: %w", cfg.Listen, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
		if ip != nil && ip.To4() == nil {
			host = "::1"
		}
	}
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, port) + "/healthz", nil
}

func probe(ctx context.Context, url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{
		// Loopback probe of our own server: its certificate does not cover 127.0.0.1.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // G402: local self-probe
		Proxy:           nil,
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", url, resp.Status)
	}
	return nil
}
