// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"slices"
	"testing"

	"github.com/containerd/errdefs"
)

func TestNetworkCreateArgs(t *testing.T) {
	got, err := NetworkCreateArgs(NetworkSpec{
		Name: "app", Subnet: "10.20.0.7/24", Gateway: "10.20.0.1", IPRange: "10.20.0.128/25",
		Internal: true, Labels: map[string]string{"team": "home", "a": "x=y"},
	})
	want := []string{
		"network", "create", "--driver=bridge", "--subnet=10.20.0.0/24", "--gateway=10.20.0.1",
		"--ip-range=10.20.0.128/25", "--internal", "--label=a=x=y", "--label=team=home", "--", "app",
	}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("bridge: %q, %v", got, err)
	}
	got, err = NetworkCreateArgs(NetworkSpec{Name: "lan", Driver: "macvlan", Parent: "eth0", Subnet: "192.168.1.0/24"})
	want = []string{"network", "create", "--driver=macvlan", "--opt=parent=eth0", "--subnet=192.168.1.0/24", "--", "lan"}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("macvlan: %q, %v", got, err)
	}

	bad := map[string]NetworkSpec{
		"empty name":        {},
		"flag as name":      {Name: "--help"},
		"pre-defined":       {Name: "bridge"},
		"host":              {Name: "host"},
		"driver":            {Name: "n", Driver: "overlay"},
		"macvlan no parent": {Name: "n", Driver: "macvlan"},
		"parent injection":  {Name: "n", Driver: "ipvlan", Parent: "eth0 --x"},
		"bridge parent":     {Name: "n", Parent: "eth0"},
		"macvlan internal":  {Name: "n", Driver: "macvlan", Parent: "eth0", Internal: true},
		"subnet":            {Name: "n", Subnet: "10.0.0.0"},
		"gateway no subnet": {Name: "n", Gateway: "10.0.0.1"},
		"gateway outside":   {Name: "n", Subnet: "10.0.0.0/24", Gateway: "10.0.1.1"},
		"range outside":     {Name: "n", Subnet: "10.0.0.0/24", IPRange: "10.0.1.0/25"},
		"label key":         {Name: "n", Labels: map[string]string{"a=b": "c"}},
		"label newline":     {Name: "n", Labels: map[string]string{"a": "b\nc"}},
		"reserved label":    {Name: "n", Labels: map[string]string{"nerdctl/default-network": "true"}},
	}
	for name, spec := range bad {
		if _, err := NetworkCreateArgs(spec); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
