// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package templates

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/errdefs"
	"go.yaml.in/yaml/v3"

	"github.com/pessom/nerdui/internal/runtime"
)

// Containers from templates and templates from containers (#19, ADR-0059).

// Services lists the services of a compose file in file order; nil when it cannot be read.
func Services(compose string) []string {
	var doc yaml.Node
	if yaml.Unmarshal([]byte(compose), &doc) != nil || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "services" || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		var out []string
		m := root.Content[i+1]
		for j := 0; j+1 < len(m.Content); j += 2 {
			out = append(out, m.Content[j].Value)
		}
		return out
	}
	return nil
}

// ContainerFromTemplate is a template's only service as the create request of a container.
// Keep in sync with api/openapi.yaml.
type ContainerFromTemplate struct {
	Spec runtime.CreateSpec `json:"spec"`
	// Unsupported lists settings of the service that a container from the form lacks.
	Unsupported []string `json:"unsupported"`
}

// workDir stands for the directory a stack would have: bind mounts under it were relative.
const workDir = "/nerdui-template"

var nameRe = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

// ToContainer loads compose with the variables values (and nothing from nerdui's own
// environment) and converts its only service into a container named name (by default the
// service's container_name or name). Named volumes become "<name>-<volume>".
func ToContainer(ctx context.Context, compose string, values map[string]string, name string) (ContainerFromTemplate, error) {
	env := types.Mapping{}
	maps.Copy(env, values)
	p, err := loader.LoadWithContext(ctx, types.ConfigDetails{
		WorkingDir:  workDir,
		ConfigFiles: []types.ConfigFile{{Filename: "compose.yaml", Content: []byte(compose)}},
		Environment: env,
	}, func(o *loader.Options) {
		o.SetProjectName("template", true)
		o.SkipResolveEnvironment = false
	})
	if err != nil {
		return ContainerFromTemplate{}, fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	if len(p.Services) != 1 {
		return ContainerFromTemplate{}, fmt.Errorf("%w: a container needs a template with one service, this one has %d",
			errdefs.ErrInvalidArgument, len(p.Services))
	}
	var svc types.ServiceConfig
	for _, s := range p.Services {
		svc = s
	}
	if name == "" {
		name = svc.ContainerName
	}
	if name == "" {
		name = svc.Name
	}
	name = strings.Trim(nameRe.ReplaceAllString(name, "-"), "-._")
	c := converter{name: name, project: p}
	c.convert(svc)
	if c.unsupported == nil {
		c.unsupported = []string{}
	}
	return ContainerFromTemplate{Spec: c.spec, Unsupported: c.unsupported}, nil
}

type converter struct {
	name        string
	project     *types.Project
	spec        runtime.CreateSpec
	unsupported []string
}

func (c *converter) skip(format string, args ...any) {
	c.unsupported = append(c.unsupported, fmt.Sprintf(format, args...))
}

func (c *converter) convert(s types.ServiceConfig) {
	sp := &c.spec
	sp.Name, sp.Image = c.name, s.Image
	if s.Image == "" {
		c.skip("build: the service has no image")
	}
	if len(s.Entrypoint) > 0 {
		sp.Entrypoint = s.Entrypoint[0]
		sp.Command = append(slices.Clone(s.Entrypoint[1:]), s.Command...)
	} else {
		sp.Command = slices.Clone(s.Command)
	}
	for _, k := range slices.Sorted(maps.Keys(s.Environment)) {
		if v := s.Environment[k]; v != nil {
			sp.Env = append(sp.Env, runtime.EnvVar{Name: k, Value: *v})
		} else {
			c.skip("environment: %s takes its value from the host's environment", k)
		}
	}
	for _, port := range s.Ports {
		c.port(port)
	}
	for _, v := range s.Volumes {
		c.volume(v)
	}
	for _, t := range s.Tmpfs {
		target, _, _ := strings.Cut(t, ":")
		sp.Mounts = append(sp.Mounts, runtime.MountSpec{Type: "tmpfs", Target: target})
	}
	switch s.NetworkMode {
	case "", "bridge", "default":
	case "host", "none":
		sp.Network = s.NetworkMode
	default:
		c.skip("network_mode: %s", s.NetworkMode)
	}
	for n, cfg := range s.Networks {
		if n != "default" {
			c.skip("networks: %s", n)
		} else if cfg != nil && (cfg.Ipv4Address != "" || len(cfg.Aliases) > 0) {
			c.skip("networks: settings of the default network")
		}
	}
	sp.Hostname, sp.User, sp.Workdir, sp.Restart = s.Hostname, s.User, s.WorkingDir, s.Restart
	sp.CPUs, sp.MemoryBytes = float64(s.CPUS), int64(s.MemLimit)
	if d := s.Deploy; d != nil {
		if l := d.Resources.Limits; l != nil {
			if sp.CPUs == 0 {
				sp.CPUs = float64(l.NanoCPUs)
			}
			if sp.MemoryBytes == 0 {
				sp.MemoryBytes = int64(l.MemoryBytes)
			}
		}
		rest := *d
		rest.Resources.Limits = nil
		if !reflect.ValueOf(rest).IsZero() {
			c.skip("deploy: settings besides resource limits")
		}
	}
	for k, v := range s.Labels {
		if !strings.HasPrefix(k, "com.docker.compose.") {
			if sp.Labels == nil {
				sp.Labels = map[string]string{}
			}
			sp.Labels[k] = v
		}
	}
	sp.Privileged = s.Privileged
	sp.CapAdd, sp.CapDrop = slices.Clone(s.CapAdd), slices.Clone(s.CapDrop)
	for _, d := range s.Devices {
		dev := d.Source + ":" + d.Target
		if d.Permissions != "" {
			dev += ":" + d.Permissions
		}
		sp.Devices = append(sp.Devices, dev)
	}
	sp.DNS = slices.Clone(s.DNS)
	for _, host := range slices.Sorted(maps.Keys(s.ExtraHosts)) {
		for _, ip := range s.ExtraHosts[host] {
			sp.ExtraHosts = append(sp.ExtraHosts, host+":"+ip)
		}
	}
	c.healthcheck(s.HealthCheck)
	c.rest(s)
}

func (c *converter) port(p types.ServicePortConfig) {
	ps := runtime.PortSpec{HostIP: p.HostIP, ContainerPort: int(p.Target), Protocol: p.Protocol}
	if ps.Protocol == "tcp" {
		ps.Protocol = ""
	}
	if p.Published != "" {
		n, err := strconv.Atoi(p.Published)
		if err != nil {
			c.skip("ports: %s:%d (a range)", p.Published, p.Target)
			return
		}
		ps.HostPort = n
	}
	c.spec.Ports = append(c.spec.Ports, ps)
}

func (c *converter) volume(v types.ServiceVolumeConfig) {
	m := runtime.MountSpec{Type: v.Type, Target: v.Target, ReadOnly: v.ReadOnly}
	switch v.Type {
	case types.VolumeTypeVolume:
		if v.Source != "" {
			m.Source = c.name + "-" + v.Source
			if vol, ok := c.project.Volumes[v.Source]; ok && bool(vol.External) {
				m.Source = vol.Name
			}
		}
	case types.VolumeTypeBind:
		if v.Source == workDir || strings.HasPrefix(v.Source, workDir+"/") {
			c.skip("volumes: %s (relative to the stack's directory)", "."+strings.TrimPrefix(v.Source, workDir)+":"+v.Target)
			return
		}
		m.Source = v.Source
	case types.VolumeTypeTmpfs:
	default:
		c.skip("volumes: %s of type %s", v.Target, v.Type)
		return
	}
	c.spec.Mounts = append(c.spec.Mounts, m)
}

func (c *converter) healthcheck(h *types.HealthCheckConfig) {
	if h == nil {
		return
	}
	if h.Disable || (len(h.Test) > 0 && h.Test[0] == "NONE") {
		c.spec.Healthcheck = &runtime.HealthcheckSpec{Disabled: true}
		return
	}
	hc := &runtime.HealthcheckSpec{}
	switch {
	case len(h.Test) > 1 && h.Test[0] == "CMD-SHELL":
		hc.Command = strings.Join(h.Test[1:], " ")
	case len(h.Test) > 1 && h.Test[0] == "CMD":
		hc.Command = shellJoin(h.Test[1:])
	case len(h.Test) > 0:
		hc.Command = strings.Join(h.Test, " ")
	}
	dur := func(d *types.Duration) string {
		if d == nil || *d == 0 {
			return ""
		}
		return time.Duration(*d).String()
	}
	hc.Interval, hc.Timeout, hc.StartPeriod = dur(h.Interval), dur(h.Timeout), dur(h.StartPeriod)
	if h.Retries != nil {
		hc.Retries = int(min(*h.Retries, 1000)) // the form allows up to 100
	}
	c.spec.Healthcheck = hc
}

// rest reports every other setting the service has.
func (c *converter) rest(s types.ServiceConfig) {
	handled := map[string]bool{
		"name": true, "container_name": true, "image": true, "entrypoint": true, "command": true,
		"environment": true, "ports": true, "volumes": true, "tmpfs": true, "network_mode": true,
		"networks": true, "hostname": true, "user": true, "working_dir": true, "cpus": true,
		"mem_limit": true, "deploy": true, "restart": true, "labels": true, "privileged": true,
		"cap_add": true, "cap_drop": true, "devices": true, "dns": true, "extra_hosts": true,
		"healthcheck": true, "build": true, "expose": true, "env_file": true,
		// Set by the loader for every service.
		"custom_labels": true, "label_file": true,
	}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		t := v.Type()
		for i := range t.NumField() {
			f := t.Field(i)
			tag, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
			if f.Anonymous && tag == "" {
				walk(v.Field(i))
				continue
			}
			if tag == "" || tag == "-" || handled[tag] || !f.IsExported() || v.Field(i).IsZero() {
				continue
			}
			if tag == "pull_policy" || strings.HasPrefix(tag, "x-") {
				continue
			}
			c.skip("%s", tag)
		}
	}
	walk(reflect.ValueOf(s))
	sort.Strings(c.unsupported)
}

