// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/containerd/containerd/v2/pkg/namespaces"
)

// PortsReport lists the published and exposed ports of all containers of the host (#62).
// Keep in sync with api/openapi.yaml.
type PortsReport struct {
	Ports []PortEntry `json:"ports"`
	// HostChecked is true when the host's own listening sockets were compared too.
	HostChecked bool `json:"hostChecked"`
}

// PortEntry is a port of a container: published on the host (HostPort > 0) or only exposed by
// its image.
type PortEntry struct {
	Namespace     string `json:"namespace"`
	ContainerID   string `json:"containerId"`
	ContainerName string `json:"containerName,omitempty"`
	State         string `json:"state"`
	HostIP        string `json:"hostIp,omitempty"`
	HostPort      int32  `json:"hostPort,omitempty"`
	ContainerPort int32  `json:"containerPort"`
	Protocol      string `json:"protocol"`
	Published     bool   `json:"published"`
	// Conflicts describes other users of the same host port: containers (a stopped one cannot
	// start while another holds the port) and host processes.
	Conflicts []string `json:"conflicts,omitempty"`
}

// Ports collects the ports of every namespace's containers and marks conflicts.
func (c *Client) Ports(ctx context.Context) (PortsReport, error) {
	nss, err := c.Namespaces(ctx)
	if err != nil {
		return PortsReport{}, err
	}
	var entries []PortEntry
	for _, ns := range nss {
		list, err := c.Containers().List(ctx, ns)
		if err != nil {
			return PortsReport{}, err
		}
		exposedOf := map[string][]Port{} // by image
		for _, ct := range list {
			published := map[string]bool{}
			for _, p := range ct.Ports {
				published[portKey(p.ContainerPort, p.Protocol)] = true
				entries = append(entries, PortEntry{
					Namespace: ns, ContainerID: ct.ID, ContainerName: ct.Name, State: ct.State,
					HostIP: p.HostIP, HostPort: p.HostPort, ContainerPort: p.ContainerPort,
					Protocol: p.Protocol, Published: true,
				})
			}
			exposed, ok := exposedOf[ct.Image]
			if !ok {
				exposed = c.exposedPorts(ctx, ns, ct.Image)
				exposedOf[ct.Image] = exposed
			}
			for _, p := range exposed {
				if published[portKey(p.ContainerPort, p.Protocol)] {
					continue
				}
				entries = append(entries, PortEntry{
					Namespace: ns, ContainerID: ct.ID, ContainerName: ct.Name, State: ct.State,
					ContainerPort: p.ContainerPort, Protocol: p.Protocol,
				})
			}
		}
	}
	listeners, checked := hostListeners("/proc/net")
	markConflicts(entries, listeners)
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Published != b.Published {
			return a.Published
		}
		if a.HostPort != b.HostPort {
			return a.HostPort < b.HostPort
		}
		if a.ContainerPort != b.ContainerPort {
			return a.ContainerPort < b.ContainerPort
		}
		return a.ContainerName < b.ContainerName
	})
	if entries == nil {
		entries = []PortEntry{}
	}
	return PortsReport{Ports: entries, HostChecked: checked}, nil
}

func portKey(port int32, proto string) string { return strconv.Itoa(int(port)) + "/" + proto }

// exposedPorts returns the ports an image declares (EXPOSE); nil when unknown.
func (c *Client) exposedPorts(ctx context.Context, namespace, ref string) []Port {
	cl, err := c.conn()
	if err != nil || ref == "" {
		return nil
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	img, err := cl.GetImage(ctx, ref)
	if err != nil {
		return nil
	}
	spec, err := img.Spec(ctx)
	if err != nil {
		return nil
	}
	var out []Port
	for k := range spec.Config.ExposedPorts {
		port, proto, _ := strings.Cut(k, "/")
		if proto == "" {
			proto = "tcp"
		}
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			continue
		}
		out = append(out, Port{ContainerPort: int32(n), Protocol: proto})
	}
	return out
}

// sameAddress reports whether two bind addresses overlap: equal, or either is a wildcard.
func sameAddress(a, b string) bool {
	wild := func(s string) bool {
		ip := net.ParseIP(s)
		return s == "" || (ip != nil && ip.IsUnspecified())
	}
	if wild(a) || wild(b) {
		return true
	}
	ia, ib := net.ParseIP(a), net.ParseIP(b)
	return ia != nil && ib != nil && ia.Equal(ib)
}

func hostAddr(ip string, port int32) string {
	if ip == "" {
		ip = "0.0.0.0"
	}
	return net.JoinHostPort(ip, strconv.Itoa(int(port)))
}

// markConflicts sets Conflicts on published entries that share a host port and protocol with
// another container's published port or with a host socket.
func markConflicts(entries []PortEntry, listeners []Listener) {
	for i := range entries {
		e := &entries[i]
		if !e.Published || e.HostPort == 0 {
			continue
		}
		for j := range entries {
			o := entries[j]
			if i == j || !o.Published || o.ContainerID == e.ContainerID || o.HostPort != e.HostPort ||
				o.Protocol != e.Protocol || !sameAddress(o.HostIP, e.HostIP) {
				continue
			}
			name := o.ContainerName
			if name == "" {
				name = o.ContainerID[:min(12, len(o.ContainerID))]
			}
			e.Conflicts = append(e.Conflicts, fmt.Sprintf("container %s (%s, %s)", name, o.Namespace, o.State))
		}
		for _, l := range listeners {
			if l.Port == e.HostPort && l.Protocol == e.Protocol && sameAddress(l.IP, e.HostIP) {
				e.Conflicts = append(e.Conflicts, "host process on "+hostAddr(l.IP, l.Port)+"/"+l.Protocol)
			}
		}
	}
}
