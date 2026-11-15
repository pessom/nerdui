// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/containerinspector"
	"github.com/containerd/nerdctl/v2/pkg/identifiers"
	"github.com/containerd/nerdctl/v2/pkg/labels"
	"github.com/containerd/nerdctl/v2/pkg/netutil"
)

// Networks are nerdctl's CNI networks (#60). Listing and removal use nerdctl's netutil; creation
// runs the host's `nerdctl network create`, whose options differ between nerdctl releases
// (ADR-0024).
type Networks interface {
	// List returns the networks of a namespace sorted by name, with their containers.
	List(ctx context.Context, namespace string) ([]Network, error)
	// Create creates a network with the host nerdctl and returns it.
	Create(ctx context.Context, namespace string, spec NetworkSpec) (Network, error)
	// Remove removes a network like `nerdctl network rm`: refused (409) while a running
	// container uses it, for the default and pre-defined networks and for networks managed
	// outside nerdctl.
	Remove(ctx context.Context, namespace, name string) error
}

// Network is a CNI network. Keep in sync with api/openapi.yaml.
type Network struct {
	Name   string            `json:"name"`
	ID     string            `json:"id,omitempty"`
	Driver string            `json:"driver"`
	Labels map[string]string `json:"labels"`
	IPAM   []NetworkSubnet   `json:"ipam"`
	// Parent is the host interface of a macvlan or ipvlan network.
	Parent string `json:"parent,omitempty"`
	// Internal bridge networks have no gateway to the outside.
	Internal bool `json:"internal"`
	// Default is nerdctl's default network ("bridge").
	Default bool `json:"default"`
	// Removable is false for the default network, pre-defined networks and networks not created
	// by nerdctl.
	Removable  bool               `json:"removable"`
	File       string             `json:"file,omitempty"`
	Containers []NetworkContainer `json:"containers"`
}

// NetworkSubnet is one IPAM range of a network.
type NetworkSubnet struct {
	Subnet     string `json:"subnet,omitempty"`
	Gateway    string `json:"gateway,omitempty"`
	RangeStart string `json:"rangeStart,omitempty"`
	RangeEnd   string `json:"rangeEnd,omitempty"`
}

// NetworkContainer is a container attached to a network; addresses are known while it runs.
type NetworkContainer struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Running bool     `json:"running"`
	IPs     []string `json:"ips,omitempty"`
	MAC     string   `json:"mac,omitempty"`
}

// NetworkSpec is a network to create.
type NetworkSpec struct {
	Name   string `json:"name"`
	Driver string `json:"driver"` // bridge (default), macvlan, ipvlan
	Subnet string `json:"subnet,omitempty"`
	// Gateway and IPRange require Subnet.
	Gateway string `json:"gateway,omitempty"`
	IPRange string `json:"ipRange,omitempty"`
	// Parent is the host interface of a macvlan or ipvlan network (required for them).
	Parent   string            `json:"parent,omitempty"`
	Internal bool              `json:"internal,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

var interfaceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@:-]{0,14}$`)

// NetworkCreateArgs validates spec and returns the arguments of `nerdctl network create`.
// Values use the --flag=value form and the name follows "--", so nothing is read as a flag.
func NetworkCreateArgs(spec NetworkSpec) ([]string, error) {
	if err := identifiers.ValidateDockerCompat(spec.Name); err != nil {
		return nil, fmt.Errorf("%w: network name: %w", errdefs.ErrInvalidArgument, err)
	}
	if spec.Name == "host" || spec.Name == "none" || spec.Name == netutil.DefaultNetworkName {
		return nil, fmt.Errorf("%w: %q is a pre-defined network name", errdefs.ErrInvalidArgument, spec.Name)
	}
	driver := spec.Driver
	if driver == "" {
		driver = "bridge"
	}
	args := []string{"network", "create", "--driver=" + driver}
	switch driver {
	case "bridge":
		if spec.Parent != "" {
			return nil, fmt.Errorf("%w: parent applies to macvlan and ipvlan networks", errdefs.ErrInvalidArgument)
		}
	case "macvlan", "ipvlan":
		if !interfaceName.MatchString(spec.Parent) {
			return nil, fmt.Errorf("%w: %s needs the parent host interface, e.g. eth0", errdefs.ErrInvalidArgument, driver)
		}
		if spec.Internal {
			return nil, fmt.Errorf("%w: internal applies to bridge networks", errdefs.ErrInvalidArgument)
		}
		args = append(args, "--opt=parent="+spec.Parent)
	default:
		return nil, fmt.Errorf("%w: driver must be bridge, macvlan or ipvlan", errdefs.ErrInvalidArgument)
	}
	var subnet *net.IPNet
	if spec.Subnet != "" {
		_, n, err := net.ParseCIDR(spec.Subnet)
		if err != nil {
			return nil, fmt.Errorf("%w: subnet must be CIDR, e.g. 10.20.0.0/24", errdefs.ErrInvalidArgument)
		}
		subnet = n
		args = append(args, "--subnet="+n.String())
	}
	if spec.Gateway != "" {
		ip := net.ParseIP(spec.Gateway)
		if subnet == nil || ip == nil || !subnet.Contains(ip) {
			return nil, fmt.Errorf("%w: gateway must be an address inside the subnet", errdefs.ErrInvalidArgument)
		}
		args = append(args, "--gateway="+ip.String())
	}
	if spec.IPRange != "" {
		ip, r, err := net.ParseCIDR(spec.IPRange)
		if subnet == nil || err != nil || !subnet.Contains(ip) {
			return nil, fmt.Errorf("%w: IP range must be CIDR inside the subnet, e.g. 10.20.0.128/25", errdefs.ErrInvalidArgument)
		}
		args = append(args, "--ip-range="+r.String())
	}
	if spec.Internal {
		args = append(args, "--internal")
	}
	keys := make([]string, 0, len(spec.Labels))
	for k := range spec.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := spec.Labels[k]
		if k == "" || strings.ContainsAny(k, "=\n") || strings.Contains(v, "\n") {
			return nil, fmt.Errorf("%w: invalid label %q", errdefs.ErrInvalidArgument, k)
		}
		if strings.HasPrefix(k, labels.Prefix) {
			return nil, fmt.Errorf("%w: label %q uses nerdctl's reserved prefix", errdefs.ErrInvalidArgument, k)
		}
		args = append(args, "--label="+k+"="+v)
	}
	return append(args, "--", spec.Name), nil
}

