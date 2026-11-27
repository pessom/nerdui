// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package hostcheck finds host settings known to break containers, so that the UI can explain
// a failure the runtime does not report (ADR-0060).
package hostcheck

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Warning is the code of a host problem; the UI words it and links the documentation.
type Warning string

// AppArmorSignals: AppArmor on Linux 7 with
// kernel.apparmor_restrict_unprivileged_unconfined=1 (Ubuntu 26.04) denies signals between the
// processes of a container. Programs that switch user from Go (gosu, the entrypoint of the
// MySQL, MariaDB, PostgreSQL and many other images) then spin at 100% CPU and never start.
const AppArmorSignals Warning = "apparmor_signals"

// Paths are the procfs and sysfs mounts; empty means /proc and /sys.
type Paths struct {
	Proc string
	Sys  string
}

// Check returns the warnings for the host; none when the files cannot be read (not Linux).
func Check(p Paths) []Warning {
	if p.Proc == "" {
		p.Proc = "/proc"
	}
	if p.Sys == "" {
		p.Sys = "/sys"
	}
	var out []Warning
	if appArmorSignals(p) {
		out = append(out, AppArmorSignals)
	}
	return out
}

func appArmorSignals(p Paths) bool {
	return read(p.Sys, "module/apparmor/parameters/enabled") == "Y" &&
		read(p.Proc, "sys/kernel/apparmor_restrict_unprivileged_unconfined") == "1" &&
		kernelMajor(read(p.Proc, "sys/kernel/osrelease")) >= 7
}

// read returns a file's trimmed content, or "" when it cannot be read.
func read(root, name string) string {
	b, err := os.ReadFile(filepath.Join(root, name)) //nolint:gosec // G304: root is the configured procfs or sysfs mount
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// kernelMajor returns 7 for "7.0.0-1017-gcp"; 0 when it cannot be parsed.
func kernelMajor(release string) int {
	major, _, _ := strings.Cut(release, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}
