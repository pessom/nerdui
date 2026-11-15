// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestIntegrationPorts lists a published port of a running container and reads the host's
// listening sockets from /proc/net (#62).
func TestIntegrationPorts(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// A host process listening on a port: it must be read from /proc/net.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	hostPort := int32(ln.Addr().(*net.TCPAddr).Port)

	name := "it-ports-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", name).Run() })
	out, err := exec.Command("nerdctl", "run", "-d", "--name", name, "-p", "127.0.0.1:18431:80/tcp",
		"docker.io/library/alpine:3", "sleep", "600").CombinedOutput()
	if err != nil {
		t.Fatalf("nerdctl run: %v: %s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	id := strings.TrimSpace(lines[len(lines)-1])

	report, err := c.Ports(ctx)
	if err != nil {
		t.Fatalf("ports: %v", err)
	}
	if !report.HostChecked {
		t.Error("host sockets were not read")
	}
	found := false
	for _, p := range report.Ports {
		if p.ContainerID == id {
			found = p.Published && p.HostIP == "127.0.0.1" && p.HostPort == 18431 && p.ContainerPort == 80 &&
				p.Protocol == "tcp" && p.ContainerName == name && p.Namespace == DefaultNamespace && p.State == StateRunning
		}
	}
	if !found {
		t.Errorf("published port of %s not listed: %+v", name, report.Ports)
	}

	listeners, ok := hostListeners("/proc/net")
	seen := false
	for _, l := range listeners {
		seen = seen || (l.Port == hostPort && l.IP == "127.0.0.1" && l.Protocol == "tcp")
	}
	if !ok || !seen {
		t.Errorf("listener 127.0.0.1:%d not found in /proc/net (%d sockets)", hostPort, len(listeners))
	}
}
