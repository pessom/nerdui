// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/inspecttypes/dockercompat"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// ContainerSpec is what recreating a container would create (#18, ADR-0031): its settings as
// a CreateSpec, and the settings that cannot be carried over, which block recreating.
type ContainerSpec struct {
	// ID is the container's full ID.
	ID   string     `json:"id"`
	Spec CreateSpec `json:"spec"`
	// Unsupported lists settings the create form cannot express; recreating would lose them.
	Unsupported []string `json:"unsupported"`
	// Running tells whether the container runs now (the new one is started if so).
	Running bool `json:"running"`
	// Stack is the compose project managing the container: recreate it through the stack.
	Stack string `json:"stack,omitempty"`
	// ImageChanged: the image tag points elsewhere than when the container was created, so
	// values equal to the old image's defaults may be kept explicitly.
	ImageChanged bool `json:"imageChanged,omitempty"`
}

const composeProjectLabel = "com.docker.compose.project"

// defaultShmSize is nerdctl's default /dev/shm size.
const defaultShmSize = 64 << 20

// Spec reconstructs the settings a container was created with (#18). nerdctl keeps no record
// of the original command line: the settings come from `nerdctl inspect` (labels nerdctl
// records and the OCI spec), and values equal to the image's defaults are left out, so a
// recreated container follows a new image's defaults.
func (cc clientContainers) Spec(ctx context.Context, namespace, ref string) (ContainerSpec, error) {
	raw, err := cc.Inspect(ctx, namespace, ref, false)
	if err != nil {
		return ContainerSpec{}, err
	}
	c, ok := raw.(*dockercompat.Container)
	if !ok {
		return ContainerSpec{}, fmt.Errorf("inspect %s: unexpected %T", ref, raw)
	}
	cl, err := cc.c.conn()
	if err != nil {
		return ContainerSpec{}, err
	}
	nctx := namespaces.WithNamespace(ctx, namespace)
	ctr, err := cl.LoadContainer(nctx, c.ID)
	if err != nil {
		return ContainerSpec{}, fmt.Errorf("load container %s: %w", ref, classify(err))
	}
	oci, err := ctr.Spec(nctx)
	if err != nil {
		return ContainerSpec{}, fmt.Errorf("spec of %s: %w", ref, classify(err))
	}
	info, err := ctr.Info(nctx)
	if err != nil {
		return ContainerSpec{}, fmt.Errorf("info of %s: %w", ref, classify(err))
	}

	var img ocispec.ImageConfig
	imageChanged := false
	if i, err := cl.GetImage(nctx, info.Image); err == nil {
		if ic, err := i.Spec(nctx); err == nil {
			img = ic.Config
		}
		if d := info.Labels[imageDigestLabel]; d != "" && d != i.Target().Digest.String() {
			imageChanged = true
		}
	}

	out := ContainerSpec{
		ID:           c.ID,
		Running:      c.State != nil && c.State.Running,
		Stack:        info.Labels[composeProjectLabel],
		ImageChanged: imageChanged,
	}
	out.Spec, out.Unsupported = specFrom(c, oci, img, info.Image, info.Labels)
	return out, nil
}

