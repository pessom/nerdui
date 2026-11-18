// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/netip"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/referenceutil"
)

// CreateSpec is the typed request for a new container (#17, ADR-0003). Every field becomes a
// separate argument of `nerdctl create`; nothing goes through a shell.
type CreateSpec struct {
	Image string `json:"image"`
	Name  string `json:"name,omitempty"`
	// Entrypoint replaces the image's entrypoint (first element) — nerdctl takes one value.
	Entrypoint string `json:"entrypoint,omitempty"`
	// Command replaces the image's command (argv after the image).
	Command  []string    `json:"command,omitempty"`
	Ports    []PortSpec  `json:"ports,omitempty"`
	Env      []EnvVar    `json:"env,omitempty"`
	Mounts   []MountSpec `json:"mounts,omitempty"`
	Network  string      `json:"network,omitempty"`
	Hostname string      `json:"hostname,omitempty"`
	User     string      `json:"user,omitempty"`
	Workdir  string      `json:"workdir,omitempty"`
	// CPUs and MemoryBytes are limits; 0 means none.
	CPUs        float64 `json:"cpus,omitempty"`
	MemoryBytes int64   `json:"memoryBytes,omitempty"`
	// Restart is no, always, unless-stopped or on-failure[:N].
	Restart string            `json:"restart,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`

	// Advanced settings (#18): extra networks after Network, a static IPv4 address in the
	// first network, privileges, devices and name resolution.
	ExtraNetworks []string `json:"extraNetworks,omitempty"`
	IP            string   `json:"ip,omitempty"`
	Privileged    bool     `json:"privileged,omitempty"`
	CapAdd        []string `json:"capAdd,omitempty"`
	CapDrop       []string `json:"capDrop,omitempty"`
	// Devices are host[:container[:permissions]], as in --device.
	Devices []string `json:"devices,omitempty"`
	DNS     []string `json:"dns,omitempty"`
	// ExtraHosts are name:address entries for /etc/hosts (--add-host); the address may be
	// host-gateway.
	ExtraHosts []string `json:"extraHosts,omitempty"`
}

// PortSpec publishes a container port.
type PortSpec struct {
	HostIP        string `json:"hostIp,omitempty"`
	HostPort      int    `json:"hostPort,omitempty"` // 0 = random
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol,omitempty"` // tcp (default) or udp
}

// EnvVar is an environment variable.
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// MountSpec is a volume, bind mount or tmpfs.
type MountSpec struct {
	Type     string `json:"type"` // volume, bind or tmpfs
	Source   string `json:"source,omitempty"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly,omitempty"`
}

var (
	containerNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	envNameRe       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	volumeNameRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	restartRe       = regexp.MustCompile(`^(no|always|unless-stopped|on-failure(:[0-9]+)?)$`)
	networkRe       = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]*$`)
	userRe          = regexp.MustCompile(`^[a-zA-Z0-9_.-]+(:[a-zA-Z0-9_.-]+)?$`)
	hostnameRe      = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9.-]{0,252}[a-zA-Z0-9])?$`)
	capabilityRe    = regexp.MustCompile(`^(?i:(CAP_)?[A-Z_]{2,32})$`)
	devicePathRe    = regexp.MustCompile(`^/[^:\x00\n\r]*$`)
	devicePermsRe   = regexp.MustCompile(`^[rwm]{1,3}$`)
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errdefs.ErrInvalidArgument, fmt.Sprintf(format, args...))
}

// noControl rejects values that could break a single argument (NUL) or mislead a reader.
func noControl(field, v string) error {
	if strings.ContainsAny(v, "\x00\n\r") {
		return invalid("%s must not contain control characters", field)
	}
	return nil
}

