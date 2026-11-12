// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"encoding/json"
	"os/exec"
	"slices"
	"testing"
	"time"
)

// TestIntegrationInfo compares nerdui's host and runtime info with `nerdctl info` (#66).
func TestIntegrationInfo(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })

	out, err := exec.Command("nerdctl", "info", "--format", "{{json .}}").Output()
	if err != nil {
		t.Fatalf("nerdctl info: %v", err)
	}
	var cli struct {
		ID, Driver, CgroupDriver, CgroupVersion string
		KernelVersion, Architecture             string
		ServerVersion                           string
		NCPU                                    int
		MemTotal                                int64
		Plugins                                 struct{ Storage []string }
	}
	if err := json.Unmarshal(out, &cli); err != nil {
		t.Fatalf("nerdctl info output: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	got, err := c.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name      string
		got, want any
	}{
		{"ID", got.ID, cli.ID},
		{"Snapshotter", got.Snapshotter, cli.Driver},
		{"CgroupDriver", got.CgroupDriver, cli.CgroupDriver},
		{"CgroupVersion", got.CgroupVersion, cli.CgroupVersion},
		{"KernelVersion", got.KernelVersion, cli.KernelVersion},
		{"Architecture", got.Architecture, cli.Architecture},
		{"CPUs", got.CPUs, cli.NCPU},
		{"MemoryBytes", got.MemoryBytes, cli.MemTotal},
		{"Snapshotters", got.Snapshotters, cli.Plugins.Storage},
	}
	for _, c := range checks {
		if string(mustJSON(t, c.got)) != string(mustJSON(t, c.want)) {
			t.Errorf("%s = %v, nerdctl info has %v", c.name, c.got, c.want)
		}
	}

	if i := slices.IndexFunc(got.Components, func(c Component) bool { return c.Name == "containerd" }); i < 0 ||
		got.Components[i].Version != cli.ServerVersion {
		t.Errorf("containerd component %+v, nerdctl info has %s", got.Components, cli.ServerVersion)
	}
	// nerdctl-full in CI ships runc, buildctl and the CNI plugins.
	for _, c := range got.Components {
		if c.Version == "" {
			t.Errorf("component %s without version: %s", c.Name, c.Error)
		}
	}
	if got.CNI.Version == "" || !slices.Contains(got.CNI.Plugins, "bridge") {
		t.Errorf("CNI %+v", got.CNI)
	}
	if !slices.ContainsFunc(got.Plugins, func(p Plugin) bool {
		return p.Type == "io.containerd.snapshotter.v1" && p.ID == got.Snapshotter && p.Status == PluginOK
	}) {
		t.Errorf("snapshotter plugin %s not ok in %d plugins", got.Snapshotter, len(got.Plugins))
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
