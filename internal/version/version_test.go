// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	info := Get()

	if info.Version == "" {
		t.Fatal("version must not be empty")
	}
	if info.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	if want := runtime.GOOS + "/" + runtime.GOARCH; info.Platform != want {
		t.Errorf("Platform = %q, want %q", info.Platform, want)
	}
	if !strings.HasPrefix(info.String(), "nerdui "+info.Version) {
		t.Errorf("String() = %q, want prefix %q", info.String(), "nerdui "+info.Version)
	}
}

func TestSourceAndLicense(t *testing.T) {
	info := Get()
	if info.Source == "" {
		t.Error("Source must be set (AGPL-3.0 section 13)")
	}
	if info.License != "AGPL-3.0-only" {
		t.Errorf("License = %q", info.License)
	}
	if !strings.Contains(info.String(), info.Source) {
		t.Errorf("String() must mention source URL: %q", info.String())
	}
}
