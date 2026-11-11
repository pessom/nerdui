// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/config"
)

func noEnv(string) (string, bool) { return "", false }

func execute(ctx context.Context, t *testing.T, env lookupEnv, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := newRootCmd(&out, &errOut, env)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(ctx)
	return out.String(), errOut.String(), err
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		out, _, err := execute(context.Background(), t, noEnv, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.HasPrefix(out, "nerdui ") || !strings.Contains(out, "AGPL-3.0-only") {
			t.Errorf("%v output = %q", args, out)
		}
	}
}

func TestHelpListsFlags(t *testing.T) {
	out, _, err := execute(context.Background(), t, noEnv, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--listen", "--data-dir", "--nerdctl-path", "NERDUI_DATA_DIR", "serve", "admin"} {
		if !strings.Contains(out, flag) {
			t.Errorf("help does not mention %s", flag)
		}
	}
}

func TestInvalidConfig(t *testing.T) {
	_, _, err := execute(context.Background(), t, noEnv, "--data-dir", "relative", "--log-level", "loud")
	if err == nil || !strings.Contains(err.Error(), "--data-dir") || !strings.Contains(err.Error(), "--log-level") {
		t.Fatalf("err = %v", err)
	}
}

func TestEnvAppliesToSubcommand(t *testing.T) {
	env := func(k string) (string, bool) {
		if k == "NERDUI_LOG_LEVEL" {
			return "loud", true
		}
		return "", false
	}
	_, _, err := execute(context.Background(), t, env, "serve")
	if err == nil || !strings.Contains(err.Error(), "--log-level") {
		t.Fatalf("env not applied to serve: %v", err)
	}
}

func TestPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	_, _, err = execute(context.Background(), t, noEnv,
		"serve", "--listen", ln.Addr().String(), "--data-dir", filepath.Join(t.TempDir(), "data"))
	if err == nil || !strings.Contains(err.Error(), "already in use") || !strings.Contains(err.Error(), "--listen") {
		t.Fatalf("err = %v", err)
	}
}

func TestServeStartsAndStops(t *testing.T) {
	// Reserve a free port, then release it for the server.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	dataDir := filepath.Join(t.TempDir(), "data")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := execute(ctx, t, noEnv, "--listen", addr, "--data-dir", dataDir,
			"--address", filepath.Join(t.TempDir(), "missing.sock"))
		done <- err
	}()

	url := "http://" + addr + "/healthz"
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url) //nolint:noctx // test
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}

	c := config.Default()
	c.DataDir = dataDir
	if insecure, err := c.EnsureDataDir(); err != nil || insecure {
		t.Errorf("data dir not created securely: insecure=%v err=%v", insecure, err)
	}
}

func TestResetPasswordNotImplemented(t *testing.T) {
	_, _, err := execute(context.Background(), t, noEnv, "admin", "reset-password", "admin",
		"--data-dir", t.TempDir())
	if !errors.Is(err, errNotImplemented) {
		t.Fatalf("err = %v", err)
	}
	_, _, err = execute(context.Background(), t, noEnv, "admin", "reset-password")
	if err == nil {
		t.Fatal("missing username accepted")
	}
}
