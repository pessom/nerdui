// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

// TestIntegrationNetworks creates bridge and macvlan networks with the host nerdctl, sees a
// running container's address, refuses removal while it runs and removes it afterwards (#60).
func TestIntegrationNetworks(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	suffix := strconv.FormatInt(time.Now().UnixNano()%1e6, 36)
	bridge, lan, ctr := "it-net-"+suffix, "it-lan-"+suffix, "it-net-ctr-"+suffix
	t.Cleanup(func() {
		_ = exec.Command("nerdctl", "rm", "-f", ctr).Run()
		_ = exec.Command("nerdctl", "network", "rm", bridge, lan).Run()
	})

	created, err := c.Networks().Create(ctx, DefaultNamespace, NetworkSpec{
		Name: bridge, Subnet: "10.231.7.0/24", Gateway: "10.231.7.1", IPRange: "10.231.7.128/25",
		Labels: map[string]string{"app": "it"},
	})
	if err != nil {
		t.Fatalf("create bridge: %v", err)
	}
	if created.Driver != "bridge" || created.Labels["app"] != "it" || !created.Removable || created.ID == "" ||
		len(created.IPAM) != 1 || created.IPAM[0].Subnet != "10.231.7.0/24" || created.IPAM[0].Gateway != "10.231.7.1" {
		t.Errorf("created %+v", created)
	}
	if _, err := c.Networks().Create(ctx, DefaultNamespace, NetworkSpec{Name: bridge}); !errdefs.IsAlreadyExists(err) {
		t.Errorf("create again: %v", err)
	}
	macvlan, err := c.Networks().Create(ctx, DefaultNamespace, NetworkSpec{
		Name: lan, Driver: "macvlan", Parent: "eth0", Subnet: "10.232.7.0/24",
	})
	if err != nil {
		t.Fatalf("create macvlan: %v", err)
	}
	if macvlan.Driver != "macvlan" || macvlan.Parent != "eth0" {
		t.Errorf("macvlan %+v", macvlan)
	}
	// nerdctl sees the networks nerdui created.
	if out, err := exec.Command("nerdctl", "network", "inspect", bridge, lan).CombinedOutput(); err != nil {
		t.Fatalf("nerdctl network inspect: %v: %s", err, out)
	}

	out, err := exec.Command("nerdctl", "run", "-d", "--name", ctr, "--network", bridge,
		"docker.io/library/alpine:3", "sleep", "600").CombinedOutput()
	if err != nil {
		t.Fatalf("nerdctl run: %v: %s", err, out)
	}
	find := func(name string) Network {
		t.Helper()
		list, err := c.Networks().List(ctx, DefaultNamespace)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, n := range list {
			if n.Name == name {
				return n
			}
		}
		t.Fatalf("network %s not listed", name)
		return Network{}
	}
	n := find(bridge)
	if len(n.Containers) != 1 || n.Containers[0].Name != ctr || !n.Containers[0].Running ||
		len(n.Containers[0].IPs) == 0 || !strings.HasPrefix(n.Containers[0].IPs[0], "10.231.7.") {
		t.Errorf("attached %+v", n.Containers)
	}

	if err := c.Networks().Remove(ctx, DefaultNamespace, bridge); !errdefs.IsFailedPrecondition(err) {
		t.Fatalf("remove used network: %v", err)
	}
	if err := c.Networks().Remove(ctx, DefaultNamespace, "bridge"); !errdefs.IsFailedPrecondition(err) && !errdefs.IsNotFound(err) {
		t.Errorf("remove default network: %v", err)
	}
	// Like nerdctl: a stopped container does not block removal.
	if out, err := exec.Command("nerdctl", "stop", "-t", "0", ctr).CombinedOutput(); err != nil {
		t.Fatalf("nerdctl stop: %v: %s", err, out)
	}
	if n := find(bridge); len(n.Containers) != 1 || n.Containers[0].Running {
		t.Errorf("stopped container %+v", n.Containers)
	}
	for _, name := range []string{bridge, lan} {
		if err := c.Networks().Remove(ctx, DefaultNamespace, name); err != nil {
			t.Fatalf("remove %s: %v", name, err)
		}
	}
	if err := c.Networks().Remove(ctx, DefaultNamespace, bridge); !errdefs.IsNotFound(err) {
		t.Errorf("remove again: %v", err)
	}
}
