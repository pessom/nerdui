// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import "testing"

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
