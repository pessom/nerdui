// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/nerdctl/v2/pkg/buildkitutil"
	"github.com/containerd/nerdctl/v2/pkg/infoutil"
	"github.com/containerd/nerdctl/v2/pkg/logging"
	"github.com/containerd/nerdctl/v2/pkg/rootlessutil"
	"github.com/containerd/plugin"
)

// Info describes the host and the container runtime of an environment (#66, ADR-0015).
// Host fields come from the machine nerdui runs on, which is the containerd host.
type Info struct {
	// ID is the containerd server UUID.
	ID              string `json:"id"`
	Hostname        string `json:"hostname"`
	OperatingSystem string `json:"operatingSystem"`
	OSType          string `json:"osType"`
	Architecture    string `json:"architecture"`
	KernelVersion   string `json:"kernelVersion"`
	CPUs            int    `json:"cpus"`
	MemoryBytes     int64  `json:"memoryBytes"`
	CgroupDriver    string `json:"cgroupDriver"`
	CgroupVersion   string `json:"cgroupVersion"`
	Rootless        bool   `json:"rootless"`
	// Snapshotter is nerdctl's default snapshotter; Snapshotters are those containerd loaded.
	Snapshotter     string   `json:"snapshotter"`
	Snapshotters    []string `json:"snapshotters"`
	LogDrivers      []string `json:"logDrivers"`
	SecurityOptions []string `json:"securityOptions"`
	// Features are the cgroup and kernel capabilities reported by `nerdctl info`.
	Features   Features    `json:"features"`
	Components []Component `json:"components"`
	CNI        CNIInfo     `json:"cni"`
	// Warnings are `nerdctl info` warnings (missing limits, IP forwarding…).
	Warnings     []string      `json:"warnings"`
	Deprecations []Deprecation `json:"deprecations"`
	Plugins      []Plugin      `json:"plugins"`
}

// Features are the resource limits and kernel settings available to containers.
type Features struct {
	MemoryLimit    bool `json:"memoryLimit"`
	SwapLimit      bool `json:"swapLimit"`
	CPUCfsPeriod   bool `json:"cpuCfsPeriod"`
	CPUCfsQuota    bool `json:"cpuCfsQuota"`
	CPUShares      bool `json:"cpuShares"`
	CPUSet         bool `json:"cpuSet"`
	PidsLimit      bool `json:"pidsLimit"`
	OomKillDisable bool `json:"oomKillDisable"`
	IPv4Forwarding bool `json:"ipv4Forwarding"`
}

// Component is the version of a runtime component (containerd, runc, buildctl).
type Component struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
	// Error explains why the version is unknown (binary missing, unexpected output).
	Error string `json:"error,omitempty"`
}

// CNIInfo describes the CNI plugins nerdctl uses for container networks.
type CNIInfo struct {
	Path    string   `json:"path"`
	NetConf string   `json:"netConfPath"`
	Version string   `json:"version,omitempty"`
	Plugins []string `json:"plugins"`
	Error   string   `json:"error,omitempty"`
}

// Deprecation is a containerd warning about a deprecated feature in use.
type Deprecation struct {
	ID             string    `json:"id"`
	Message        string    `json:"message"`
	LastOccurrence time.Time `json:"lastOccurrence,omitzero"`
}

