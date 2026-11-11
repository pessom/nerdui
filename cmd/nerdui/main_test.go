// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/config"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/tlsutil"
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

// startServer runs nerdui with args on a free port and returns the address and a stop
// function that waits for a clean shutdown.
func startServer(t *testing.T, dataDir string, client *http.Client, scheme string, args ...string) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr = ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		args = append([]string{"--listen", addr, "--data-dir", dataDir,
			"--address", filepath.Join(t.TempDir(), "missing.sock")}, args...)
		_, _, err := execute(ctx, t, noEnv, args...)
		done <- err
	}()

	url := scheme + "://" + addr + "/healthz"
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := client.Get(url) //nolint:noctx // test
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			break
		}
		select {
		case err := <-done:
			t.Fatalf("server exited: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	return addr, func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("serve returned %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("server did not stop")
		}
	}
}

func insecureClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed test certificate
	}}
}

func TestServeTLSByDefault(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	addr, stop := startServer(t, dataDir, insecureClient(), "https")

	// The served certificate is exactly the one stored in <data-dir>/tls/cert.pem.
	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // self-signed test
	if err != nil {
		t.Fatal(err)
	}
	served := tlsutil.Fingerprint(conn.ConnectionState().PeerCertificates[0])
	conn.Close()
	pemBytes, err := os.ReadFile(filepath.Join(dataDir, "tls", "cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pemBytes)
	stored, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if want := tlsutil.Fingerprint(stored); served != want {
		t.Errorf("served certificate %s, stored %s", served, want)
	}

	// Plain HTTP is not served on the TLS port.
	if resp, err := http.Get("http://" + addr + "/healthz"); err == nil { //nolint:noctx // test
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Error("plain HTTP served with TLS enabled")
		}
	}
	stop()

	c := config.Default()
	c.DataDir = dataDir
	if insecure, err := c.EnsureDataDir(); err != nil || insecure {
		t.Errorf("data dir not created securely: insecure=%v err=%v", insecure, err)
	}
	for _, f := range []string{store.FileName, "tls/cert.pem", "tls/key.pem"} {
		if _, err := os.Stat(filepath.Join(dataDir, f)); err != nil {
			t.Errorf("%s not created: %v", f, err)
		}
	}
}

func TestServeWithoutTLS(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	_, stop := startServer(t, dataDir, http.DefaultClient, "http", "--tls=false")
	stop()
	if _, err := os.Stat(filepath.Join(dataDir, "tls")); !os.IsNotExist(err) {
		t.Errorf("TLS material created with --tls=false: %v", err)
	}
}

func TestServeUserCertificate(t *testing.T) {
	// Generate a certificate pair with a first run, then pass it explicitly.
	gen := filepath.Join(t.TempDir(), "gen")
	_, stop := startServer(t, gen, insecureClient(), "https")
	stop()

	dataDir := filepath.Join(t.TempDir(), "data")
	_, stop = startServer(t, dataDir, insecureClient(), "https",
		"--tls-cert", filepath.Join(gen, "tls/cert.pem"), "--tls-key", filepath.Join(gen, "tls/key.pem"))
	stop()
	if _, err := os.Stat(filepath.Join(dataDir, "tls")); !os.IsNotExist(err) {
		t.Errorf("self-signed certificate generated although user files were given: %v", err)
	}
}

func TestTLSFlagValidation(t *testing.T) {
	for _, args := range [][]string{
		{"--tls-cert", "/x/cert.pem"},
		{"--tls=false", "--tls-cert", "/x/c.pem", "--tls-key", "/x/k.pem"},
	} {
		_, _, err := execute(context.Background(), t, noEnv, append(args, "--data-dir", t.TempDir())...)
		if err == nil || !strings.Contains(err.Error(), "--tls") {
			t.Errorf("%v: err = %v", args, err)
		}
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
