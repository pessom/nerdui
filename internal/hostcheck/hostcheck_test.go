// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package hostcheck

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func host(t *testing.T, files map[string]string) Paths {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return Paths{Proc: filepath.Join(root, "proc"), Sys: filepath.Join(root, "sys")}
}

func TestAppArmorSignals(t *testing.T) {
	ubuntu := map[string]string{
		"sys/module/apparmor/parameters/enabled":                    "Y",
		"proc/sys/kernel/apparmor_restrict_unprivileged_unconfined": "1",
		"proc/sys/kernel/osrelease":                                 "7.0.0-1017-gcp",
	}
	with := func(name, value string) map[string]string {
		m := maps.Clone(ubuntu)
		if value == "" {
			delete(m, name)
		} else {
			m[name] = value
		}
		return m
	}
	tests := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"Ubuntu 26.04", ubuntu, true},
		{"restriction off", with("proc/sys/kernel/apparmor_restrict_unprivileged_unconfined", "0"), false},
		{"no restriction setting", with("proc/sys/kernel/apparmor_restrict_unprivileged_unconfined", ""), false},
		{"AppArmor disabled", with("sys/module/apparmor/parameters/enabled", "N"), false},
		{"no AppArmor", with("sys/module/apparmor/parameters/enabled", ""), false},
		{"Linux 6", with("proc/sys/kernel/osrelease", "6.17.0-14-generic"), false},
		{"Linux 8", with("proc/sys/kernel/osrelease", "8.1.0"), true},
		{"nothing readable", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slices.Contains(Check(host(t, tt.files)), AppArmorSignals)
			if got != tt.want {
				t.Errorf("AppArmorSignals = %v, want %v", got, tt.want)
			}
		})
	}
}
