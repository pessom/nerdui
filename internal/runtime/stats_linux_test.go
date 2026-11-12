// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"strings"
	"testing"
	"time"
)

func TestParseProcStat(t *testing.T) {
	in := "cpu  100 0 50 800 10 0 40 0 0 0\ncpu0 50 0 25 400 5 0 20 0 0 0\ncpu1 50 0 25 400 5 0 20 0 0 0\nintr 123\n"
	total, cpus, err := parseProcStat(strings.NewReader(in))
	if err != nil || cpus != 2 || total != 1000*uint64(time.Second)/clockTicks {
		t.Errorf("total %d, cpus %d, err %v", total, cpus, err)
	}
	if _, _, err := parseProcStat(strings.NewReader("cpu 1 2\n")); err == nil {
		t.Error("short cpu line accepted")
	}
}
