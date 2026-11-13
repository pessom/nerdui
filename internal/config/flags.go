// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

// EnvPrefix is the prefix of environment variables mirroring flags.
const EnvPrefix = "NERDUI_"

// EnvName returns the environment variable for a flag, e.g. "data-dir" -> "NERDUI_DATA_DIR".
func EnvName(flag string) string {
	return EnvPrefix + strings.ToUpper(strings.ReplaceAll(flag, "-", "_"))
}

// BindServeFlags registers the server flags on fs, storing values into c.
// c should be initialized with Default() so flag defaults match.
func BindServeFlags(fs *pflag.FlagSet, c *Config) {
	str := func(p *string, name, usage string) {
		fs.StringVar(p, name, *p, usage+" (env "+EnvName(name)+")")
	}
	BindListenFlags(fs, c)
	str(&c.ContainerdAddress, "address", "containerd socket address")
	str(&c.Namespace, "namespace", "default containerd namespace")
	BindDataDirFlag(fs, c)
	str(&c.StacksDir, "stacks-dir", "directory with compose stacks managed by nerdui (default <data-dir>/stacks)")
	fs.StringSliceVar(&c.StacksScanDirs, "stacks-scan-dirs", c.StacksScanDirs,
		"comma-separated directories scanned for existing compose projects (env "+EnvName("stacks-scan-dirs")+")")
	str(&c.NerdctlPath, "nerdctl-path", "host nerdctl binary")
	fs.BoolVar(&c.SkipNerdctlVersionCheck, "skip-nerdctl-version-check", c.SkipNerdctlVersionCheck,
		"allow a host nerdctl whose major.minor differs from the linked packages (unsupported) (env "+
			EnvName("skip-nerdctl-version-check")+")")
	BindTLSCertFlag(fs, c)
	str(&c.TLSKey, "tls-key", "PEM private key file for --tls-cert")
	str(&c.LogLevel, "log-level", "log level: debug, info, warn, error")
	fs.BoolVar(&c.DisableExec, "disable-exec", c.DisableExec,
		"turn off the web terminal into containers (env "+EnvName("disable-exec")+")")
	fs.DurationVar(&c.EventsRetention, "events-retention", c.EventsRetention,
		"how long the event history is kept, e.g. 168h; 0 disables it (env "+EnvName("events-retention")+")")
}

// BindListenFlags registers --listen and --tls (shared by serve and healthcheck).
func BindListenFlags(fs *pflag.FlagSet, c *Config) {
	fs.StringVar(&c.Listen, "listen", c.Listen, "address to listen on (env "+EnvName("listen")+")")
	fs.BoolVar(&c.TLS, "tls", c.TLS,
		"serve HTTPS; set --tls=false only behind a TLS-terminating reverse proxy (env "+EnvName("tls")+")")
}

// BindTLSCertFlag registers --tls-cert (shared by serve and healthcheck).
func BindTLSCertFlag(fs *pflag.FlagSet, c *Config) {
	fs.StringVar(&c.TLSCert, "tls-cert", c.TLSCert,
		"PEM certificate file (default: self-signed in <data-dir>/tls) (env "+EnvName("tls-cert")+")")
}

// BindDataDirFlag registers only --data-dir (shared by serve and admin commands).
func BindDataDirFlag(fs *pflag.FlagSet, c *Config) {
	fs.StringVar(&c.DataDir, "data-dir", c.DataDir, "directory for nerdui state (env "+EnvName("data-dir")+")")
}

// ApplyEnv sets every flag that was not given on the command line from its NERDUI_*
// environment variable, if present. lookup is usually os.LookupEnv.
func ApplyEnv(fs *pflag.FlagSet, lookup func(string) (string, bool)) error {
	var firstErr error
	fs.VisitAll(func(f *pflag.Flag) {
		if firstErr != nil || f.Changed || f.Name == "help" {
			return
		}
		name := EnvName(f.Name)
		v, ok := lookup(name)
		if !ok {
			return
		}
		if err := fs.Set(f.Name, v); err != nil {
			firstErr = fmt.Errorf("%s=%q: %w", name, v, err)
		}
	})
	return firstErr
}
