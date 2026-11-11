// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package tlsutil

import (
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func serve(t *testing.T, m *Manager) *tls.Certificate {
	t.Helper()
	c, err := m.TLSConfig().GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSelfSignedGenerateAndReuse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	clk := &clock{t: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}

	m, err := SelfSigned(dir, []string{"localhost", "nas", "192.168.1.10", "::1"}, WithClock(clk.now))
	if err != nil {
		t.Fatal(err)
	}
	info := m.Info()
	if !info.SelfSigned || !slices.Equal(info.DNSNames, []string{"localhost", "nas"}) {
		t.Errorf("info = %+v", info)
	}
	if len(info.IPAddresses) != 2 || !info.IPAddresses[0].Equal(net.ParseIP("192.168.1.10")) {
		t.Errorf("IPs = %v", info.IPAddresses)
	}
	if want := clk.t.Add(SelfSignedValidity); !info.NotAfter.Equal(want.Truncate(time.Second)) {
		t.Errorf("NotAfter = %v, want %v", info.NotAfter, want)
	}
	if len(strings.Split(info.SHA256, ":")) != 32 {
		t.Errorf("fingerprint = %q", info.SHA256)
	}

	if got := mode(t, dir); got != dirMode {
		t.Errorf("dir mode = %v", got)
	}
	if got := mode(t, filepath.Join(dir, keyFileName)); got != keyMode {
		t.Errorf("key mode = %v", got)
	}

	// A second start reuses the stored certificate.
	m2, err := SelfSigned(dir, []string{"other"}, WithClock(clk.now))
	if err != nil {
		t.Fatal(err)
	}
	if m2.Info().SHA256 != info.SHA256 {
		t.Error("certificate regenerated although still valid")
	}
}

func TestSelfSignedRenewal(t *testing.T) {
	dir := t.TempDir()
	clk := &clock{t: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	m, err := SelfSigned(dir, []string{"localhost"}, WithClock(clk.now))
	if err != nil {
		t.Fatal(err)
	}
	first := m.Info().SHA256

	// Still valid for more than RenewBefore: served unchanged.
	clk.t = clk.t.Add(SelfSignedValidity - RenewBefore - 24*time.Hour)
	serve(t, m)
	if m.Info().SHA256 != first {
		t.Fatal("renewed too early")
	}

	// Inside the renewal window: renewed on the next handshake.
	clk.t = clk.t.Add(48 * time.Hour)
	serve(t, m)
	if m.Info().SHA256 == first {
		t.Fatal("not renewed inside the renewal window")
	}

	// Renewal also happens at startup.
	clk.t = clk.t.Add(SelfSignedValidity)
	m2, err := SelfSigned(dir, []string{"localhost"}, WithClock(clk.now))
	if err != nil {
		t.Fatal(err)
	}
	if !m2.Info().NotAfter.After(clk.t) {
		t.Error("expired certificate not regenerated at startup")
	}
}

func TestSelfSignedRecoversFromCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, certFileName), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SelfSigned(dir, []string{"localhost"}); err != nil {
		t.Fatalf("corrupt files not replaced: %v", err)
	}
}

func TestFromFilesAndReload(t *testing.T) {
	src := t.TempDir()
	clk := &clock{t: time.Now()}
	gen, err := SelfSigned(src, []string{"one.example"}, WithClock(clk.now))
	if err != nil {
		t.Fatal(err)
	}

	userDir := t.TempDir()
	certFile, keyFile := filepath.Join(userDir, "c.pem"), filepath.Join(userDir, "k.pem")
	copyFile(t, filepath.Join(src, certFileName), certFile)
	copyFile(t, filepath.Join(src, keyFileName), keyFile)

	var logs []string
	m, err := FromFiles(certFile, keyFile, WithClock(clk.now),
		WithLogf(func(f string, _ ...any) { logs = append(logs, f) }))
	if err != nil {
		t.Fatal(err)
	}
	if m.Info().SelfSigned || m.Info().SHA256 != gen.Info().SHA256 {
		t.Fatalf("info = %+v", m.Info())
	}

	// Replace the files (as certbot would) and advance past the reload interval.
	src2 := t.TempDir()
	gen2, err := SelfSigned(src2, []string{"two.example"})
	if err != nil {
		t.Fatal(err)
	}
	copyFile(t, filepath.Join(src2, certFileName), certFile)
	copyFile(t, filepath.Join(src2, keyFileName), keyFile)
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(certFile, future, future); err != nil {
		t.Fatal(err)
	}

	clk.t = clk.t.Add(2 * reloadInterval)
	serve(t, m)
	if m.Info().SHA256 != gen2.Info().SHA256 {
		t.Fatalf("certificate not reloaded; logs: %v", logs)
	}

	// A broken replacement keeps serving the last good certificate.
	if err := os.WriteFile(certFile, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	future = future.Add(time.Hour)
	_ = os.Chtimes(certFile, future, future)
	clk.t = clk.t.Add(2 * reloadInterval)
	if c := serve(t, m); c == nil || m.Info().SHA256 != gen2.Info().SHA256 {
		t.Fatal("broken reload replaced the good certificate")
	}
}

func TestFromFilesErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := FromFiles(filepath.Join(dir, "missing.pem"), filepath.Join(dir, "k.pem")); err == nil {
		t.Error("missing files accepted")
	}

	a, b := t.TempDir(), t.TempDir()
	if _, err := SelfSigned(a, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := SelfSigned(b, []string{"b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := FromFiles(filepath.Join(a, certFileName), filepath.Join(b, keyFileName)); err == nil {
		t.Error("mismatched certificate and key accepted")
	}
}

func TestTLSConfigHandshake(t *testing.T) {
	m, err := SelfSigned(t.TempDir(), []string{"localhost", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.TLSConfig()
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x", cfg.MinVersion)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_ = c.(*tls.Conn).Handshake()
			_ = c.Close()
		}
	}()

	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // self-signed test
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := Fingerprint(conn.ConnectionState().PeerCertificates[0]); got != m.Info().SHA256 {
		t.Errorf("served %s, want %s", got, m.Info().SHA256)
	}
}

func TestLocalHosts(t *testing.T) {
	hosts := LocalHosts()
	if hosts[0] != "localhost" || !slices.Contains(hosts, "127.0.0.1") {
		t.Errorf("LocalHosts = %v", hosts)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
