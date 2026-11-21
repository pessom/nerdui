// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReadProcess(t *testing.T) {
	root := t.TempDir()
	boot := time.Now().Add(-time.Hour).Unix()
	write := func(path, content string) {
		t.Helper()
		p := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("stat", "cpu  1 2 3\nbtime "+strconv.FormatInt(boot, 10)+"\n")
	// Started 1800 s after boot (180000 ticks), used 900 s of CPU (90000 ticks): 50 % over the
	// 1800 s it has been running.
	fields := make([]string, 52)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[1], fields[2], fields[3] = "42", "(my (odd) app)", "S", "7"
	fields[13], fields[14] = "60000", "30000" // utime, stime
	fields[21], fields[23] = "180000", "256"  // starttime, rss pages
	write("42/stat", strings.Join(fields, " "))
	write("42/status", "Name:\tapp\nUid:\t0\t0\t0\t0\nGid:\t0\t0\t0\t0\n")
	write("42/cmdline", "/usr/bin/app\x00--port\x0080\x00")
	fields[0], fields[1] = "43", "(kworker)"
	write("43/stat", strings.Join(fields, " "))

	users := map[int]string{}
	p, err := readProcess(root, 42, users)
	if err != nil {
		t.Fatal(err)
	}
	if p.PID != 42 || p.PPID != 7 || p.State != "S" || p.UID != 0 || p.User != "root" ||
		p.Command != "/usr/bin/app --port 80" || p.RSS != 256*int64(os.Getpagesize()) {
		t.Errorf("process %+v", p)
	}
	if p.CPU < 49 || p.CPU > 51 {
		t.Errorf("cpu %v", p.CPU)
	}
	if want := time.Unix(boot+1800, 0); !p.Started.Equal(want) {
		t.Errorf("started %v, want %v", p.Started, want)
	}
	// No status and no command line: unknown user, the command name in brackets.
	if p, err := readProcess(root, 43, users); err != nil || p.Command != "[kworker]" || p.UID != -1 {
		t.Errorf("kernel thread %+v %v", p, err)
	}
	if _, err := readProcess(root, 44, users); err == nil {
		t.Error("missing process read")
	}
}

func TestReadProcessesOfThisProcess(t *testing.T) {
	list, err := readProcesses([]int{os.Getpid(), 1 << 30})
	if err != nil || len(list) != 1 || list[0].PID != os.Getpid() || list[0].Command == "" {
		t.Errorf("%+v %v", list, err)
	}
}