// CreateArgs validates the spec and returns the arguments of `nerdctl create` after the
// subcommand. Values are passed as --flag=value, so none can be taken for a flag; the
// command follows the image, where nerdctl stops parsing flags.
func CreateArgs(s CreateSpec) ([]string, error) {
	if s.Image == "" {
		return nil, invalid("image is required")
	}
	if _, err := referenceutil.Parse(s.Image); err != nil || strings.HasPrefix(s.Image, "-") {
		return nil, invalid("invalid image reference %q", s.Image)
	}
	var a []string
	if s.Name != "" {
		if !containerNameRe.MatchString(s.Name) || len(s.Name) > 128 {
			return nil, invalid("name may contain letters, digits, '_', '.' and '-' and must start with a letter or digit")
		}
		a = append(a, "--name="+s.Name)
	}
	if s.Entrypoint != "" {
		if err := noControl("entrypoint", s.Entrypoint); err != nil {
			return nil, err
		}
		a = append(a, "--entrypoint="+s.Entrypoint)
	}
	for _, p := range s.Ports {
		arg, err := portArg(p)
		if err != nil {
			return nil, err
		}
		a = append(a, "--publish="+arg)
	}
	seen := map[string]bool{}
	for _, e := range s.Env {
		if !envNameRe.MatchString(e.Name) {
			return nil, invalid("invalid environment variable name %q", e.Name)
		}
		if seen[e.Name] {
			return nil, invalid("environment variable %s is set twice", e.Name)
		}
		seen[e.Name] = true
		if strings.ContainsRune(e.Value, 0) {
			return nil, invalid("environment variable %s contains a NUL character", e.Name)
		}
		a = append(a, "--env="+e.Name+"="+e.Value)
	}
	for _, m := range s.Mounts {
		arg, err := mountArg(m)
		if err != nil {
			return nil, err
		}
		a = append(a, "--mount="+arg)
	}
	if s.Network != "" {
		if !networkRe.MatchString(s.Network) {
			return nil, invalid("invalid network %q", s.Network)
		}
		a = append(a, "--network="+s.Network)
	}
	advanced, err := advancedArgs(s)
	if err != nil {
		return nil, err
	}
	a = append(a, advanced...)
	if s.Hostname != "" {
		if !hostnameRe.MatchString(s.Hostname) {
			return nil, invalid("invalid hostname %q", s.Hostname)
		}
		a = append(a, "--hostname="+s.Hostname)
	}
	if s.User != "" {
		if !userRe.MatchString(s.User) {
			return nil, invalid("user must be a name or UID, optionally with :group")
		}
		a = append(a, "--user="+s.User)
	}
	if s.Workdir != "" {
		if !strings.HasPrefix(s.Workdir, "/") {
			return nil, invalid("working directory must be an absolute path")
		}
		if err := noControl("working directory", s.Workdir); err != nil {
			return nil, err
		}
		a = append(a, "--workdir="+s.Workdir)
	}
	if s.CPUs < 0 || s.MemoryBytes < 0 {
		return nil, invalid("limits must not be negative")
	}
	if s.CPUs > 0 {
		a = append(a, "--cpus="+strconv.FormatFloat(s.CPUs, 'f', -1, 64))
	}
	if s.MemoryBytes > 0 {
		if s.MemoryBytes < 6<<20 {
			return nil, invalid("memory limit must be at least 6 MiB")
		}
		a = append(a, "--memory="+strconv.FormatInt(s.MemoryBytes, 10))
	}
	if s.Restart != "" {
		if !restartRe.MatchString(s.Restart) {
			return nil, invalid("restart must be no, always, unless-stopped or on-failure[:N]")
		}
		a = append(a, "--restart="+s.Restart)
	}
	keys := make([]string, 0, len(s.Labels))
	for k := range s.Labels {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if k == "" || strings.ContainsAny(k, "=\x00\n") || strings.HasPrefix(k, "nerdctl/") {
			return nil, invalid("invalid label %q (nerdctl/ labels are reserved)", k)
		}
		if err := noControl("label "+k, s.Labels[k]); err != nil {
			return nil, err
		}
		a = append(a, "--label="+k+"="+s.Labels[k])
	}
	a = append(a, s.Image)
	for _, c := range s.Command {
		if strings.ContainsRune(c, 0) {
			return nil, invalid("command contains a NUL character")
		}
	}
	return append(a, s.Command...), nil
}

