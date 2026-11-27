// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package templates

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/pessom/nerdui/internal/runtime"
)

func TestBuiltinContainers(t *testing.T) {
	ctx := context.Background()
	single := 0
	for _, tpl := range Builtin() {
		if len(Services(tpl.Compose)) != 1 {
			continue
		}
		single++
		values := map[string]string{}
		for _, v := range tpl.Variables {
			values[v.Name] = v.Default
			if v.Default == "" {
				values[v.Name] = "secret-value"
			}
		}
		c, err := ToContainer(ctx, tpl.Compose, values, "")
		if err != nil {
			t.Errorf("%s: %v", tpl.ID, err)
			continue
		}
		if c.Spec.Image == "" || c.Spec.Name == "" || len(c.Unsupported) > 0 {
			t.Errorf("%s: %+v", tpl.ID, c)
		}
		if _, err := runtime.CreateArgs(c.Spec); err != nil {
			t.Errorf("%s: create arguments: %v", tpl.ID, err)
		}
	}
	if single < 4 {
		t.Errorf("only %d one-service built-in templates", single)
	}
}

func TestToContainer(t *testing.T) {
	compose := `
services:
  web:
    image: nginx:${TAG:-1}
    container_name: site
    entrypoint: ["/docker-entrypoint.sh", "-x"]
    command: nginx -g 'daemon off;'
    environment:
      PASSWORD: ${PASSWORD:?set it}
      FROM_HOST:
    ports: ["8080:80", "127.0.0.1::443/udp", "9000-9001:9000-9001"]
    volumes:
      - data:/data
      - shared:/shared:ro
      - ./conf:/etc/nginx/conf.d
      - /srv/www:/www
      - type: tmpfs
        target: /cache
    networks: [default, backend]
    restart: unless-stopped
    mem_limit: 256m
    labels: { nerdui.auto-update: "true" }
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost/a b"]
      interval: 30s
      retries: 3
    sysctls: { net.core.somaxconn: 1024 }
volumes:
  data:
  shared:
    external: true
    name: shared-data
networks:
  backend:
`
	c, err := ToContainer(context.Background(), compose, map[string]string{"PASSWORD": "pw"}, "")
	if err != nil {
		t.Fatal(err)
	}
	s := c.Spec
	if s.Name != "site" || s.Image != "nginx:1" || s.Entrypoint != "/docker-entrypoint.sh" ||
		!slices.Equal(s.Command, []string{"-x", "nginx", "-g", "daemon off;"}) || s.Restart != "unless-stopped" ||
		s.MemoryBytes != 256<<20 || s.Labels["nerdui.auto-update"] != "true" {
		t.Errorf("spec = %+v", s)
	}
	if !reflect.DeepEqual(s.Env, []runtime.EnvVar{{Name: "PASSWORD", Value: "pw"}}) {
		t.Errorf("env = %+v", s.Env)
	}
	if !reflect.DeepEqual(s.Ports, []runtime.PortSpec{
		{HostPort: 8080, ContainerPort: 80}, {HostIP: "127.0.0.1", ContainerPort: 443, Protocol: "udp"},
		{HostPort: 9000, ContainerPort: 9000}, {HostPort: 9001, ContainerPort: 9001}, // ranges are expanded
	}) {
		t.Errorf("ports = %+v", s.Ports)
	}
	if !reflect.DeepEqual(s.Mounts, []runtime.MountSpec{
		{Type: "volume", Source: "site-data", Target: "/data"},
		{Type: "volume", Source: "shared-data", Target: "/shared", ReadOnly: true},
		{Type: "bind", Source: "/srv/www", Target: "/www"},
		{Type: "tmpfs", Target: "/cache"},
	}) {
		t.Errorf("mounts = %+v", s.Mounts)
	}
	if h := s.Healthcheck; h == nil || h.Command != "curl -f 'http://localhost/a b'" || h.Interval != "30s" || h.Retries != 3 {
		t.Errorf("healthcheck = %+v", h)
	}
	want := []string{
		"environment: FROM_HOST takes its value from the host's environment",
		"networks: backend",
		"sysctls",
		"volumes: ./conf:/etc/nginx/conf.d (relative to the stack's directory)",
	}
	if !slices.Equal(c.Unsupported, want) {
		t.Errorf("unsupported = %q", c.Unsupported)
	}

	for _, bad := range []string{"services: {}", "services:\n  a: {image: x}\n  b: {image: y}", "services: [", "services:\n  a:\n    image: ${X:?set X}"} {
		if _, err := ToContainer(context.Background(), bad, nil, ""); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestFromContainerRoundTrip(t *testing.T) {
	spec := runtime.CreateSpec{
		Image: "ghcr.io/example/app:1", Name: "My App", Command: []string{"serve", "--price=$5"},
		Env:   []runtime.EnvVar{{Name: "DB_PASSWORD", Value: "hunter2"}, {Name: "MODE", Value: "prod"}},
		Ports: []runtime.PortSpec{{HostPort: 8080, ContainerPort: 80}, {HostIP: "127.0.0.1", ContainerPort: 53, Protocol: "udp"}},
		Mounts: []runtime.MountSpec{
			{Type: "volume", Source: "My App-appdata", Target: "/data"}, {Type: "bind", Source: "/etc/app", Target: "/etc/app", ReadOnly: true},
			{Type: "tmpfs", Target: "/tmp"},
		},
		Restart: "always", Labels: map[string]string{"nerdui.auto-heal": "true"},
		Healthcheck: &runtime.HealthcheckSpec{Command: "wget -q -O- localhost", Interval: "10s", Retries: 2},
		CapAdd:      []string{"NET_ADMIN"}, MemoryBytes: 1 << 30,
	}
	tpl := FromContainer(spec, nil)
	if strings.Contains(tpl.Compose+tpl.Env, "hunter2") {
		t.Fatalf("secret saved:\n%s\n%s", tpl.Compose, tpl.Env)
	}
	if !strings.Contains(tpl.Compose, "\n  my-app:\n") || !strings.Contains(tpl.Compose, "- appdata:/data") || tpl.Env != "# DB_PASSWORD of the container (secret)\nDB_PASSWORD=\n" {
		t.Errorf("compose:\n%s\nenv:\n%s", tpl.Compose, tpl.Env)
	}
	if got := Services(tpl.Compose); !slices.Equal(got, []string{"my-app"}) {
		t.Errorf("services = %v", got)
	}
	back, err := ToContainer(context.Background(), tpl.Compose, map[string]string{"DB_PASSWORD": "new"}, "copy")
	if err != nil {
		t.Fatalf("%v\n%s", err, tpl.Compose)
	}
	want := spec
	want.Name = "copy"
	want.Env = []runtime.EnvVar{{Name: "DB_PASSWORD", Value: "new"}, {Name: "MODE", Value: "prod"}}
	want.Mounts[0].Source = "copy-appdata"
	if len(back.Unsupported) > 0 || !reflect.DeepEqual(back.Spec, want) {
		t.Errorf("round trip:\n got %+v\nwant %+v\nunsupported %v\n%s", back.Spec, want, back.Unsupported, tpl.Compose)
	}
}
