// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// procRoot is /proc; tests use a fake tree.
var procRoot = "/proc"

// readProcess reads one process from root/<pid>: stat, status and cmdline. users caches user
// names by UID.
func readProcess(root string, pid int, users map[int]string) (Process, error) {
	dir := filepath.Join(root, strconv.Itoa(pid))
	stat, err := os.ReadFile(filepath.Join(dir, "stat")) //nolint:gosec // G304: /proc files of numeric PIDs
	if err != nil {
		return Process{}, err
	}
	s := string(stat)
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return Process{}, fmt.Errorf("parse %s/stat", dir)
	}
	comm := s[open+1 : end]
	// fields[0] is field 3 (state).
	fields := strings.Fields(s[end+1:])
	if len(fields) < 22 {
		return Process{}, fmt.Errorf("parse %s/stat: %d fields", dir, len(fields))
	}
	num := func(i int) int64 { n, _ := strconv.ParseInt(fields[i], 10, 64); return n }
	ppid, _ := strconv.Atoi(fields[1])
	p := Process{PID: pid, PPID: ppid, State: fields[0], UID: -1}
	// utime, stime (fields 14, 15), starttime (22), rss in pages (24).
	cpuTicks, startTicks := num(11)+num(12), num(19)
	p.RSS = num(21) * int64(os.Getpagesize())
	if boot, ok := bootTimeAt(root); ok {
		p.Started = boot.Add(time.Duration(startTicks) * (time.Second / clockTicks)).UTC()
		if elapsed := time.Since(p.Started).Seconds(); elapsed > 0 {
			p.CPU = math.Round(float64(cpuTicks)/clockTicks/elapsed*1000) / 10
		}
	}
	if status, err := os.ReadFile(filepath.Join(dir, "status")); err == nil { //nolint:gosec // G304: /proc files of numeric PIDs
		for line := range strings.SplitSeq(string(status), "\n") {
			if v, ok := strings.CutPrefix(line, "Uid:"); ok {
				if f := strings.Fields(v); len(f) > 0 {
					p.UID, _ = strconv.Atoi(f[0])
				}
				break
			}
		}
	}
	if p.UID >= 0 {
		name, ok := users[p.UID]
		if !ok {
			if u, err := user.LookupId(strconv.Itoa(p.UID)); err == nil {
				name = u.Username
			}
			users[p.UID] = name
		}
		p.User = name
	}
	p.Command = "[" + comm + "]"                                                            // kernel threads and zombies have no command line
	if cmd, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil && len(cmd) > 0 { //nolint:gosec // G304: /proc files of numeric PIDs
		p.Command = strings.TrimSpace(string(bytes.ReplaceAll(bytes.TrimRight(cmd, "\x00"), []byte{0}, []byte{' '})))
	}
	return p, nil
}

// bootTimeAt reads the boot time from root/stat.
func bootTimeAt(root string) (time.Time, bool) {
	if root == "/proc" {
		return bootTime()
	}
	data, err := os.ReadFile(filepath.Join(root, "stat")) //nolint:gosec // G304: /proc files of numeric PIDs
	if err != nil {
		return time.Time{}, false
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			sec, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return time.Unix(sec, 0), err == nil
		}
	}
	return time.Time{}, false
}
