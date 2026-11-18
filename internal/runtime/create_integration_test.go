// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

// TestIntegrationCreate creates and starts a container like `nerdctl run -d` (#17): the
// image is pulled first, the spec reaches the container, and the container behaves like a
// CLI-created one (logs, name).
func TestIntegrationCreate(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg), WithNerdctl("nerdctl", false))
	t.Cleanup(func() { _ = c.Close() })
	const image = "docker.io/library/busybox:1.35"
	_ = exec.Command("nerdctl", "rmi", "-f", image).Run()
	name := "it-create-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() {
		_ = exec.Command("nerdctl", "rm", "-f", name).Run()
		_ = exec.Command("nerdctl", "rmi", "-f", image).Run()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	spec := CreateSpec{
		Image:       image,
		Name:        name,
		Command:     []string{"sh", "-c", "echo hello from $GREETING; sleep 300"},
		Env:         []EnvVar{{Name: "GREETING", Value: "nerdui"}},
		Ports:       []PortSpec{{HostIP: "127.0.0.1", HostPort: 18091, ContainerPort: 80}},
		Mounts:      []MountSpec{{Type: "tmpfs", Target: "/scratch"}},
		Hostname:    "box",
		MemoryBytes: 64 << 20,
		Restart:     "unless-stopped",
		Labels:      map[string]string{"it.nerdui": "create"},
	}
	var out bytes.Buffer
	id, err := c.Containers().Create(ctx, DefaultNamespace, spec, true, &out)
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "elapsed") || !strings.Contains(out.String(), "Started") {
		t.Errorf("output without pull progress or start:\n%s", out.String())
	}

	raw, err := exec.Command("nerdctl", "inspect", id).Output()
	if err != nil {
		t.Fatalf("nerdctl inspect: %v", err)
	}
	var insp []struct {
		Name   string
		State  struct{ Running bool }
		Config struct {
			Hostname string
			Labels   map[string]string
			Env      []string
		}
		HostConfig struct {
			Memory        int64
			RestartPolicy struct{ Name string }
			PortBindings  map[string][]struct{ HostIP, HostPort string }
		}
	}
	if err := json.Unmarshal(raw, &insp); err != nil || len(insp) != 1 {
		t.Fatalf("inspect output: %v", err)
	}
	got := insp[0]
	if got.Name != name || !got.State.Running || got.Config.Hostname != "box" || got.Config.Labels["it.nerdui"] != "create" ||
		got.HostConfig.Memory != 64<<20 || got.HostConfig.RestartPolicy.Name != "unless-stopped" {
		t.Errorf("inspect %+v", got)
	}
	if b := got.HostConfig.PortBindings["80/tcp"]; len(b) != 1 || b[0].HostPort != "18091" {
		t.Errorf("ports %+v", got.HostConfig.PortBindings)
	}

	// Logs work through the host nerdctl's logging driver, like for CLI containers.
	deadline := time.Now().Add(20 * time.Second)
	for {
		logs, _ := exec.Command("nerdctl", "logs", name).CombinedOutput()
		if strings.Contains(string(logs), "hello from nerdui") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("logs %q", logs)
		}
		time.Sleep(500 * time.Millisecond)
	}

	if _, err := c.Containers().Create(ctx, DefaultNamespace, CreateSpec{Image: image, Name: name}, false, &out); !errdefs.IsConflict(err) {
		t.Errorf("duplicate name: %v", err)
	}
}

// TestIntegrationCreateAdvanced passes the advanced settings (#18) to nerdctl: the container
// starts with the capabilities, device, DNS server and /etc/hosts entry.
func TestIntegrationCreateAdvanced(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg), WithNerdctl("nerdctl", false))
	t.Cleanup(func() { _ = c.Close() })
	name := "it-create-adv-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", name).Run() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	spec := CreateSpec{
		Image:      "docker.io/library/alpine:3",
		Name:       name,
		Command:    []string{"sh", "-c", "cat /etc/hosts /etc/resolv.conf; grep CapEff /proc/self/status; ls /dev/net/tun; sleep 300"},
		CapAdd:     []string{"NET_ADMIN"},
		CapDrop:    []string{"MKNOD"},
		Devices:    []string{"/dev/net/tun"},
		DNS:        []string{"9.9.9.9"},
		ExtraHosts: []string{"db.internal:10.1.0.9"},
	}
	var out bytes.Buffer
	if _, err := c.Containers().Create(ctx, DefaultNamespace, spec, true, &out); err != nil {
		t.Fatalf("create: %v\n%s", err, out.String())
	}
	var logs string
	for range 30 {
		b, _ := exec.Command("nerdctl", "logs", name).CombinedOutput()
		if logs = string(b); strings.Contains(logs, "/dev/net/tun") {
			break
		}
		time.Sleep(time.Second)
	}
	for _, want := range []string{"10.1.0.9", "db.internal", "nameserver 9.9.9.9", "/dev/net/tun"} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %q:\n%s", want, logs)
		}
	}
}
