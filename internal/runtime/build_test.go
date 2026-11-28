// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"slices"
	"testing"

	"github.com/containerd/errdefs"
)

func TestBuildArgs(t *testing.T) {
	got, err := BuildArgs("/ctx", BuildOptions{
		Tags:       []string{"registry.local/app:1", "app:latest"},
		Dockerfile: "docker/Dockerfile.prod",
		Target:     "runtime",
		Args:       map[string]string{"VERSION": "1.2", "GO_VERSION": "1.27"},
		NoCache:    true,
		Pull:       true,
		Platform:   "linux/arm64",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"build", "--progress=plain",
		"--tag=registry.local/app:1", "--tag=app:latest",
		"--file=/ctx/docker/Dockerfile.prod", "--target=runtime",
		"--build-arg=GO_VERSION=1.27", "--build-arg=VERSION=1.2",
		"--platform=linux/arm64", "--no-cache", "--pull",
		"--", "/ctx",
	}
	if !slices.Equal(got, want) {
		t.Errorf("args\n got %q\nwant %q", got, want)
	}

	if got, _ := BuildArgs("/ctx", BuildOptions{Tags: []string{"app"}}); !slices.Equal(got, []string{"build", "--progress=plain", "--tag=app", "--", "/ctx"}) {
		t.Errorf("minimal: %q", got)
	}

	for name, opts := range map[string]BuildOptions{
		"no tag":            {},
		"bad tag":           {Tags: []string{"UPPER:case"}},
		"flag as tag":       {Tags: []string{"--push"}},
		"dockerfile escape": {Tags: []string{"a"}, Dockerfile: "../Dockerfile"},
		"absolute file":     {Tags: []string{"a"}, Dockerfile: "/etc/passwd"},
		"bad target":        {Tags: []string{"a"}, Target: "-x"},
		"bad arg name":      {Tags: []string{"a"}, Args: map[string]string{"A B": "1"}},
		"bad platform":      {Tags: []string{"a"}, Platform: "linux/amd64 --push"},
	} {
		if _, err := BuildArgs("/ctx", opts); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestShownBuildArgs(t *testing.T) {
	args, err := BuildArgs("/ctx", BuildOptions{Tags: []string{"app"}, Args: map[string]string{"TOKEN": "s3cret"}})
	if err != nil {
		t.Fatal(err)
	}
	got := ShownBuildArgs(args)
	want := []string{"build", "--progress=plain", "--tag=app", "--build-arg=TOKEN=…"}
	if !slices.Equal(got, want) {
		t.Errorf("shown %q, want %q", got, want)
	}
}
