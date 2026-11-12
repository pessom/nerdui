// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

// syncBuffer collects terminal output written by containerd's copy goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestIntegrationExec runs interactive processes with a terminal (#15).
func TestIntegrationExec(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	name := "it-exec-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", name).Run() })
	out, err := exec.Command("nerdctl", "run", "-d", "--name", name, "docker.io/library/alpine:3", "sleep", "600").CombinedOutput()
	if err != nil {
		t.Fatalf("nerdctl run: %v: %s", err, out)
	}
	id := strings.TrimSpace(string(out))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	ct := c.Containers()

	// Default shell (sh on Alpine), input, output and exit code; TERM is set.
	var buf syncBuffer
	code, err := ct.Exec(ctx, DefaultNamespace, id, ExecOptions{Cols: 80, Rows: 24},
		strings.NewReader("echo term=$TERM; stty size; exit 7\n"), &buf, nil)
	if err != nil || code != 7 {
		t.Fatalf("exec: code %d, err %v, output %q", code, err, buf.String())
	}
	if s := buf.String(); !strings.Contains(s, "term=xterm-256color") || !strings.Contains(s, "24 80") {
		t.Errorf("output %q", s)
	}

	// User by name, resolved in the container.
	buf = syncBuffer{}
	code, err = ct.Exec(ctx, DefaultNamespace, id, ExecOptions{Cmd: []string{"id", "-u"}, User: "nobody"},
		strings.NewReader(""), &buf, nil)
	if err != nil || code != 0 || !strings.Contains(buf.String(), "65534") {
		t.Errorf("exec as nobody: %d %v %q", code, err, buf.String())
	}

	// Closing the session kills the process.
	sctx, stop := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	defer pw.Close()
	done := make(chan error, 1)
	go func() {
		_, err := ct.Exec(sctx, DefaultNamespace, id, ExecOptions{Cmd: []string{"sleep", "300"}}, pr, io.Discard, nil)
		done <- err
	}()
	time.Sleep(time.Second)
	stop()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a killed session reported success")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("exec did not end after the session was closed")
	}
	if ps, _ := exec.Command("nerdctl", "exec", name, "ps").Output(); strings.Contains(string(ps), "sleep 300") {
		t.Errorf("exec process survived:\n%s", ps)
	}

	if out, err := exec.Command("nerdctl", "stop", "-t", "0", name).CombinedOutput(); err != nil {
		t.Fatalf("stop: %v: %s", err, out)
	}
	_, err = ct.Exec(ctx, DefaultNamespace, id, ExecOptions{}, strings.NewReader(""), io.Discard, nil)
	if !errdefs.IsFailedPrecondition(err) {
		t.Errorf("exec in a stopped container: %v", err)
	}
}
