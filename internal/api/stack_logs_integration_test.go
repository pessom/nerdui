// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package api

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/runtime"
)

// TestIntegrationStackLogs merges the logs of a real compose project's containers and follows
// them without repeating the history (#30).
func TestIntegrationStackLogs(t *testing.T) {
	ncfg, _, err := runtime.LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := runtime.New(runtime.DefaultAddress, runtime.DefaultNamespace, runtime.WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	project := "it-logs-" + strconv.FormatInt(time.Now().UnixNano()%1e6, 36)
	dir := t.TempDir()
	compose := `services:
  a:
    image: docker.io/library/alpine:3
    command: ["sh", "-c", "for i in 1 2 3 4 5 6 7 8; do echo a$$i; sleep 0.5; done; sleep 600"]
  b:
    image: docker.io/library/alpine:3
    command: ["sh", "-c", "for i in 1 2 3 4 5 6 7 8; do echo b$$i; sleep 0.5; done; sleep 600"]
`
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("nerdctl", "compose", "-p", project, "--project-directory", dir, "down").Run()
	})
	if out, err := exec.Command("nerdctl", "compose", "-p", project, "--project-directory", dir, "up", "-d").CombinedOutput(); err != nil {
		t.Fatalf("up: %v: %s", err, out)
	}
	time.Sleep(2 * time.Second) // a few lines of history

	list, err := c.Containers().List(ctx, runtime.DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	var containers []stackContainer
	for _, ct := range list {
		if ct.Compose != nil && ct.Compose.Project == project {
			containers = append(containers, stackContainer{id: ct.ID, name: ct.Name, service: ct.Compose.Service})
		}
	}
	if len(containers) != 2 {
		t.Fatalf("containers %+v", containers)
	}

	followCtx, stop := context.WithCancel(ctx)
	var mu sync.Mutex
	var got []StackLogLine
	done := make(chan error, 1)
	go func() {
		done <- streamStackLogs(followCtx, containers, runtime.LogOptions{Tail: 100, Follow: true},
			func(ctx context.Context, id string, o runtime.LogOptions, emit runtime.LogEmitter) error {
				return c.Containers().Logs(ctx, runtime.DefaultNamespace, id, o, emit)
			},
			func(l StackLogLine) error {
				mu.Lock()
				defer mu.Unlock()
				got = append(got, l)
				if len(got) == 16 {
					stop()
				}
				return nil
			})
	}()
	var streamErr error
	select {
	case streamErr = <-done:
	case <-time.After(30 * time.Second):
		stop()
		streamErr = <-done
	}
	if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
		t.Errorf("stream: %v", streamErr)
	}
	mu.Lock()
	defer mu.Unlock()
	seen := map[string]int{}
	var text []string
	for i, l := range got {
		seen[l.Line]++
		text = append(text, l.Service+":"+l.Line)
		if !strings.HasPrefix(l.Line, l.Service) {
			t.Errorf("line %d %q from service %s", i, l.Line, l.Service)
		}
	}
	for _, s := range []string{"a", "b"} {
		for i := 1; i <= 8; i++ {
			if n := seen[s+strconv.Itoa(i)]; n != 1 {
				t.Errorf("%s%d seen %d times: %v", s, i, n, text)
			}
		}
	}
	t.Logf("stack logs: %v", text)
}
