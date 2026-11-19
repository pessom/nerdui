// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"bytes"
	"context"
	"net"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestIntegrationRecreate reconstructs a CLI-created container's settings, recreates it with
// a change and rolls back a recreate that fails (#18).
func TestIntegrationRecreate(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg), WithNerdctl("nerdctl", false))
	t.Cleanup(func() { _ = c.Close() })
	suffix := strconv.FormatInt(time.Now().UnixNano()%1e8, 36)
	name, vol := "it-recreate-"+suffix, "it-recreate-vol-"+suffix
	t.Cleanup(func() {
		out, _ := exec.Command("nerdctl", "ps", "-aq", "--filter", "name="+name).Output()
		for _, id := range strings.Fields(string(out)) {
			_ = exec.Command("nerdctl", "rm", "-f", id).Run()
		}
		_ = exec.Command("nerdctl", "volume", "rm", vol).Run()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	run := []string{"run", "-d", "--name", name, "-p", "127.0.0.1:18095:80", "-v", vol + ":/data",
		"-e", "TZ=UTC", "--restart=unless-stopped", "--cap-add=NET_ADMIN", "--dns=9.9.9.9",
		"--add-host=db.internal:10.0.0.9", "--label", "app=it", "docker.io/library/alpine:3", "sleep", "600"}
	if out, err := exec.Command("nerdctl", run...).CombinedOutput(); err != nil {
		t.Fatalf("nerdctl run: %v: %s", err, out)
	}

	cs, err := c.Containers().Spec(ctx, DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("spec %+v, unsupported %v", cs.Spec, cs.Unsupported)
	if len(cs.Unsupported) > 0 {
		t.Errorf("a container created with supported flags reports unsupported settings: %v", cs.Unsupported)
	}
	s := cs.Spec
	if !cs.Running || s.Name != name || !slices.Equal(s.Command, []string{"sleep", "600"}) || s.Restart != "unless-stopped" {
		t.Errorf("basic settings %+v running=%v", s, cs.Running)
	}
	if !slices.Equal(s.Env, []EnvVar{{Name: "TZ", Value: "UTC"}}) {
		t.Errorf("env %v", s.Env)
	}
	if len(s.Ports) != 1 || s.Ports[0].HostPort != 18095 || s.Ports[0].ContainerPort != 80 || s.Ports[0].HostIP != "127.0.0.1" {
		t.Errorf("ports %+v", s.Ports)
	}
	if !slices.ContainsFunc(s.Mounts, func(m MountSpec) bool { return m.Type == "volume" && m.Source == vol && m.Target == "/data" }) {
		t.Errorf("mounts %+v", s.Mounts)
	}
	if !slices.ContainsFunc(s.CapAdd, func(c string) bool { return strings.HasSuffix(c, "NET_ADMIN") }) {
		t.Errorf("capabilities %v", s.CapAdd)
	}
	if !slices.Equal(s.DNS, []string{"9.9.9.9"}) || !slices.Contains(s.ExtraHosts, "db.internal:10.0.0.9") || s.Labels["app"] != "it" {
		t.Errorf("dns %v hosts %v labels %v", s.DNS, s.ExtraHosts, s.Labels)
	}
	if _, err := CreateArgs(s); err != nil {
		t.Fatalf("reconstructed spec is invalid: %v", err)
	}

	// Recreate with a changed variable: same name, new container, running, old one gone.
	s.Env = []EnvVar{{Name: "TZ", Value: "Europe/Berlin"}}
	var out bytes.Buffer
	newID, err := c.Containers().Recreate(ctx, DefaultNamespace, name, s, RecreateOptions{}, &out)
	if err != nil {
		ss, _ := exec.Command("ss", "-ltnp").CombinedOutput()
		ps, _ := exec.Command("nerdctl", "ps", "-a").CombinedOutput()
		t.Fatalf("recreate: %v\n%s\nlisteners:\n%s\ncontainers:\n%s", err, out.String(), ss, ps)
	}
	if newID == cs.ID {
		t.Fatal("recreate kept the container")
	}
	env, _ := exec.Command("nerdctl", "exec", name, "printenv", "TZ").Output()
	if strings.TrimSpace(string(env)) != "Europe/Berlin" {
		t.Errorf("TZ in the new container: %q\n%s", env, out.String())
	}
	if all, _ := exec.Command("nerdctl", "ps", "-aq", "--no-trunc").Output(); strings.Contains(string(all), cs.ID) {
		t.Error("the previous container was not removed")
	}

	// A failing recreate (bind source missing) restores the container.
	bad := s
	bad.Mounts = append(slices.Clone(s.Mounts), MountSpec{Type: "bind", Source: "/nonexistent/nerdui-it", Target: "/x"})
	out.Reset()
	if _, err := c.Containers().Recreate(ctx, DefaultNamespace, name, bad, RecreateOptions{}, &out); err == nil {
		t.Fatalf("recreate with a missing bind source succeeded\n%s", out.String())
	}
	id, _ := exec.Command("nerdctl", "inspect", "-f", "{{.ID}} {{.State.Running}}", name).Output()
	if got := strings.TrimSpace(string(id)); got != newID+" true" {
		t.Errorf("after rollback: %q, want %s running\n%s", got, newID, out.String())
	}
}

// TestIntegrationStopReleasesPorts stops a container through nerdui and finds its published
// host port free, as after `nerdctl stop` (nerdctl's port reserver is ended).
func TestIntegrationStopReleasesPorts(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg), WithNerdctl("nerdctl", false))
	t.Cleanup(func() { _ = c.Close() })
	name := "it-stop-ports-" + strconv.FormatInt(time.Now().UnixNano()%1e8, 36)
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", name).Run() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.Command("nerdctl", "run", "-d", "--name", name, "-p", "127.0.0.1:18096:80",
		"docker.io/library/alpine:3", "sleep", "600").Output()
	if err != nil {
		t.Fatalf("nerdctl run: %v", err)
	}
	if err := c.Containers().Do(ctx, DefaultNamespace, strings.TrimSpace(string(out)), ActionStop, ActionOptions{}); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:18096")
	if err != nil {
		ss, _ := exec.Command("ss", "-ltnp").CombinedOutput()
		t.Fatalf("port still held after stop: %v\n%s", err, ss)
	}
	_ = ln.Close()
}
