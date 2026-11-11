// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func parse(t *testing.T, args []string, env map[string]string) (Config, error) {
	t.Helper()
	c := Default()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	BindServeFlags(fs, &c)
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	err := ApplyEnv(fs, func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	return c, err
}

func TestPrecedence(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"default", nil, nil, DefaultListen},
		{"env", nil, map[string]string{"NERDUI_LISTEN": ":1111"}, ":1111"},
		{"flag", []string{"--listen", ":2222"}, nil, ":2222"},
		{"flag over env", []string{"--listen=:2222"}, map[string]string{"NERDUI_LISTEN": ":1111"}, ":2222"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parse(t, tt.args, tt.env)
			if err != nil {
				t.Fatal(err)
			}
			if c.Listen != tt.want {
				t.Errorf("Listen = %q, want %q", c.Listen, tt.want)
			}
		})
	}
}

func TestEnvTypes(t *testing.T) {
	c, err := parse(t, nil, map[string]string{
		"NERDUI_STACKS_SCAN_DIRS":           "/srv/a,/srv/b",
		"NERDUI_SKIP_NERDCTL_VERSION_CHECK": "true",
		"NERDUI_DATA_DIR":                   "/tmp/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.StacksScanDirs, []string{"/srv/a", "/srv/b"}) {
		t.Errorf("StacksScanDirs = %v", c.StacksScanDirs)
	}
	if !c.SkipNerdctlVersionCheck || c.DataDir != "/tmp/x" {
		t.Errorf("got %+v", c)
	}
}

func TestEnvInvalidValue(t *testing.T) {
	_, err := parse(t, nil, map[string]string{"NERDUI_SKIP_NERDCTL_VERSION_CHECK": "maybe"})
	if err == nil || !strings.Contains(err.Error(), "NERDUI_SKIP_NERDCTL_VERSION_CHECK") {
		t.Fatalf("err = %v", err)
	}
}

func TestEveryFlagMentionsEnv(t *testing.T) {
	c := Default()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	BindServeFlags(fs, &c)
	fs.VisitAll(func(f *pflag.Flag) {
		if !strings.Contains(f.Usage, EnvName(f.Name)) {
			t.Errorf("flag --%s usage does not mention %s", f.Name, EnvName(f.Name))
		}
	})
}

func TestValidate(t *testing.T) {
	ok := Default()
	if err := ok.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}

	bad := Default()
	bad.Listen = "9443"
	bad.DataDir = "relative/dir"
	bad.StacksScanDirs = []string{"rel"}
	bad.LogLevel = "loud"
	bad.Namespace = " "
	err := bad.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"--listen", "--data-dir", "--stacks-scan-dirs", "--log-level", "--namespace"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}

	for _, l := range []string{":0", ":65536", "host:abc"} {
		c := Default()
		c.Listen = l
		if c.Validate() == nil {
			t.Errorf("Listen %q accepted", l)
		}
	}
}

func TestEffectiveStacksDir(t *testing.T) {
	c := Default()
	if got := c.EffectiveStacksDir(); got != "/var/lib/nerdui/stacks" {
		t.Errorf("got %q", got)
	}
	c.StacksDir = "/srv/stacks"
	if got := c.EffectiveStacksDir(); got != "/srv/stacks" {
		t.Errorf("got %q", got)
	}
}

func TestEnsureDataDir(t *testing.T) {
	base := t.TempDir()

	c := Default()
	c.DataDir = filepath.Join(base, "new", "data")
	insecure, err := c.EnsureDataDir()
	if err != nil || insecure {
		t.Fatalf("create: insecure=%v err=%v", insecure, err)
	}
	info, _ := os.Stat(c.DataDir)
	if info.Mode().Perm() != DataDirMode {
		t.Errorf("mode = %v, want %v", info.Mode().Perm(), DataDirMode)
	}

	open := filepath.Join(base, "open")
	if err := os.Mkdir(open, 0o755); err != nil {
		t.Fatal(err)
	}
	c.DataDir = open
	if insecure, err := c.EnsureDataDir(); err != nil || !insecure {
		t.Errorf("existing 0755: insecure=%v err=%v", insecure, err)
	}

	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c.DataDir = file
	if _, err := c.EnsureDataDir(); err == nil {
		t.Error("file accepted as data dir")
	}
}

func TestSlogLevel(t *testing.T) {
	c := Default()
	c.LogLevel = "debug"
	if c.SlogLevel().String() != "DEBUG" {
		t.Errorf("got %v", c.SlogLevel())
	}
}
