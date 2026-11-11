// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package config defines nerdui's runtime configuration: defaults, validation and
// filesystem preparation. Values come from command-line flags or NERDUI_* environment
// variables (see flags.go); precedence is flag > environment > default.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Defaults. See docs/architecture.md (Configuration).
const (
	DefaultListen            = ":9443"
	DefaultContainerdAddress = "/run/containerd/containerd.sock"
	DefaultNamespace         = "default"
	DefaultDataDir           = "/var/lib/nerdui"
	DefaultNerdctlPath       = "nerdctl"
	DefaultLogLevel          = "info"

	// DataDirMode is the permission of the data directory (ADR-0004).
	DataDirMode fs.FileMode = 0o700
)

// Config is the configuration of the nerdui server.
type Config struct {
	// Listen is the TCP address of the HTTP(S) server, e.g. ":9443" or "127.0.0.1:9443".
	Listen string
	// ContainerdAddress is the containerd socket path.
	ContainerdAddress string
	// Namespace is the default containerd namespace.
	Namespace string
	// DataDir holds nerdui's own state: database, TLS material, stacks (ADR-0004).
	DataDir string
	// StacksDir holds compose projects managed by nerdui; empty means <DataDir>/stacks (ADR-0008).
	StacksDir string
	// StacksScanDirs are extra directories scanned for existing compose projects (#25).
	StacksScanDirs []string
	// NerdctlPath is the host nerdctl binary used for hooks, logging and compose (ADR-0003).
	NerdctlPath string
	// SkipNerdctlVersionCheck disables the major.minor compatibility check (ADR-0011).
	SkipNerdctlVersionCheck bool
	// TLS enables HTTPS (default). Disable only behind a TLS-terminating reverse proxy.
	TLS bool
	// TLSCert and TLSKey are a user-provided PEM certificate and key; when both are empty a
	// self-signed certificate is generated in <DataDir>/tls (ADR-0005).
	TLSCert string
	TLSKey  string
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
}

// Default returns the configuration with all default values.
func Default() Config {
	return Config{
		Listen:            DefaultListen,
		ContainerdAddress: DefaultContainerdAddress,
		Namespace:         DefaultNamespace,
		DataDir:           DefaultDataDir,
		NerdctlPath:       DefaultNerdctlPath,
		TLS:               true,
		LogLevel:          DefaultLogLevel,
	}
}

// TLSDir is where the self-signed certificate is kept.
func (c Config) TLSDir() string { return filepath.Join(c.DataDir, "tls") }

// EffectiveStacksDir returns StacksDir or its default inside DataDir.
func (c Config) EffectiveStacksDir() string {
	if c.StacksDir != "" {
		return c.StacksDir
	}
	return filepath.Join(c.DataDir, "stacks")
}

// SlogLevel returns the parsed log level. Call Validate first.
func (c Config) SlogLevel() slog.Level {
	var l slog.Level
	_ = l.UnmarshalText([]byte(c.LogLevel))
	return l
}

// Validate checks the configuration and returns all problems at once.
func (c Config) Validate() error {
	var errs []error

	if err := validateListen(c.Listen); err != nil {
		errs = append(errs, fmt.Errorf("--listen %q: %w", c.Listen, err))
	}
	if c.ContainerdAddress == "" {
		errs = append(errs, errors.New("--address must not be empty"))
	}
	if strings.TrimSpace(c.Namespace) == "" {
		errs = append(errs, errors.New("--namespace must not be empty"))
	}
	if !filepath.IsAbs(c.DataDir) {
		errs = append(errs, fmt.Errorf("--data-dir %q must be an absolute path", c.DataDir))
	}
	if c.StacksDir != "" && !filepath.IsAbs(c.StacksDir) {
		errs = append(errs, fmt.Errorf("--stacks-dir %q must be an absolute path", c.StacksDir))
	}
	for _, d := range c.StacksScanDirs {
		if !filepath.IsAbs(d) {
			errs = append(errs, fmt.Errorf("--stacks-scan-dirs entry %q must be an absolute path", d))
		}
	}
	if c.NerdctlPath == "" {
		errs = append(errs, errors.New("--nerdctl-path must not be empty"))
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		errs = append(errs, errors.New("--tls-cert and --tls-key must be set together"))
	}
	if !c.TLS && (c.TLSCert != "" || c.TLSKey != "") {
		errs = append(errs, errors.New("--tls-cert/--tls-key cannot be used with --tls=false"))
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(c.LogLevel)); err != nil {
		errs = append(errs, fmt.Errorf("--log-level %q: must be debug, info, warn or error", c.LogLevel))
	}
	return errors.Join(errs...)
}

func validateListen(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host != "" && net.ParseIP(host) == nil && strings.ContainsAny(host, " /") {
		return fmt.Errorf("invalid host %q", host)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("invalid port %q", port)
	}
	return nil
}

// EnsureDataDir creates the data directory with DataDirMode if it does not exist.
// It reports insecure=true when an existing directory is accessible by group or others;
// the permissions of an existing directory are never changed silently.
func (c Config) EnsureDataDir() (insecure bool, err error) {
	info, err := os.Stat(c.DataDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(c.DataDir, DataDirMode); err != nil {
			return false, fmt.Errorf("create data dir: %w", err)
		}
		// MkdirAll is subject to umask; enforce the exact mode on the leaf directory.
		if err := os.Chmod(c.DataDir, DataDirMode); err != nil {
			return false, fmt.Errorf("chmod data dir: %w", err)
		}
		return false, nil
	case err != nil:
		return false, fmt.Errorf("stat data dir: %w", err)
	case !info.IsDir():
		return false, fmt.Errorf("data dir %q is not a directory", c.DataDir)
	}
	return info.Mode().Perm()&0o077 != 0, nil
}
