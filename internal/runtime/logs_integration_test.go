// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

// TestIntegrationLogs reads logs written by nerdctl's json-file driver (#14).
func TestIntegrationLogs(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	cc := c.Containers()
	name := "it-logs-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", name).Run() })

	// Three lines, one on stderr with ANSI colors, then one line per 200 ms until stopped.
	script := `echo one; printf '\033[31mtwo\033[0m\n' >&2; echo three; ` +
		`i=0; while true; do i=$((i+1)); echo "tick $i"; sleep 0.2; done`
	out, err := exec.Command("nerdctl", "run", "-d", "--name", name,
		"docker.io/library/alpine:3", "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("nerdctl run: %v: %s", err, out)
	}
	id := strings.TrimSpace(string(out))

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	collect := func(ctx context.Context, opts LogOptions, until func([]LogLine) bool) ([]LogLine, error) {
		var mu sync.Mutex
		var lines []LogLine
		ctx, stop := context.WithCancel(ctx)
		defer stop()
		err := cc.Logs(ctx, DefaultNamespace, id, opts, func(l LogLine) error {
			mu.Lock()
			defer mu.Unlock()
			lines = append(lines, l)
			if until != nil && until(lines) {
				stop()
			}
			return nil
		})
		mu.Lock()
		defer mu.Unlock()
		return lines, err
	}

	// Follow: lines keep arriving; canceling the context ends the stream promptly.
	start := time.Now()
	lines, err := collect(ctx, LogOptions{Follow: true}, func(l []LogLine) bool { return len(l) >= 8 })
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 15*time.Second {
		t.Errorf("follow took %v", time.Since(start))
	}
	if len(lines) < 8 || lines[0].Line != "one" || lines[2].Line != "three" || lines[0].Time.IsZero() {
		t.Fatalf("follow lines: %+v", lines)
	}
	if lines[1].Stream != "stderr" || lines[1].Line != "\x1b[31mtwo\x1b[0m" {
		t.Errorf("stderr line with ANSI: %+v", lines[1])
	}
	if !strings.HasPrefix(lines[3].Line, "tick ") {
		t.Errorf("line 4: %+v", lines[3])
	}

	// Tail without follow returns immediately.
	tail, err := collect(ctx, LogOptions{Tail: 2}, nil)
	if err != nil || len(tail) != 2 || !strings.HasPrefix(tail[1].Line, "tick ") {
		t.Fatalf("tail: %v %+v", err, tail)
	}
	// since: nothing before the last line's second.
	since, err := collect(ctx, LogOptions{Since: tail[1].Time.Format(time.RFC3339Nano)}, nil)
	if err != nil || len(since) == 0 || since[0].Time.Before(tail[1].Time.Truncate(time.Second)) {
		t.Fatalf("since: %v %+v", err, since)
	}

	// Follow ends by itself when the container stops.
	done := make(chan error, 1)
	go func() {
		_, err := collect(ctx, LogOptions{Follow: true, Tail: 1}, nil)
		done <- err
	}()
	if out, err := exec.Command("nerdctl", "stop", "-t", "1", name).CombinedOutput(); err != nil {
		t.Fatalf("nerdctl stop: %v: %s", err, out)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("follow did not end after the container stopped")
	}

	if _, err := collect(ctx, LogOptions{Since: "yesterday"}, nil); !errdefs.IsInvalidArgument(err) {
		t.Errorf("invalid since: %v", err)
	}
	if err := cc.Logs(ctx, DefaultNamespace, strings.Repeat("f", 64), LogOptions{}, func(LogLine) error { return nil }); !errdefs.IsNotFound(err) {
		t.Errorf("missing container: %v", err)
	}
}
