// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseRuncVersion(t *testing.T) {
	v, commit, ok := parseRuncVersion("runc version 1.3.0\ncommit: v1.3.0-0-g4ca628d1\nspec: 1.2.1\ngo: go1.24.4\n")
	if !ok || v != "1.3.0" || commit != "v1.3.0-0-g4ca628d1" {
		t.Errorf("got %q %q %v", v, commit, ok)
	}
	if _, _, ok := parseRuncVersion("crun version 1.21\n"); ok {
		t.Error("crun output accepted")
	}
}

func TestParseBuildctlVersion(t *testing.T) {
	v, commit, ok := parseBuildctlVersion("buildctl github.com/moby/buildkit v0.23.2 940c2f1d\n")
	if !ok || v != "v0.23.2" || commit != "940c2f1d" {
		t.Errorf("got %q %q %v", v, commit, ok)
	}
	if v, _, ok := parseBuildctlVersion("buildctl github.com/moby/buildkit v0.23.2"); !ok || v != "v0.23.2" {
		t.Errorf("without commit: %q %v", v, ok)
	}
	if _, _, ok := parseBuildctlVersion("something else"); ok {
		t.Error("garbage accepted")
	}
}

func TestPluginStatus(t *testing.T) {
	if s, _ := pluginStatus("no btrfs on /var/lib/containerd: skip plugin"); s != PluginSkip {
		t.Errorf("skip: %s", s)
	}
	if s, msg := pluginStatus("failed to load"); s != PluginError || msg != "failed to load" {
		t.Errorf("error: %s %s", s, msg)
	}
}

func TestProbeCNI(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'CNI bridge plugin v1.7.1' >&2\necho 'CNI protocol versions supported: 1.0.0' >&2\n"
	for name, body := range map[string]string{"bridge": script, "loopback": "#!/bin/sh\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	ci := probeCNI(context.Background(), dir, "/etc/cni/net.d")
	if ci.Version != "v1.7.1" || ci.Error != "" || !slices.Equal(ci.Plugins, []string{"bridge", "loopback"}) {
		t.Errorf("got %+v", ci)
	}

	ci = probeCNI(context.Background(), filepath.Join(dir, "missing"), "")
	if ci.Error == "" || len(ci.Plugins) != 0 {
		t.Errorf("missing dir: %+v", ci)
	}

	empty := t.TempDir()
	if ci := probeCNI(context.Background(), empty, ""); !strings.Contains(ci.Error, "bridge") {
		t.Errorf("no bridge: %+v", ci)
	}
}

func TestProbeComponentMissing(t *testing.T) {
	c := probeComponent(context.Background(), "runc", "nerdui-no-such-binary", parseRuncVersion)
	if c.Version != "" || !strings.Contains(c.Error, "not found") {
		t.Errorf("got %+v", c)
	}
}
