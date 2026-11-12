// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-FileCopyrightText: The containerd Authors
// SPDX-License-Identifier: AGPL-3.0-only AND Apache-2.0

// sampleTask adapts collect and setContainerStatsAndRenderStatsEntry of nerdctl
// pkg/cmd/container/stats.go and stats_linux.go (Apache-2.0): the same numbers as
// `nerdctl stats`, one container at a time, without the CLI's rendering loop.

package runtime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	v1 "github.com/containerd/cgroups/v3/cgroup1/stats"
	v2 "github.com/containerd/cgroups/v3/cgroup2/stats"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/nerdctl/v2/pkg/containerinspector"
	"github.com/containerd/nerdctl/v2/pkg/statsutil"
	"github.com/containerd/typeurl/v2"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func sampleTask(ctx context.Context, task containerd.Task, prev *statsutil.ContainerStats, first bool) (statsutil.StatsEntry, error) {
	var entry statsutil.StatsEntry
	systemUsage, onlineCPUs, err := systemCPUUsage()
	if err != nil {
		return entry, err
	}
	metric, err := task.Metrics(ctx)
	if err != nil {
		return entry, err
	}
	data, err := typeurl.UnmarshalAny(metric.Data)
	if err != nil {
		return entry, err
	}
	var links []netlink.Link
	if !first {
		links, err = netLinks(ctx, int(task.Pid()))
		if err != nil {
			return entry, err
		}
	}
	switch m := data.(type) {
	case *v1.Metrics:
		if !first {
			entry, err = statsutil.SetCgroupStatsFields(prev, m, links,
				statsutil.SystemInfo{OnlineCPUs: onlineCPUs, SystemUsage: systemUsage})
		}
		prev.CgroupCPU = m.CPU.Usage.Total
		prev.CgroupSystem = systemUsage
	case *v2.Metrics:
		if !first {
			entry, err = statsutil.SetCgroup2StatsFields(prev, m, links)
		}
		prev.Cgroup2CPU = m.CPU.UsageUsec * 1000
		prev.Cgroup2System = m.CPU.SystemUsec * 1000
	default:
		return entry, errors.New("unknown cgroup metrics")
	}
	prev.Time = time.Now()
	return entry, err
}

// netLinks returns the up, non-loopback interfaces in the task's network namespace.
func netLinks(ctx context.Context, pid int) ([]netlink.Link, error) {
	nsInfo, err := containerinspector.InspectNetNS(ctx, pid)
	if err != nil {
		return nil, err
	}
	ns, err := netns.GetFromPid(pid)
	if err != nil {
		return nil, fmt.Errorf("network namespace of pid %d: %w", pid, err)
	}
	defer ns.Close()
	h, err := netlink.NewHandleAt(ns)
	if err != nil {
		return nil, fmt.Errorf("netlink in namespace of pid %d: %w", pid, err)
	}
	defer h.Close()
	var links []netlink.Link
	for _, itf := range nsInfo.Interfaces {
		l, err := h.LinkByIndex(itf.Index)
		if err != nil {
			return nil, fmt.Errorf("interface %s: %w", itf.Name, err)
		}
		a := l.Attrs()
		if a.Flags&net.FlagUp == 0 || a.Flags&net.FlagLoopback != 0 || strings.HasPrefix(a.Name, "lo") {
			continue
		}
		links = append(links, l)
	}
	return links, nil
}

// systemCPUUsage reads the total CPU time in nanoseconds and the number of CPUs from
// /proc/stat.
func systemCPUUsage() (uint64, uint32, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	return parseProcStat(f)
}

func parseProcStat(r io.Reader) (uint64, uint32, error) {
	var total uint64
	var cpus uint32
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "cpu") {
			break
		}
		if fields[0] != "cpu" {
			cpus++
			continue
		}
		if len(fields) < 8 {
			return 0, 0, errors.New("invalid /proc/stat cpu line")
		}
		for _, v := range fields[1:8] {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("parse /proc/stat: %w", err)
			}
			total += n
		}
	}
	if err := sc.Err(); err != nil {
		return 0, 0, err
	}
	return total * uint64(time.Second) / clockTicks, cpus, nil
}