// Plugin is a containerd plugin as `ctr plugins ls` shows it.
type Plugin struct {
	Type         string   `json:"type"`
	ID           string   `json:"id"`
	Platforms    []string `json:"platforms,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	// Status is ok, skip (not applicable to this host) or error.
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Plugin statuses.
const (
	PluginOK    = "ok"
	PluginSkip  = "skip"
	PluginError = "error"
)

// probeTimeout bounds each `--version` call of a host binary.
const probeTimeout = 5 * time.Second

// Info collects host and runtime information like `nerdctl info` and `nerdctl version`,
// plus the containerd plugin list (`ctr plugins ls`) and CNI plugin versions.
func (c *Client) Info(ctx context.Context) (Info, error) {
	cl, err := c.conn()
	if err != nil {
		return Info{}, err
	}
	cfg := c.nerdctlFor(c.namespace)
	ctx = namespaces.WithNamespace(ctx, c.namespace)

	ci, err := infoutil.Info(ctx, cl, cfg.Snapshotter, cfg.CgroupManager, cfg.SelinuxEnabled)
	if err != nil {
		return Info{}, fmt.Errorf("get runtime info: %w", classify(err))
	}
	intro := cl.IntrospectionService()
	plugins, err := intro.Plugins(ctx)
	if err != nil {
		return Info{}, fmt.Errorf("list containerd plugins: %w", classify(err))
	}
	server, err := intro.Server(ctx)
	if err != nil {
		return Info{}, fmt.Errorf("get containerd server info: %w", classify(err))
	}
	sv, err := cl.Version(ctx)
	if err != nil {
		return Info{}, fmt.Errorf("get containerd version: %w", classify(err))
	}

	info := Info{
		ID:              ci.ID,
		Hostname:        ci.Name,
		OperatingSystem: ci.OperatingSystem,
		OSType:          ci.OSType,
		Architecture:    ci.Architecture,
		KernelVersion:   ci.KernelVersion,
		CPUs:            ci.NCPU,
		MemoryBytes:     ci.MemTotal,
		CgroupDriver:    ci.CgroupDriver,
		CgroupVersion:   ci.CgroupVersion,
		Rootless:        rootlessutil.IsRootless(),
		Snapshotter:     ci.Driver,
		Snapshotters:    nonNil(ci.Plugins.Storage),
		LogDrivers:      nonNil(logging.Drivers()),
		SecurityOptions: nonNil(ci.SecurityOptions),
		Features: Features{
			MemoryLimit: ci.MemoryLimit, SwapLimit: ci.SwapLimit,
			CPUCfsPeriod: ci.CPUCfsPeriod, CPUCfsQuota: ci.CPUCfsQuota,
			CPUShares: ci.CPUShares, CPUSet: ci.CPUSet, PidsLimit: ci.PidsLimit,
			OomKillDisable: ci.OomKillDisable, IPv4Forwarding: ci.IPv4Forwarding,
		},
		Warnings:     nonNil(ci.Warnings),
		Deprecations: []Deprecation{},
		Plugins:      make([]Plugin, 0, len(plugins.Plugins)),
	}
	for _, d := range server.Deprecations {
		dep := Deprecation{ID: d.ID, Message: d.Message}
		if d.LastOccurrence != nil {
			dep.LastOccurrence = d.LastOccurrence.AsTime()
		}
		info.Deprecations = append(info.Deprecations, dep)
	}
	for _, p := range plugins.Plugins {
		out := Plugin{Type: p.Type, ID: p.ID, Capabilities: p.Capabilities, Status: PluginOK}
		for _, pl := range p.Platforms {
			s := pl.OS + "/" + pl.Architecture
			if pl.Variant != "" {
				s += "/" + pl.Variant
			}
			out.Platforms = append(out.Platforms, s)
		}
		if p.InitErr != nil {
			out.Status, out.Error = pluginStatus(p.InitErr.Message)
		}
		info.Plugins = append(info.Plugins, out)
	}

	// Host binaries are probed concurrently; each has its own timeout.
	containerd := Component{Name: "containerd", Version: sv.Version, Commit: sv.Revision}
	var runc, buildctl Component
	var wg sync.WaitGroup
	wg.Go(func() { runc = probeComponent(ctx, "runc", "runc", parseRuncVersion) })
	wg.Go(func() {
		bin, err := buildkitutil.BuildctlBinary()
		if err != nil {
			buildctl = Component{Name: "buildctl", Error: "buildctl not found in PATH"}
			return
		}
		buildctl = probeComponent(ctx, "buildctl", bin, parseBuildctlVersion)
	})
	wg.Go(func() { info.CNI = probeCNI(ctx, cfg.CNIPath, cfg.CNINetConfPath) })
	wg.Wait()
	info.Components = []Component{containerd, runc, buildctl}
	return info, nil
}

// pluginStatus classifies a plugin init error like `ctr plugins ls`.
func pluginStatus(msg string) (status, detail string) {
	if strings.Contains(msg, plugin.ErrSkipPlugin.Error()) {
		return PluginSkip, msg
	}
	return PluginError, msg
}

type versionParser func(out string) (version, commit string, ok bool)

func probeComponent(ctx context.Context, name, bin string, parse versionParser) Component {
	c := Component{Name: name}
	out, err := runProbe(ctx, bin, "--version")
	if err != nil {
		c.Error = err.Error()
		return c
	}
	v, commit, ok := parse(out)
	if !ok {
		c.Error = fmt.Sprintf("unexpected output of %s --version: %q", name, firstLine(out))
		return c
	}
	c.Version, c.Commit = v, commit
	return c
}

func runProbe(ctx context.Context, bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("%s not found", bin)
	}
	out, err := exec.CommandContext(ctx, path, args...).Output() //nolint:gosec // G204: fixed host binaries
	if err != nil {
		return "", fmt.Errorf("run %s %s: %w", filepath.Base(path), strings.Join(args, " "), err)
	}
	return string(out), nil
}

// parseRuncVersion reads "runc version 1.3.0\ncommit: v1.3.0-0-g4ca628d\n…".
func parseRuncVersion(out string) (string, string, bool) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	f := strings.Fields(lines[0])
	if len(f) != 3 || f[0] != "runc" || f[1] != "version" {
		return "", "", false
	}
	var commit string
	for _, l := range lines[1:] {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "commit" {
			commit = strings.TrimSpace(v)
		}
	}
	return f[2], commit, true
}

// parseBuildctlVersion reads "buildctl github.com/moby/buildkit v0.23.2 940c2f1…".
func parseBuildctlVersion(out string) (string, string, bool) {
	f := strings.Fields(strings.TrimSpace(out))
	if len(f) < 3 || len(f) > 4 || f[0] != "buildctl" {
		return "", "", false
	}
	if len(f) == 4 {
		return f[2], f[3], true
	}
	return f[2], "", true
}

// cniAbout matches the first line CNI plugins print without CNI_COMMAND:
// "CNI bridge plugin v1.7.1".
var cniAbout = regexp.MustCompile(`CNI \S+ plugin (v?\d+\.\d+\.\d+\S*)`)

// probeCNI lists the CNI plugin binaries and reads the version of the bridge plugin
// (nerdctl's default network driver) from its about text.
func probeCNI(ctx context.Context, dir, netConf string) CNIInfo {
	ci := CNIInfo{Path: dir, NetConf: netConf, Plugins: []string{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		ci.Error = fmt.Sprintf("read CNI plugin directory: %v", err)
		return ci
	}
	for _, e := range entries {
		if e.Type().IsRegular() || e.Type()&os.ModeSymlink != 0 {
			ci.Plugins = append(ci.Plugins, e.Name())
		}
	}
	slices.Sort(ci.Plugins)
	if !slices.Contains(ci.Plugins, "bridge") {
		ci.Error = "bridge plugin not found"
		return ci
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(pctx, filepath.Join(dir, "bridge")) //nolint:gosec // G204: CNI dir from nerdctl config
	cmd.Env = []string{}                                           // no CNI_COMMAND: the plugin prints its about text
	out, err := cmd.CombinedOutput()
	if m := cniAbout.FindStringSubmatch(string(out)); m != nil {
		ci.Version = m[1]
		return ci
	}
	if err == nil {
		err = errors.New("no version in output")
	}
	ci.Error = fmt.Sprintf("bridge plugin version: %v", err)
	return ci
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