// shellJoin quotes args for /bin/sh -c, as a healthcheck command runs.
func shellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a != "" && !strings.ContainsAny(a, " \t\n'\"\\$`|&;<>()*?[]#~=%!{}") {
			out[i] = a
		} else {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(out, " ")
}

// TemplateFromContainer is a one-service compose file and its .env.example made from a
// container's settings. Keep in sync with api/openapi.yaml.
type TemplateFromContainer struct {
	Compose string `json:"compose"`
	Env     string `json:"env"`
	// Unsupported lists settings of the container the template lacks.
	Unsupported []string `json:"unsupported"`
}

type composeHealth struct {
	Test        []string `yaml:"test,omitempty,flow"`
	Interval    string   `yaml:"interval,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"`
	StartPeriod string   `yaml:"start_period,omitempty"`
	Retries     int      `yaml:"retries,omitempty"`
	Disable     bool     `yaml:"disable,omitempty"`
}

type composeService struct {
	Image       string            `yaml:"image"`
	Entrypoint  []string          `yaml:"entrypoint,omitempty,flow"`
	Command     []string          `yaml:"command,omitempty,flow"`
	Restart     string            `yaml:"restart,omitempty"`
	Hostname    string            `yaml:"hostname,omitempty"`
	User        string            `yaml:"user,omitempty"`
	WorkingDir  string            `yaml:"working_dir,omitempty"`
	Environment []string          `yaml:"environment,omitempty"`
	Ports       []string          `yaml:"ports,omitempty"`
	Volumes     []string          `yaml:"volumes,omitempty"`
	Tmpfs       []string          `yaml:"tmpfs,omitempty"`
	NetworkMode string            `yaml:"network_mode,omitempty"`
	Networks    []string          `yaml:"networks,omitempty"`
	CPUs        float64           `yaml:"cpus,omitempty"`
	MemLimit    int64             `yaml:"mem_limit,omitempty"`
	Privileged  bool              `yaml:"privileged,omitempty"`
	CapAdd      []string          `yaml:"cap_add,omitempty"`
	CapDrop     []string          `yaml:"cap_drop,omitempty"`
	Devices     []string          `yaml:"devices,omitempty"`
	DNS         []string          `yaml:"dns,omitempty"`
	ExtraHosts  []string          `yaml:"extra_hosts,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Healthcheck *composeHealth    `yaml:"healthcheck,omitempty"`
}

type external struct {
	External bool `yaml:"external"`
}

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
	Volumes  map[string]struct{}       `yaml:"volumes,omitempty"`
	Networks map[string]external       `yaml:"networks,omitempty"`
}

// esc keeps a literal value from being read as a variable.
func esc(s string) string { return strings.ReplaceAll(s, "$", "$$") }

func escAll(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = esc(s)
	}
	return out
}

// FromContainer turns a container's settings into a one-service compose file. Environment
// variables named like secrets become variables without a value in .env.example; named
// volumes become volumes of the stack, other networks external ones.
func FromContainer(s runtime.CreateSpec, unsupported []string) TemplateFromContainer {
	service := strings.Trim(strings.ToLower(nameRe.ReplaceAllString(s.Name, "-")), "-._")
	if service == "" {
		service = "app"
	}
	svc := composeService{
		Image: esc(s.Image), Restart: s.Restart, Hostname: s.Hostname, User: s.User, WorkingDir: esc(s.Workdir),
		CPUs: s.CPUs, MemLimit: s.MemoryBytes, Privileged: s.Privileged,
		CapAdd: s.CapAdd, CapDrop: s.CapDrop, Devices: s.Devices, DNS: s.DNS, ExtraHosts: s.ExtraHosts,
	}
	if s.Entrypoint != "" {
		svc.Entrypoint = []string{esc(s.Entrypoint)}
	}
	svc.Command = escAll(s.Command)
	var env strings.Builder
	for _, e := range s.Env {
		if secretRe.MatchString(e.Name) {
			svc.Environment = append(svc.Environment, e.Name+"=${"+e.Name+":?set "+e.Name+"}")
			fmt.Fprintf(&env, "# %s of the container (secret)\n%s=\n", e.Name, e.Name)
			continue
		}
		svc.Environment = append(svc.Environment, e.Name+"="+esc(e.Value))
	}
	for _, p := range s.Ports {
		v := strconv.Itoa(p.ContainerPort)
		if p.HostPort != 0 {
			v = strconv.Itoa(p.HostPort) + ":" + v
		}
		if p.HostIP != "" {
			if p.HostPort == 0 {
				v = ":" + v
			}
			v = p.HostIP + ":" + v
		}
		if p.Protocol != "" && p.Protocol != "tcp" {
			v += "/" + p.Protocol
		}
		svc.Ports = append(svc.Ports, v)
	}
	f := composeFile{Services: map[string]composeService{}}
	// A volume named "<container>-data", as ToContainer names them, is "data" of the template.
	volume := func(src string) string {
		if rest, ok := strings.CutPrefix(src, s.Name+"-"); ok && s.Name != "" && rest != "" {
			return rest
		}
		return src
	}
	for _, m := range s.Mounts {
		switch m.Type {
		case "tmpfs":
			svc.Tmpfs = append(svc.Tmpfs, m.Target)
			continue
		case "volume":
			if m.Source == "" {
				svc.Volumes = append(svc.Volumes, esc(m.Target))
				continue
			}
			m.Source = volume(m.Source)
			if f.Volumes == nil {
				f.Volumes = map[string]struct{}{}
			}
			f.Volumes[m.Source] = struct{}{}
		}
		v := esc(m.Source) + ":" + esc(m.Target)
		if m.ReadOnly {
			v += ":ro"
		}
		svc.Volumes = append(svc.Volumes, v)
	}
	switch s.Network {
	case "", "bridge", "nerdctl0":
	case "host", "none":
		svc.NetworkMode = s.Network
	default:
		svc.Networks = append(svc.Networks, s.Network)
	}
	svc.Networks = append(svc.Networks, s.ExtraNetworks...)
	for _, n := range svc.Networks {
		if f.Networks == nil {
			f.Networks = map[string]external{}
		}
		f.Networks[n] = external{External: true}
	}
	if s.IP != "" {
		unsupported = append(unsupported, "static IP address "+s.IP)
	}
	if len(s.Labels) > 0 {
		svc.Labels = map[string]string{}
		for k, v := range s.Labels {
			svc.Labels[k] = esc(v)
		}
	}
	if h := s.Healthcheck; h != nil {
		if h.Disabled {
			svc.Healthcheck = &composeHealth{Disable: true}
		} else {
			svc.Healthcheck = &composeHealth{
				Interval: h.Interval, Timeout: h.Timeout, StartPeriod: h.StartPeriod, Retries: h.Retries,
			}
			if h.Command != "" {
				svc.Healthcheck.Test = []string{"CMD-SHELL", esc(h.Command)}
			}
		}
	}
	f.Services[service] = svc
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	_ = enc.Encode(f)
	if unsupported == nil {
		unsupported = []string{}
	}
	return TemplateFromContainer{Compose: b.String(), Env: env.String(), Unsupported: unsupported}
}