// Networks returns network operations of the local containerd.
func (c *Client) Networks() Networks { return clientNetworks{c} }

type clientNetworks struct{ c *Client }

func (cn clientNetworks) cniEnv(namespace string) (*netutil.CNIEnv, error) {
	cfg := cn.c.nerdctlFor(namespace)
	env, err := netutil.NewCNIEnv(cfg.CNIPath, cfg.CNINetConfPath, netutil.WithNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("CNI configuration: %w", err)
	}
	return env, nil
}

func (cn clientNetworks) List(ctx context.Context, namespace string) ([]Network, error) {
	cl, err := cn.c.conn()
	if err != nil {
		return nil, err
	}
	env, err := cn.cniEnv(namespace)
	if err != nil {
		return nil, err
	}
	list, err := env.NetworkList()
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	attached, err := networkContainers(namespaces.WithNamespace(ctx, namespace), cl)
	if err != nil {
		return nil, err
	}
	out := make([]Network, 0, len(list))
	for _, n := range list {
		item := describeNetwork(n)
		item.Containers = attached[n.Name]
		if item.Containers == nil {
			item.Containers = []NetworkContainer{}
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// cniPluginConf is the part of a CNI plugin configuration shown in the UI.
type cniPluginConf struct {
	Type   string `json:"type"`
	Master string `json:"master"`
	IsGW   *bool  `json:"isGateway"`
	IPAM   struct {
		Ranges [][]NetworkSubnet `json:"ranges"`
	} `json:"ipam"`
}

func describeNetwork(n *netutil.NetworkConfig) Network {
	out := Network{Name: n.Name, Labels: map[string]string{}, IPAM: []NetworkSubnet{}, File: n.File}
	if n.NerdctlID != nil {
		out.ID = *n.NerdctlID
	}
	if n.NerdctlLabels != nil {
		for k, v := range *n.NerdctlLabels {
			out.Labels[k] = v
		}
	}
	if len(n.Plugins) > 0 {
		var conf cniPluginConf
		if err := json.Unmarshal(n.Plugins[0].Bytes, &conf); err == nil {
			out.Driver = conf.Type
			out.Parent = conf.Master
			out.Internal = conf.Type == "bridge" && conf.IsGW != nil && !*conf.IsGW
			for _, set := range conf.IPAM.Ranges {
				out.IPAM = append(out.IPAM, set...)
			}
		}
	}
	_, isDefault := out.Labels[labels.NerdctlDefaultNetwork]
	out.Default = isDefault || n.Name == netutil.DefaultNetworkName
	delete(out.Labels, labels.NerdctlDefaultNetwork)
	out.Removable = !out.Default && n.File != "" && n.NerdctlID != nil
	return out
}

// networkContainers returns, per network name, the containers of the namespace (in ctx)
// attached to it, with addresses for running ones. CNI names the interface of the i-th network
// of a container "eth<i>", in the order of the "nerdctl/networks" label.
func networkContainers(ctx context.Context, cl *containerd.Client) (map[string][]NetworkContainer, error) {
	containers, err := cl.Containers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", classify(err))
	}
	out := map[string][]NetworkContainer{}
	for _, c := range containers {
		lbls, err := c.Labels(ctx)
		if err != nil {
			continue // removed while listing
		}
		var nets []string
		if raw := lbls[labels.Networks]; raw == "" || json.Unmarshal([]byte(raw), &nets) != nil {
			continue
		}
		running := false
		var ifaces map[string]NetworkContainer
		if task, err := c.Task(ctx, nil); err == nil {
			if st, err := task.Status(ctx); err == nil && (st.Status == containerd.Running || st.Status == containerd.Paused) {
				running = true
				ifaces = interfaceAddrs(ctx, c)
			}
		}
		for i, name := range nets {
			nc := NetworkContainer{ID: c.ID(), Name: lbls[labels.Name], Running: running}
			if a, ok := ifaces["eth"+strconv.Itoa(i)]; ok {
				nc.IPs, nc.MAC = a.IPs, a.MAC
			}
			out[name] = append(out[name], nc)
		}
	}
	for _, list := range out {
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	}
	return out, nil
}

// interfaceAddrs reads the addresses of a running container's interfaces from its network
// namespace (as `nerdctl inspect` does); nil when they cannot be read.
func interfaceAddrs(ctx context.Context, c containerd.Container) map[string]NetworkContainer {
	n, err := containerinspector.Inspect(ctx, c)
	if err != nil || n.Process == nil || n.Process.NetNS == nil {
		return nil
	}
	out := map[string]NetworkContainer{}
	for _, x := range n.Process.NetNS.Interfaces {
		var ips []string
		for _, a := range x.Addrs {
			ip, _, err := net.ParseCIDR(a)
			if err != nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			ips = append(ips, ip.String())
		}
		out[x.Name] = NetworkContainer{IPs: ips, MAC: x.HardwareAddr}
	}
	return out
}

func (cn clientNetworks) Create(ctx context.Context, namespace string, spec NetworkSpec) (Network, error) {
	args, err := NetworkCreateArgs(spec)
	if err != nil {
		return Network{}, err
	}
	cfg := cn.c.nerdctlFor(namespace)
	bin, global := cn.c.nerdctlInvocation(cfg)
	if _, err := exec.LookPath(bin); err != nil {
		return Network{}, fmt.Errorf("%w: nerdctl binary %q not found", errdefs.ErrFailedPrecondition, bin)
	}
	global = append(global, "--cni-path="+cfg.CNIPath, "--cni-netconfpath="+cfg.CNINetConfPath)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, append(global, args...)...) //nolint:gosec // G204: validated arguments, no shell (ADR-0024)
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		switch {
		case strings.Contains(msg, "already exists"):
			return Network{}, fmt.Errorf("%w: network %q already exists", errdefs.ErrAlreadyExists, spec.Name)
		case strings.Contains(msg, "conflict") || strings.Contains(msg, "overlap"):
			return Network{}, fmt.Errorf("%w: %s", errdefs.ErrConflict, msg)
		}
		return Network{}, fmt.Errorf("%w: nerdctl network create: %s", errdefs.ErrInvalidArgument, msg)
	}
	env, err := cn.cniEnv(namespace)
	if err != nil {
		return Network{}, err
	}
	nets, err := env.NetworkMap()
	if err != nil {
		return Network{}, fmt.Errorf("list networks: %w", err)
	}
	n, ok := nets[spec.Name]
	if !ok {
		return Network{}, fmt.Errorf("network %q was not found after creating it", spec.Name)
	}
	out := describeNetwork(n)
	out.Containers = []NetworkContainer{}
	return out, nil
}

