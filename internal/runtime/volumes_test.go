// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"slices"
	"testing"

	"github.com/containerd/nerdctl/v2/pkg/labels"
)

// TestMountedVolumeNames covers both label formats: nerdctl 2.3+ splits mounts into
// "nerdctl/mounts.N"; older containers have one "nerdctl/mounts" array (#57).
func TestMountedVolumeNames(t *testing.T) {
	mounts := []byte(`[{"Type":"volume","Name":"pgdata","Destination":"/a"},` +
		`{"Type":"bind","Source":"/srv","Destination":"/b"},` +
		`{"Type":"volume","Name":"pgdata","Destination":"/c"},` +
		`{"Type":"volume","Name":"cache","Destination":"/d"}]`)
	split := map[string]string{labels.Name: "db"}
	if err := labels.SetMount(split, mounts); err != nil {
		t.Fatal(err)
	}
	if _, ok := split[labels.Mounts]; ok {
		t.Fatalf("expected split labels, got %v", split)
	}
	for name, lbls := range map[string]map[string]string{
		"split":  split,
		"legacy": {labels.Mounts: string(mounts)},
	} {
		got, err := mountedVolumeNames(lbls)
		if err != nil || !slices.Equal(got, []string{"pgdata", "cache"}) {
			t.Errorf("%s: %v, %v", name, got, err)
		}
	}
	if got, err := mountedVolumeNames(map[string]string{}); err != nil || got != nil {
		t.Errorf("no mounts: %v, %v", got, err)
	}
	if _, err := mountedVolumeNames(map[string]string{labels.Mounts: "{"}); err == nil {
		t.Error("broken mounts must be an error, removal fails closed")
	}
}
