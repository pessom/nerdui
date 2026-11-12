// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"encoding/json"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

// TestIntegrationInspect compares nerdui's inspect with `nerdctl inspect` (#13).
func TestIntegrationInspect(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	name := "it-insp-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", "-v", name).Run() })

	out, err := exec.Command("nerdctl", "run", "-d", "--name", name, "--restart", "unless-stopped",
		"-e", "GREETING=hello", "-l", "it.nerdui=yes", "-v", "/data", "-p", "127.0.0.1:18082:80",
		"docker.io/library/alpine:3", "sleep", "300").CombinedOutput()
	if err != nil {
		t.Fatalf("nerdctl run: %v: %s", err, out)
	}
	id := strings.TrimSpace(string(out))
	out, err = exec.Command("nerdctl", "inspect", id).Output()
	if err != nil {
		t.Fatalf("nerdctl inspect: %v", err)
	}
	var cli []map[string]any
	if err := json.Unmarshal(out, &cli); err != nil || len(cli) != 1 {
		t.Fatalf("nerdctl inspect output: %v (%d entries)", err, len(cli))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cc := c.Containers()
	for _, ref := range []string{id, id[:12], name} {
		v, err := cc.Inspect(ctx, DefaultNamespace, ref, false)
		if err != nil {
			t.Fatalf("inspect %s: %v", ref, err)
		}
		b, _ := json.Marshal(v)
		var got map[string]any
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		for _, path := range [][]string{
			{"Id"}, {"Name"}, {"Image"}, {"State", "Status"}, {"Mounts"}, {"Config", "Env"},
			{"Config", "Labels"}, {"HostConfig", "RestartPolicy"}, {"NetworkSettings", "Ports"},
		} {
			if g, w := field(got, path), field(cli[0], path); g == nil || !reflect.DeepEqual(g, w) {
				t.Errorf("inspect %s: %s = %v, nerdctl has %v", ref, strings.Join(path, "."), g, w)
			}
		}
	}

	v, err := cc.Inspect(ctx, DefaultNamespace, id, true)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(v); !strings.Contains(string(b), `"SizeRootFs"`) {
		t.Errorf("size=true: no SizeRootFs in %s", b)
	}
	if _, err := cc.Inspect(ctx, DefaultNamespace, "it-no-such-container", false); !errdefs.IsNotFound(err) {
		t.Errorf("missing container: err = %v, want NotFound", err)
	}
	if _, err := cc.Inspect(ctx, DefaultNamespace, "../etc", false); !errdefs.IsInvalidArgument(err) {
		t.Errorf("invalid ref: err = %v, want InvalidArgument", err)
	}
}

func field(m map[string]any, path []string) any {
	var v any = m
	for _, p := range path {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[p]
	}
	return v
}