func (cn clientNetworks) Remove(ctx context.Context, namespace, name string) error {
	if err := identifiers.ValidateDockerCompat(name); err != nil {
		return fmt.Errorf("%w: network name: %w", errdefs.ErrInvalidArgument, err)
	}
	cl, err := cn.c.conn()
	if err != nil {
		return err
	}
	env, err := cn.cniEnv(namespace)
	if err != nil {
		return err
	}
	nets, err := env.NetworkMap()
	if err != nil {
		return fmt.Errorf("list networks: %w", err)
	}
	n, ok := nets[name]
	if !ok {
		return fmt.Errorf("%w: network %q not found", errdefs.ErrNotFound, name)
	}
	if d := describeNetwork(n); !d.Removable {
		return fmt.Errorf("%w: %q is a default or pre-defined network, or managed outside nerdctl", errdefs.ErrFailedPrecondition, name)
	}
	// Running containers of every namespace, like `nerdctl network rm`.
	used, err := netutil.UsedNetworks(ctx, cl)
	if err != nil {
		return fmt.Errorf("check network usage: %w", classify(err))
	}
	if ids := used[name]; len(ids) > 0 {
		return fmt.Errorf("%w: network %q is used by %d running container(s); stop or remove them first",
			errdefs.ErrFailedPrecondition, name, len(ids))
	}
	if err := env.RemoveNetwork(n); err != nil {
		return fmt.Errorf("remove network %s: %w", name, err)
	}
	return nil
}
