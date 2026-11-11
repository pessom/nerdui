// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"os"
	"testing"
	"time"
)

func TestProcessStartTime(t *testing.T) {
	start := processStartTime(uint32(os.Getpid())) //nolint:gosec // pid fits
	if start == nil {
		t.Fatal("start time of the test process unavailable")
	}
	// The test binary started moments ago (allow for slow CI and clock tick rounding).
	if age := time.Since(*start); age < -2*time.Second || age > 10*time.Minute {
		t.Errorf("start time %v is %v ago", start, age)
	}
	if processStartTime(0) != nil {
		t.Error("pid 0 must yield nil")
	}
	if processStartTime(1<<31) != nil {
		t.Error("missing process must yield nil")
	}
}