func portArg(p PortSpec) (string, error) {
	if p.ContainerPort < 1 || p.ContainerPort > 65535 || p.HostPort < 0 || p.HostPort > 65535 {
		return "", invalid("ports must be 1-65535")
	}
	proto := p.Protocol
	if proto == "" {
		proto = "tcp"
	}
	if proto != "tcp" && proto != "udp" {
		return "", invalid("protocol must be tcp or udp")
	}
	host := ""
	if p.HostPort > 0 {
		host = strconv.Itoa(p.HostPort)
	}
	if p.HostIP != "" {
		ip := p.HostIP
		if !regexp.MustCompile(`^[0-9a-fA-F.:]+$`).MatchString(ip) {
			return "", invalid("invalid host IP %q", ip)
		}
		if strings.Contains(ip, ":") {
			ip = "[" + ip + "]"
		}
		return fmt.Sprintf("%s:%s:%d/%s", ip, host, p.ContainerPort, proto), nil
	}
	if host == "" {
		return fmt.Sprintf("%d/%s", p.ContainerPort, proto), nil
	}
	return fmt.Sprintf("%s:%d/%s", host, p.ContainerPort, proto), nil
}

func mountArg(m MountSpec) (string, error) {
	if !strings.HasPrefix(m.Target, "/") || strings.ContainsAny(m.Target, ",=\x00\n") {
		return "", invalid("mount target must be an absolute path without ',' or '='")
	}
	parts := []string{"type=" + m.Type}
	switch m.Type {
	case "volume":
		if m.Source != "" {
			if !volumeNameRe.MatchString(m.Source) {
				return "", invalid("invalid volume name %q", m.Source)
			}
			parts = append(parts, "source="+m.Source)
		}
	case "bind":
		if !strings.HasPrefix(m.Source, "/") || strings.ContainsAny(m.Source, ",=\x00\n") {
			return "", invalid("bind mount source must be an absolute host path without ',' or '='")
		}
		parts = append(parts, "source="+m.Source)
	case "tmpfs":
		if m.Source != "" {
			return "", invalid("tmpfs mounts have no source")
		}
	default:
		return "", invalid("mount type must be volume, bind or tmpfs")
	}
	parts = append(parts, "target="+m.Target)
	if m.ReadOnly {
		parts = append(parts, "readonly")
	}
	return strings.Join(parts, ","), nil
}

// RunCommand is the `nerdctl run -d` equivalent of a spec, quoted for a POSIX shell, shown
// before creating (#17).
func RunCommand(s CreateSpec) (string, error) {
	args, err := CreateArgs(s)
	if err != nil {
		return "", err
	}
	out := []string{"nerdctl", "run", "-d"}
	for _, a := range args {
		out = append(out, shellQuote(a))
	}
	return strings.Join(out, " "), nil
}