// specFrom maps an inspected container to a CreateSpec; img is the configuration of its image.
func specFrom(c *dockercompat.Container, oci *specs.Spec, img ocispec.ImageConfig, image string, labels map[string]string) (CreateSpec, []string) {
	s := CreateSpec{Image: image, Name: strings.TrimPrefix(c.Name, "/")}
	var unsupported []string
	add := func(format string, args ...any) { unsupported = append(unsupported, fmt.Sprintf(format, args...)) }
	hc := c.HostConfig
	if hc == nil {
		hc = &dockercompat.HostConfig{}
	}

	// Entrypoint and command: nerdctl's --entrypoint takes one value; with it, the image's
	// command is not used.
	if oci.Process != nil {
		args := oci.Process.Args
		def := append(slices.Clone(img.Entrypoint), img.Cmd...)
		switch {
		case slices.Equal(args, def):
		case len(img.Entrypoint) > 0 && len(args) >= len(img.Entrypoint) && slices.Equal(args[:len(img.Entrypoint)], img.Entrypoint):
			s.Command = slices.Clone(args[len(img.Entrypoint):])
		case len(img.Entrypoint) == 0:
			s.Command = slices.Clone(args)
		case len(args) > 0:
			s.Entrypoint = args[0]
			s.Command = slices.Clone(args[1:])
		}
		if cwd := oci.Process.Cwd; cwd != "" && cwd != img.WorkingDir && (img.WorkingDir != "" || cwd != "/") {
			s.Workdir = cwd
		}
	}

	// Environment: variables of the image with the same value are its defaults.
	imageEnv := map[string]bool{}
	for _, e := range img.Env {
		imageEnv[e] = true
	}
	hostname := labels["nerdctl/hostname"]
	if c.Config != nil {
		for _, e := range c.Config.Env {
			name, value, _ := strings.Cut(e, "=")
			if imageEnv[e] || (name == "HOSTNAME" && (value == hostname || value == shortID(c.ID))) {
				continue
			}
			s.Env = append(s.Env, EnvVar{Name: name, Value: value})
		}
		if hn := c.Config.Hostname; hn != "" && hn != shortID(c.ID) {
			s.Hostname = hn
		}
		if c.Config.Domainname != "" {
			add("domain name %s", c.Config.Domainname)
		}
		s.User = c.Config.User
	}

	// Ports.
	ports := slices.Collect(maps.Keys(hc.PortBindings))
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	for _, port := range ports {
		p := string(port)
		portProto := strings.SplitN(p, "/", 2)
		cport, err := strconv.Atoi(portProto[0])
		if err != nil {
			add("port %s", p)
			continue
		}
		proto := "tcp"
		if len(portProto) == 2 {
			proto = portProto[1]
		}
		for _, b := range hc.PortBindings[port] {
			ps := PortSpec{ContainerPort: cport, Protocol: proto}
			if b.HostIP != "" && b.HostIP != "0.0.0.0" {
				ps.HostIP = b.HostIP
			}
			if b.HostPort != "" {
				hp, err := strconv.Atoi(b.HostPort)
				if err != nil {
					add("port range %s", b.HostPort)
					continue
				}
				ps.HostPort = hp
			}
			s.Ports = append(s.Ports, ps)
		}
	}

	// Mounts recorded by nerdctl.
	for _, m := range c.Mounts {
		switch m.Type {
		case "volume":
			s.Mounts = append(s.Mounts, MountSpec{Type: "volume", Source: m.Name, Target: m.Destination, ReadOnly: !m.RW})
		case "bind":
			s.Mounts = append(s.Mounts, MountSpec{Type: "bind", Source: m.Source, Target: m.Destination, ReadOnly: !m.RW})
		case "tmpfs":
			opts := slices.DeleteFunc(strings.Split(m.Mode, ","), func(o string) bool {
				return o == "" || o == "rw" || o == "ro" || o == "noexec" || o == "nosuid" || o == "nodev"
			})
			if len(opts) > 0 {
				add("tmpfs options on %s (%s)", m.Destination, strings.Join(opts, ","))
			}
			s.Mounts = append(s.Mounts, MountSpec{Type: "tmpfs", Target: m.Destination, ReadOnly: !m.RW && strings.Contains(m.Mode, "ro")})
		default:
			add("%s mount on %s", m.Type, m.Destination)
		}
	}

	// Networks: the first one is the network mode, the others are extra networks.
	networks := parseJSONList(labels["nerdctl/networks"])
	for i, n := range networks {
		if strings.HasPrefix(n, "container:") || strings.HasPrefix(n, "ns:") {
			add("network %s", n)
			continue
		}
		if i == 0 {
			s.Network = n
		} else {
			s.ExtraNetworks = append(s.ExtraNetworks, n)
		}
	}
	if ip := labels["nerdctl/ip"]; ip != "" {
		s.IP = ip
	}
	for _, l := range []string{"nerdctl/ip6", "nerdctl/mac-address"} {
		if labels[l] != "" {
			add("%s %s", strings.TrimPrefix(l, "nerdctl/"), labels[l])
		}
	}

	// Resources.
	if hc.CPUQuota > 0 {
		period := hc.CPUPeriod
		if period == 0 {
			period = 100000
		}
		s.CPUs = float64(hc.CPUQuota) / float64(period)
	}
	s.MemoryBytes = hc.Memory
	if hc.RestartPolicy.Name != "" {
		s.Restart = hc.RestartPolicy.Name
		if hc.RestartPolicy.Name == "on-failure" && hc.RestartPolicy.MaximumRetryCount > 0 {
			s.Restart += ":" + strconv.Itoa(hc.RestartPolicy.MaximumRetryCount)
		}
	}

	// Privileges, devices, name resolution.
	s.Privileged = hc.Privileged
	if !s.Privileged {
		s.CapAdd = slices.Clone(hc.CapAdd)
		s.CapDrop = slices.Clone(hc.CapDrop)
	}
	for _, d := range hc.Devices {
		dev := d.PathOnHost
		if d.PathInContainer != "" && d.PathInContainer != d.PathOnHost {
			dev += ":" + d.PathInContainer
		}
		if d.CgroupPermissions != "" && d.CgroupPermissions != "rwm" {
			if !strings.Contains(dev[1:], ":") {
				dev += ":" + d.PathOnHost
			}
			dev += ":" + d.CgroupPermissions
		}
		s.Devices = append(s.Devices, dev)
	}
	s.DNS = slices.Clone(hc.DNS)
	s.ExtraHosts = slices.Clone(hc.ExtraHosts)

	// User labels; nerdctl's and containerd's own are recreated by nerdctl.
	for k, v := range labels {
		if strings.HasPrefix(k, "nerdctl/") || strings.HasPrefix(k, "containerd.io/") ||
			strings.HasPrefix(k, "io.containerd.") || strings.HasPrefix(k, "com.docker.compose.") {
			continue
		}
		if img.Labels[k] == v {
			continue
		}
		if s.Labels == nil {
			s.Labels = map[string]string{}
		}
		s.Labels[k] = v
	}

	// Settings the form does not express.
	if hc.AutoRemove {
		add("automatic removal (--rm)")
	}
	if hc.ReadonlyRootfs {
		add("read-only root filesystem")
	}
	if len(hc.DNSOptions) > 0 || len(hc.DNSSearch) > 0 {
		add("DNS options or search domains")
	}
	sysctls := maps.Clone(hc.Sysctls)
	// nerdctl lets unprivileged processes bind low ports in container networks by default.
	if sysctls["net.ipv4.ip_unprivileged_port_start"] == "0" {
		delete(sysctls, "net.ipv4.ip_unprivileged_port_start")
	}
	if len(sysctls) > 0 {
		add("sysctls %s", strings.Join(slices.Sorted(maps.Keys(sysctls)), ", "))
	}
	if hc.PidMode == "host" || strings.HasPrefix(hc.PidMode, "container:") {
		add("PID namespace %s", hc.PidMode)
	}
	if hc.IpcMode == "host" || strings.HasPrefix(hc.IpcMode, "container:") {
		add("IPC namespace %s", hc.IpcMode)
	}
	if hc.ShmSize > 0 && hc.ShmSize != defaultShmSize {
		add("/dev/shm size %d", hc.ShmSize)
	}
	if hc.LogConfig.Driver != "" && hc.LogConfig.Driver != "json-file" {
		add("log driver %s", hc.LogConfig.Driver)
	}
	if len(hc.LogConfig.Opts) > 0 {
		add("log options")
	}
	if hc.Runtime != "" && hc.Runtime != "io.containerd.runc.v2" {
		add("runtime %s", hc.Runtime)
	}
	if hc.CPUSetCPUs != "" || hc.CPUSetMems != "" {
		add("CPU set")
	}
	if hc.CPUShares != 0 && hc.CPUShares != 1024 {
		add("CPU shares %d", hc.CPUShares)
	}
	if hc.MemoryReservation > 0 {
		add("memory reservation")
	}
	if hc.OomKillDisable || hc.OomScoreAdj != 0 {
		add("OOM settings")
	}
	if hc.PidsLimit > 0 {
		add("PIDs limit %d", hc.PidsLimit)
	}
	if len(hc.Annotations) > 0 {
		add("annotations")
	}
	return s, unsupported
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// ErrStackContainer is returned for containers of compose stacks: they are recreated by the
// stack.
var ErrStackContainer = fmt.Errorf("%w: the container belongs to a compose stack; edit or redeploy the stack", errdefs.ErrFailedPrecondition)

// parseJSONList reads a label holding a JSON array of strings; malformed values are empty.
func parseJSONList(v string) []string {
	var out []string
	if v == "" || json.Unmarshal([]byte(v), &out) != nil {
		return nil
	}
	return out
}
