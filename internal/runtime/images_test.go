// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
)

func TestSplitReference(t *testing.T) {
	for name, want := range map[string][2]string{
		"docker.io/library/nginx:1.27":       {"nginx", "1.27"},
		"ghcr.io/pessom/nerdui:0.1.0":        {"ghcr.io/pessom/nerdui", "0.1.0"},
		"registry.local:5000/app/api:latest": {"registry.local:5000/app/api", "latest"},
		"docker.io/library/alpine@sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1": {"alpine", ""},
		"sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1":                          {"", ""},
		"": {"", ""},
	} {
		repo, tag := splitReference(name)
		if repo != want[0] || tag != want[1] {
			t.Errorf("splitReference(%q) = %q, %q; want %q, %q", name, repo, tag, want[0], want[1])
		}
	}
}

func TestClassifyImageErr(t *testing.T) {
	cases := map[string]func(error) bool{
		"1 errors:\nno such image: foo":                                              errdefs.IsNotFound,
		"an image does not exist locally with the tag: foo":                          errdefs.IsNotFound,
		"conflict: unable to delete foo (must be forced) - image is being used by x": errdefs.IsConflict,
		"multiple IDs found with provided prefix: ab":                                errdefs.IsInvalidArgument,
	}
	for msg, is := range cases {
		if err := classifyImageErr(errors.New(msg)); !is(err) || !strings.Contains(err.Error(), msg) {
			t.Errorf("%q classified as %v", msg, err)
		}
	}
}

func TestClassifyLoginErr(t *testing.T) {
	if err := classifyLoginErr(errors.New("unexpected status from GET request: 401 Unauthorized")); !errdefs.IsInvalidArgument(err) {
		t.Errorf("401: %v", err)
	}
	if err := classifyLoginErr(errors.New("dial tcp 10.0.0.1:443: connect: connection refused")); !errdefs.IsUnavailable(err) {
		t.Errorf("refused: %v", err)
	}
	if err := classifyLoginErr(errors.New("something else")); errdefs.IsInvalidArgument(err) || errdefs.IsUnavailable(err) {
		t.Errorf("other: %v", err)
	}
}

func TestGroupRegistries(t *testing.T) {
	got := groupRegistries(
		[]string{"gitlab.pessom.ru:443", "https://index.docker.io/v1/", "gitlab.pessom.ru", "registry.local:5000", "https://gitlab.pessom.ru"},
		func(k string) string {
			if k == "gitlab.pessom.ru:443" {
				return "bob"
			}
			return ""
		})
	if len(got) != 3 {
		t.Fatalf("groups %+v", got)
	}
	g := got[0]
	if g.Server != "gitlab.pessom.ru" || g.Username != "bob" || !slices.Equal(g.Aliases, []string{"gitlab.pessom.ru:443", "https://gitlab.pessom.ru"}) {
		t.Errorf("gitlab group %+v", g)
	}
	if got[1].Server != "https://index.docker.io/v1/" || len(got[1].Aliases) != 0 || got[2].Server != "registry.local:5000" {
		t.Errorf("others %+v", got[1:])
	}
}