func shellQuote(s string) string {
	if s != "" && regexp.MustCompile(`^[A-Za-z0-9_./:=@%+,-]+$`).MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Create pulls the image when it is missing, creates the container with the host's
// `nerdctl create` (ADR-0003) and starts it when start is set. Progress goes to out. It returns
// the new container's ID.
func (cc clientContainers) Create(ctx context.Context, namespace string, spec CreateSpec, start bool, out io.Writer) (string, error) {
	args, err := CreateArgs(spec)
	if err != nil {
		return "", err
	}
	cl, err := cc.c.conn()
	if err != nil {
		return "", err
	}
	ref, err := referenceutil.Parse(spec.Image)
	if err != nil {
		return "", invalid("invalid image reference %q", spec.Image)
	}
	if _, err := cl.ImageService().Get(namespaces.WithNamespace(ctx, namespace), ref.String()); errdefs.IsNotFound(err) {
		if err := cc.c.Images().Pull(ctx, namespace, spec.Image, PullOptions{}, out); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", fmt.Errorf("look up image: %w", classify(err))
	}

	bin, global := cc.c.nerdctlInvocation(cc.c.nerdctlFor(namespace))
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("%w: nerdctl binary %q not found", errdefs.ErrFailedPrecondition, bin)
	}
	argv := append(append(global, "create", "--pull=never"), args...)
	_, _ = fmt.Fprintf(out, "\r\n\x1b[1mCreating container\x1b[0m\r\n")
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, argv...) //nolint:gosec // G204: typed arguments, no shell (ADR-0003)
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(&stderr, out)
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if strings.Contains(msg, "already exists") || strings.Contains(msg, "is already used") {
			return "", fmt.Errorf("%w: %s", errdefs.ErrConflict, msg)
		}
		return "", fmt.Errorf("nerdctl create: %s", msg)
	}
	id := strings.TrimSpace(stdout.String())
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(id) {
		return "", fmt.Errorf("nerdctl create printed no container ID: %q", id)
	}
	_, _ = fmt.Fprintf(out, "Created %s\r\n", id[:12])
	if start {
		if err := cc.Do(ctx, namespace, id, ActionStart, ActionOptions{}); err != nil {
			return id, fmt.Errorf("created %s but could not start it: %w", id[:12], err)
		}
		_, _ = fmt.Fprintf(out, "Started %s\r\n", id[:12])
	}
	return id, nil
}

// advancedArgs validates the advanced settings (#18).
func advancedArgs(s CreateSpec) ([]string, error) {
	var a []string
	if len(s.ExtraNetworks) > 0 {
		if s.Network == "" || s.Network == "host" || s.Network == "none" {
			return nil, invalid("extra networks need a first network other than host or none")
		}
		for _, n := range s.ExtraNetworks {
			if !networkRe.MatchString(n) || n == "host" || n == "none" || n == s.Network {
				return nil, invalid("invalid extra network %q", n)
			}
			a = append(a, "--network="+n)
		}
	}
	if s.IP != "" {
		ip, err := netip.ParseAddr(s.IP)
		if err != nil || !ip.Is4() {
			return nil, invalid("ip must be an IPv4 address")
		}
		if s.Network == "" || s.Network == "host" || s.Network == "none" {
			return nil, invalid("a static IP needs a network other than host or none")
		}
		a = append(a, "--ip="+ip.String())
	}
	if s.Privileged {
		a = append(a, "--privileged")
	}
	for _, c := range s.CapAdd {
		if !capabilityRe.MatchString(c) {
			return nil, invalid("invalid capability %q", c)
		}
		a = append(a, "--cap-add="+strings.ToUpper(c))
	}
	for _, c := range s.CapDrop {
		if !capabilityRe.MatchString(c) {
			return nil, invalid("invalid capability %q", c)
		}
		a = append(a, "--cap-drop="+strings.ToUpper(c))
	}
	for _, d := range s.Devices {
		parts := strings.Split(d, ":")
		ok := len(parts) <= 3 && devicePathRe.MatchString(parts[0])
		if ok && len(parts) >= 2 {
			// host:perms or host:container[:perms]
			ok = devicePathRe.MatchString(parts[1]) || (len(parts) == 2 && devicePermsRe.MatchString(parts[1]))
		}
		if ok && len(parts) == 3 {
			ok = devicePermsRe.MatchString(parts[2])
		}
		if !ok {
			return nil, invalid("device must be /host/path[:/container/path][:rwm], got %q", d)
		}
		a = append(a, "--device="+d)
	}
	for _, d := range s.DNS {
		ip, err := netip.ParseAddr(d)
		if err != nil {
			return nil, invalid("DNS server must be an IP address, got %q", d)
		}
		a = append(a, "--dns="+ip.String())
	}
	for _, h := range s.ExtraHosts {
		name, addr, ok := strings.Cut(h, ":")
		if ok && addr != "host-gateway" {
			_, err := netip.ParseAddr(addr)
			ok = err == nil
		}
		if !ok || !hostnameRe.MatchString(name) {
			return nil, invalid("extra host must be name:address, got %q", h)
		}
		a = append(a, "--add-host="+name+":"+addr)
	}
	return a, nil
}
