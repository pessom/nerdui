// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"slices"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
)

func TestCreateArgs(t *testing.T) {
	args, err := CreateArgs(CreateSpec{
		Image:      "nginx:1.27",
		Name:       "web",
		Entrypoint: "/docker-entrypoint.sh",
		Command:    []string{"nginx", "-g", "daemon off;"},
		Ports: []PortSpec{
			{HostPort: 8080, ContainerPort: 80},
			{HostIP: "127.0.0.1", HostPort: 8443, ContainerPort: 443},
			{HostIP: "::1", ContainerPort: 53, Protocol: "udp"},
			{ContainerPort: 9000},
		},
		Env:         []EnvVar{{Name: "TOKEN", Value: "a=b --rm"}, {Name: "EMPTY"}},
		Mounts:      []MountSpec{{Type: "volume", Source: "data", Target: "/data"}, {Type: "bind", Source: "/etc/web.conf", Target: "/etc/nginx/nginx.conf", ReadOnly: true}, {Type: "tmpfs", Target: "/tmp"}},
		Network:     "bridge",
		Hostname:    "web-1",
		User:        "101:101",
		Workdir:     "/srv",
		CPUs:        1.5,
		MemoryBytes: 256 << 20,
		Restart:     "on-failure:3",
		Labels:      map[string]string{"b": "2", "a": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--name=web", "--entrypoint=/docker-entrypoint.sh",
		"--publish=8080:80/tcp", "--publish=127.0.0.1:8443:443/tcp", "--publish=[::1]::53/udp", "--publish=9000/tcp",
		"--env=TOKEN=a=b --rm", "--env=EMPTY=",
		"--mount=type=volume,source=data,target=/data", "--mount=type=bind,source=/etc/web.conf,target=/etc/nginx/nginx.conf,readonly", "--mount=type=tmpfs,target=/tmp",
		"--network=bridge", "--hostname=web-1", "--user=101:101", "--workdir=/srv",
		"--cpus=1.5", "--memory=268435456", "--restart=on-failure:3", "--label=a=1", "--label=b=2",
		"nginx:1.27", "nginx", "-g", "daemon off;",
	}
	if !slices.Equal(args, want) {
		t.Errorf("args\n got %q\nwant %q", args, want)
	}
	// Every option is one --flag=value token, so no value can become a flag of its own.
	for _, a := range args[:len(args)-4] {
		if !strings.HasPrefix(a, "--") || !strings.Contains(a, "=") {
			t.Errorf("option %q is not --flag=value", a)
		}
	}

	cmd, err := RunCommand(CreateSpec{Image: "alpine", Env: []EnvVar{{Name: "MSG", Value: "it's here"}}, Command: []string{"sh", "-c", "echo $MSG"}})
	if err != nil || cmd != `nerdctl run -d '--env=MSG=it'\''s here' alpine sh -c 'echo $MSG'` {
		t.Errorf("run command %q %v", cmd, err)
	}
}

func TestCreateArgsRejects(t *testing.T) {
	for name, s := range map[string]CreateSpec{
		"no image":             {},
		"flag as image":        {Image: "--privileged"},
		"bad name":             {Image: "a", Name: "-x"},
		"env name":             {Image: "a", Env: []EnvVar{{Name: "A B"}}},
		"env twice":            {Image: "a", Env: []EnvVar{{Name: "A"}, {Name: "A"}}},
		"port range":           {Image: "a", Ports: []PortSpec{{ContainerPort: 70000}}},
		"protocol":             {Image: "a", Ports: []PortSpec{{ContainerPort: 1, Protocol: "sctp"}}},
		"host ip":              {Image: "a", Ports: []PortSpec{{ContainerPort: 1, HostIP: "evil,host"}}},
		"relative target":      {Image: "a", Mounts: []MountSpec{{Type: "tmpfs", Target: "tmp"}}},
		"mount option smuggle": {Image: "a", Mounts: []MountSpec{{Type: "bind", Source: "/x,bind-propagation=shared", Target: "/y"}}},
		"relative bind":        {Image: "a", Mounts: []MountSpec{{Type: "bind", Source: "x", Target: "/y"}}},
		"mount type":           {Image: "a", Mounts: []MountSpec{{Type: "image", Target: "/y"}}},
		"network":              {Image: "a", Network: "--privileged"},
		"user":                 {Image: "a", User: "root --privileged"},
		"workdir":              {Image: "a", Workdir: "srv"},
		"memory":               {Image: "a", MemoryBytes: 1024},
		"cpus":                 {Image: "a", CPUs: -1},
		"restart":              {Image: "a", Restart: "sometimes"},
		"reserved label":       {Image: "a", Labels: map[string]string{"nerdctl/name": "x"}},
		"label newline":        {Image: "a", Labels: map[string]string{"a": "b\nc"}},
		"command NUL":          {Image: "a", Command: []string{"a\x00b"}},
	} {
		if _, err := CreateArgs(s); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
