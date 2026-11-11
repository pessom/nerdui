// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// clockTicks is USER_HZ, 100 on every Linux architecture nerdui supports.
const clockTicks = 100

// processStartTime returns when a host process started, from /proc/<pid>/stat (field 22,
// clock ticks since boot) and the boot time in /proc/stat. containerd does not record when
// a task started, so this provides the uptime shown in the UI. nerdui runs on the host
// (ADR-0003), so container PIDs are visible.
func processStartTime(pid uint32) *time.Time {
	if pid == 0 {
		return nil
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return nil
	}
	// The command field may contain spaces and parentheses: fields start after the last ")".
	s := string(stat)
	end := strings.LastIndexByte(s, ')')
	if end < 0 {
		return nil
	}
	fields := strings.Fields(s[end+1:])
	// fields[0] is field 3 (state); starttime is field 22 → index 19.
	if len(fields) < 20 {
		return nil
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return nil
	}
	boot, ok := bootTime()
	if !ok {
		return nil
	}
	t := boot.Add(time.Duration(ticks) * (time.Second / clockTicks)).UTC()
	return &t
}

func bootTime() (time.Time, bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return time.Time{}, false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "btime "); ok {
			sec, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, false
			}
			return time.Unix(sec, 0), true
		}
	}
	return time.Time{}, false
}
