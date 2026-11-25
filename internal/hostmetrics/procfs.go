// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package hostmetrics

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// cpuTimes are the jiffies of all CPUs from the first line of /proc/stat.
type cpuTimes struct{ busy, total uint64 }

func readCPU(proc string) (cpuTimes, error) {
	b, err := os.ReadFile(filepath.Join(proc, "stat")) //nolint:gosec // G304: proc is the configured procfs mount
	if err != nil {
		return cpuTimes{}, err
	}
	line, _, _ := bytes.Cut(b, []byte("\n"))
	f := strings.Fields(string(line))
	if len(f) < 5 || f[0] != "cpu" {
		return cpuTimes{}, fmt.Errorf("unexpected /proc/stat: %q", line)
	}
	var t cpuTimes
	for i, s := range f[1:] {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return cpuTimes{}, fmt.Errorf("/proc/stat: %w", err)
		}
		// user nice system idle iowait irq softirq steal guest guest_nice; guest time is
		// counted in user and nice already.
		if i >= 8 {
			break
		}
		t.total += v
		if i != 3 && i != 4 { // idle, iowait
			t.busy += v
		}
	}
	return t, nil
}

// cpuPercent is the busy share between two readings, 0 to 100.
func cpuPercent(prev, next cpuTimes) float64 {
	if next.total <= prev.total || next.busy < prev.busy {
		return 0
	}
	p := float64(next.busy-prev.busy) / float64(next.total-prev.total) * 100
	return min(max(p, 0), 100)
}

// memory is /proc/meminfo in bytes.
type memory struct{ total, available, swapTotal, swapFree uint64 }

func readMemory(proc string) (memory, error) {
	f, err := os.Open(filepath.Join(proc, "meminfo")) //nolint:gosec // G304: proc is the configured procfs mount
	if err != nil {
		return memory{}, err
	}
	defer f.Close() //nolint:errcheck // read only
	var m memory
	found := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		var dst *uint64
		switch name {
		case "MemTotal":
			dst = &m.total
		case "MemAvailable":
			dst = &m.available
		case "SwapTotal":
			dst = &m.swapTotal
		case "SwapFree":
			dst = &m.swapFree
		default:
			continue
		}
		v, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
		if err != nil {
			return memory{}, fmt.Errorf("/proc/meminfo %s: %w", name, err)
		}
		*dst = v * 1024
		found++
	}
	if err := sc.Err(); err != nil {
		return memory{}, err
	}
	if m.total == 0 || found < 2 {
		return memory{}, fmt.Errorf("unexpected /proc/meminfo")
	}
	return m, nil
}

func readLoad(proc string) (float64, error) {
	b, err := os.ReadFile(filepath.Join(proc, "loadavg")) //nolint:gosec // G304: proc is the configured procfs mount
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, fmt.Errorf("unexpected /proc/loadavg")
	}
	return strconv.ParseFloat(f[0], 64)
}

// virtualInterfaces are skipped when counting network traffic: loopback, and the bridges and
// veth pairs of containers, whose traffic also crosses a physical interface or stays local.
var virtualInterfaces = []string{"lo", "veth", "cni", "nerdctl", "br-", "docker", "flannel", "cali", "vxlan", "tun", "tap", "virbr", "podman"}

// readNetwork sums the received and sent bytes of the host's physical interfaces.
func readNetwork(proc string) (rx, tx uint64, err error) {
	f, err := os.Open(filepath.Join(proc, "net", "dev")) //nolint:gosec // G304: proc is the configured procfs mount
	if err != nil {
		return 0, 0, err
	}
	defer f.Close() //nolint:errcheck // read only
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue // headers
		}
		name = strings.TrimSpace(name)
		if skipInterface(name) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		r, err1 := strconv.ParseUint(fields[0], 10, 64)
		t, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		rx += r
		tx += t
	}
	return rx, tx, sc.Err()
}

func skipInterface(name string) bool {
	for _, p := range virtualInterfaces {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
