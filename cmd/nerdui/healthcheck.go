// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/pessom/nerdui/internal/config"
	"github.com/pessom/nerdui/internal/tlsutil"
)

const defaultHealthTimeout = 5 * time.Second

func newHealthcheckCmd() *cobra.Command {
	cfg := config.Default()
	timeout := defaultHealthTimeout
	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Probe the local nerdui server (exit code 0 when healthy)",
		Long: "Probe GET /healthz of the local nerdui server and exit with 0 when it answers 200,\n" +
			"otherwise 1. It reads the same --listen, --tls, --data-dir and --tls-cert flags and\n" +
			"NERDUI_* variables as serve, so in a container it works without arguments. Wildcard\n" +
			"listen addresses are probed via loopback. With TLS, the server must present exactly the\n" +
			"certificate nerdui serves from disk (pinned), validated for a name it contains.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := healthURL(cfg)
			if err != nil {
				return err
			}
			var tlsCfg *tls.Config
			if cfg.TLS {
				if tlsCfg, err = pinnedTLSConfig(cfg); err != nil {
					return fmt.Errorf("unhealthy: %w", err)
				}
			}
			if err := probe(cmd.Context(), url, tlsCfg, timeout); err != nil {
				return fmt.Errorf("unhealthy: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "ok")
			return err
		},
	}
	config.BindListenFlags(cmd.Flags(), &cfg)
	config.BindDataDirFlag(cmd.Flags(), &cfg)
	config.BindTLSCertFlag(cmd.Flags(), &cfg)
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

// pinnedTLSConfig trusts only the certificate nerdui serves (self-signed in the data dir or
// --tls-cert) and verifies it for a name it contains. The probe connects to loopback, so the
// name comes from the certificate instead of the URL.
func pinnedTLSConfig(cfg config.Config) (*tls.Config, error) {
	file := cfg.TLSCert
	if file == "" {
		file = tlsutil.SelfSignedCertFile(cfg.TLSDir())
	}
	data, err := os.ReadFile(file) //nolint:gosec // G304: operator-configured certificate path
	if err != nil {
		return nil, fmt.Errorf("read server certificate (use the same --data-dir/--tls-cert as the server): %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("%s: no PEM certificate found", file)
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	name, err := verificationName(leaf)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return &tls.Config{RootCAs: pool, ServerName: name, MinVersion: tls.VersionTLS12}, nil
}

func verificationName(c *x509.Certificate) (string, error) {
	switch {
	case slices.Contains(c.DNSNames, "localhost"):
		return "localhost", nil
	case len(c.DNSNames) > 0:
		return c.DNSNames[0], nil
	case len(c.IPAddresses) > 0:
		return c.IPAddresses[0].String(), nil
	default:
		return "", errors.New("certificate has no DNS or IP subject alternative names")
	}
}

func probe(ctx context.Context, url string, tlsCfg *tls.Config, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg, Proxy: nil}}
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
