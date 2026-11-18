// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"reflect"
	"strings"
	"testing"

	"github.com/containerd/nerdctl/v2/pkg/inspecttypes/dockercompat"
	"github.com/docker/go-connections/nat"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func TestSpecFrom(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	img := ocispec.ImageConfig{
		Entrypoint: []string{"/docker-entrypoint.sh"},
		Cmd:        []string{"nginx", "-g", "daemon off;"},
		Env:        []string{"PATH=/usr/bin", "NGINX_VERSION=1.27"},
		WorkingDir: "",
		Labels:     map[string]string{"maintainer": "nginx"},
	}
	c := &dockercompat.Container{
		ID:   id,
		Name: "web",
		Config: &dockercompat.Config{
			Hostname: id[:12],
			Env:      []string{"PATH=/usr/bin", "NGINX_VERSION=1.27", "HOSTNAME=" + id[:12], "TZ=UTC"},
		},
		HostConfig: &dockercompat.HostConfig{
			PortBindings: nat.PortMap{
				"80/tcp": {{HostIP: "0.0.0.0", HostPort: "8080"}},
				"53/udp": {{HostIP: "127.0.0.1", HostPort: "5353"}},
			},
			RestartPolicy: dockercompat.RestartPolicy{Name: "unless-stopped"},
			CPUQuota:      150000, CPUPeriod: 100000,
			Memory:     256 << 20,
			CapAdd:     []string{"CAP_NET_ADMIN"},
			Devices:    []dockercompat.DeviceMapping{{PathOnHost: "/dev/net/tun", PathInContainer: "/dev/net/tun", CgroupPermissions: "rwm"}},
			DNS:        []string{"1.1.1.1"},
			ExtraHosts: []string{"db:10.0.0.9"},
			ShmSize:    defaultShmSize,
		},
		Mounts: []dockercompat.MountPoint{
			{Type: "volume", Name: "data", Source: "/var/lib/nerdctl/x/volumes/default/data/_data", Destination: "/data", RW: true},
			{Type: "bind", Source: "/etc/app", Destination: "/etc/app", RW: false},
		},
	}
	oci := &specs.Spec{Process: &specs.Process{
		Args: []string{"/docker-entrypoint.sh", "nginx", "-g", "daemon off;"},
		Cwd:  "/",
	}}
	labels := map[string]string{
		"nerdctl/networks": `["lan","backend"]`,
		"nerdctl/ip":       "10.1.0.5",
		"maintainer":       "nginx",
		"app":              "web",
	}
	got, unsupported := specFrom(c, oci, img, "docker.io/library/nginx:1.27", labels)
	want := CreateSpec{
		Image: "docker.io/library/nginx:1.27", Name: "web",
		Env: []EnvVar{{Name: "TZ", Value: "UTC"}},
		Ports: []PortSpec{
			{HostIP: "127.0.0.1", HostPort: 5353, ContainerPort: 53, Protocol: "udp"},
			{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"},
		},
		Mounts: []MountSpec{
			{Type: "volume", Source: "data", Target: "/data"},
			{Type: "bind", Source: "/etc/app", Target: "/etc/app", ReadOnly: true},
		},
		Network: "lan", ExtraNetworks: []string{"backend"}, IP: "10.1.0.5",
		CPUs: 1.5, MemoryBytes: 256 << 20, Restart: "unless-stopped",
		CapAdd: []string{"CAP_NET_ADMIN"}, Devices: []string{"/dev/net/tun"},
		DNS: []string{"1.1.1.1"}, ExtraHosts: []string{"db:10.0.0.9"},
		Labels: map[string]string{"app": "web"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("spec\n got %+v\nwant %+v", got, want)
	}
	if len(unsupported) != 0 {
		t.Errorf("unsupported %v", unsupported)
	}
	if _, err := CreateArgs(got); err != nil {
		t.Errorf("reconstructed spec is invalid: %v", err)
	}

	// Overridden command and entrypoint, and settings the form cannot express.
	oci.Process.Args = []string{"/bin/sh", "-c", "echo hi"}
	oci.Process.Cwd = "/srv"
	c.HostConfig.AutoRemove = true
	c.HostConfig.Sysctls = map[string]string{"net.ipv4.ip_forward": "1"}
	labels["nerdctl/mac-address"] = "02:42:ac:11:00:02"
	got, unsupported = specFrom(c, oci, img, "docker.io/library/nginx:1.27", labels)
	if got.Entrypoint != "/bin/sh" || !reflect.DeepEqual(got.Command, []string{"-c", "echo hi"}) || got.Workdir != "/srv" {
		t.Errorf("entrypoint %q command %q workdir %q", got.Entrypoint, got.Command, got.Workdir)
	}
	joined := strings.Join(unsupported, "; ")
	for _, want := range []string{"--rm", "sysctls net.ipv4.ip_forward", "mac-address"} {
		if !strings.Contains(joined, want) {
			t.Errorf("unsupported %q lacks %q", joined, want)
		}
	}

	// A command after the image's entrypoint.
	oci.Process.Args = []string{"/docker-entrypoint.sh", "nginx-debug"}
	got, _ = specFrom(c, oci, img, "x", labels)
	if got.Entrypoint != "" || !reflect.DeepEqual(got.Command, []string{"nginx-debug"}) {
		t.Errorf("command after entrypoint: %q %q", got.Entrypoint, got.Command)
	}
}
