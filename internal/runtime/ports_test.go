// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"slices"
	"testing"
)

func TestMarkConflicts(t *testing.T) {
	entries := []PortEntry{
		{ContainerID: "a", ContainerName: "web", Namespace: "default", State: "running", HostPort: 8080, ContainerPort: 80, Protocol: "tcp", Published: true},
		{ContainerID: "b", ContainerName: "web-old", Namespace: "default", State: "exited", HostIP: "0.0.0.0", HostPort: 8080, ContainerPort: 80, Protocol: "tcp", Published: true},
		{ContainerID: "c", ContainerName: "dns", Namespace: "k8s.io", State: "running", HostIP: "127.0.0.1", HostPort: 53, ContainerPort: 53, Protocol: "udp", Published: true},
		{ContainerID: "d", ContainerName: "udp8080", Namespace: "default", State: "running", HostPort: 8080, ContainerPort: 80, Protocol: "udp", Published: true},
		{ContainerID: "e", ContainerName: "lan", Namespace: "default", State: "running", HostIP: "10.0.0.5", HostPort: 9000, ContainerPort: 9000, Protocol: "tcp", Published: true},
		{ContainerID: "f", ContainerName: "exposed", Namespace: "default", State: "running", ContainerPort: 8080, Protocol: "tcp"},
	}
	listeners := []Listener{
		{IP: "127.0.0.53", Port: 53, Protocol: "udp"}, // other loopback address: no conflict
		{IP: "::", Port: 9000, Protocol: "tcp"},       // wildcard: conflicts with 10.0.0.5:9000
		{IP: "127.0.0.1", Port: 8080, Protocol: "udp"},
	}
	markConflicts(entries, listeners)
	want := [][]string{
		{"container web-old (default, exited)"},
		{"container web (default, running)"},
		nil,
		{"host process on 127.0.0.1:8080/udp"},
		{"host process on [::]:9000/tcp"},
		nil,
	}
	for i, e := range entries {
		if !slices.Equal(e.Conflicts, want[i]) {
			t.Errorf("%s: %q, want %q", e.ContainerName, e.Conflicts, want[i])
		}
	}
}
