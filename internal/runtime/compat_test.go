// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

func TestMajorMinor(t *testing.T) {
	for in, want := range map[string]string{
		"v2.4.2": "2.4", "2.4.0-rc.1": "2.4", "2.3": "2.3", "v10.12.1": "10.12", "2.4+meta": "2.4",
	} {
		if got, ok := majorMinor(in); !ok || got != want {
			t.Errorf("majorMinor(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "unknown", "v2", "latest", "2.x"} {
		if _, ok := majorMinor(bad); ok {
			t.Errorf("majorMinor(%q) accepted", bad)
		}
	}
}

func TestParseNerdctlVersion(t *testing.T) {
	for in, want := range map[string]string{
		"nerdctl version 2.4.2\n":        "2.4.2",
		"nerdctl version v2.3.5":         "2.3.5",
		"nerdctl version 2.5.0-beta.1\n": "2.5.0-beta.1",
	} {
		if got, ok := parseNerdctlVersion(in); !ok || got != want {
			t.Errorf("parse(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := parseNerdctlVersion("docker version 27"); ok {
		t.Error("foreign output accepted")
	}
}

func TestContainerdSupported(t *testing.T) {
	cases := map[string]map[string]bool{
		"v2.4.2": {"v2.3.7": true, "v2.4.2": true, "v2.0.14": false, "v2.2.10": false, "v1.7.36": false, "": false},
		// nerdctl 2.3 declares containerd up to 2.3 compatible.
		"v2.3.5": {"v2.3.7": true, "v2.4.2": false, "v2.2.10": false},
		"v2.9.0": {"v2.3.7": false},
	}
	for nerdctl, versions := range cases {
		for v, want := range versions {
			if got := containerdSupportedBy(nerdctl, v); got != want {
				t.Errorf("nerdctl %s: containerd %q supported = %v", nerdctl, v, got)
			}
		}
	}
	// Every build in go.mod / go.nerdctl-*.mod has an entry.
	if len(SupportedContainerdMinors()) == 0 {
		t.Errorf("no supported containerd versions for linked nerdctl %s", NerdctlLibraryVersion())
	}
}

func TestNerdctlChecker(t *testing.T) {
	now := time.Unix(0, 0)
	calls := 0
	output, runErr := "nerdctl version 2.4.0", error(nil)
	c := &nerdctlChecker{
		path: "nerdctl", required: "2.4",
		now: func() time.Time { return now },
		run: func(context.Context, string) (string, error) { calls++; return output, runErr },
	}
	ctx := context.Background()

	st := c.status(ctx)
	if !st.Compatible || !st.Writable() || st.Version != "2.4.0" || st.Required != "2.4" {
		t.Fatalf("compatible: %+v", st)
	}

	// Cached within the TTL, re-checked afterwards (host upgrade picked up).
	output = "nerdctl version 2.5.1"
	c.status(ctx)
	if calls != 1 {
		t.Errorf("not cached: %d calls", calls)
	}
	now = now.Add(nerdctlCheckTTL)
	if st := c.status(ctx); st.Compatible || st.Writable() || st.Version != "2.5.1" {
		t.Errorf("incompatible after TTL: %+v", st)
	}

	now = now.Add(nerdctlCheckTTL)
	output, runErr = "", errors.New("not found")
	if st := c.status(ctx); st.Writable() || st.Error == "" || st.Version != "" {
		t.Errorf("missing binary: %+v", st)
	}

	now = now.Add(nerdctlCheckTTL)
	output, runErr = "garbage", nil
	if st := c.status(ctx); st.Writable() || !strings.Contains(st.Error, "parse") {
		t.Errorf("unparsable: %+v", st)
	}

	c.skip = true
	now = now.Add(nerdctlCheckTTL)
	if st := c.status(ctx); !st.Writable() || !st.CheckSkipped || st.Compatible {
		t.Errorf("skipped: %+v", st)
	}
}

func TestRunNerdctlVersion(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("shell script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "nerdctl")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"nerdctl version 2.4.1\"\n"), 0o755); err != nil { //nolint:gosec // test script
		t.Fatal(err)
	}
	c := New("/nonexistent.sock", DefaultNamespace, WithNerdctl(fake, false))
	st := c.Nerdctl(context.Background())
	want, _ := majorMinor(NerdctlLibraryVersion())
	if st.Version != "2.4.1" || st.Compatible != (want == "2.4") {
		t.Errorf("fake nerdctl: %+v (linked %s)", st, NerdctlLibraryVersion())
	}

	missing := New("/nonexistent.sock", DefaultNamespace, WithNerdctl(filepath.Join(dir, "absent"), false))
	if st := missing.Nerdctl(context.Background()); st.Writable() || !strings.Contains(st.Error, "not found") {
		t.Errorf("missing binary: %+v", st)
	}
}
