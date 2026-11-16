// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package stacks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store/db"
)

type fakeRegistry []db.Stack

func (f fakeRegistry) ListStacks(context.Context) ([]db.Stack, error) { return f, nil }

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDefinitions(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "web", "compose.yaml"))
	write(t, filepath.Join(dir, "old", "docker-compose.yml"))
	write(t, filepath.Join(dir, "both", "compose.yml"))
	write(t, filepath.Join(dir, "both", "docker-compose.yaml"))
	write(t, filepath.Join(dir, "notes", "README.md"))
	write(t, filepath.Join(dir, ".hidden", "compose.yaml"))
	ext := t.TempDir()
	write(t, filepath.Join(ext, "compose.yaml"))

	s := &Service{Dir: dir, Registry: fakeRegistry{
		{Name: "media", Path: ext, Mode: ModeExternal},
		{Name: "web", Path: "/elsewhere", Mode: ModeExternal}, // a managed stack wins
		{Name: "gone", Path: filepath.Join(ext, "missing"), Mode: ModeExternal},
	}}
	defs, err := s.Definitions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Definition{
		{Name: "both", Dir: filepath.Join(dir, "both"), File: "compose.yml", Mode: ModeManaged},
		{Name: "gone", Dir: filepath.Join(ext, "missing"), Mode: ModeExternal},
		{Name: "media", Dir: ext, File: "compose.yaml", Mode: ModeExternal},
		{Name: "old", Dir: filepath.Join(dir, "old"), File: "docker-compose.yml", Mode: ModeManaged},
		{Name: "web", Dir: filepath.Join(dir, "web"), File: "compose.yaml", Mode: ModeManaged},
	}
	if !slices.Equal(defs, want) {
		t.Errorf("definitions:\n%+v\nwant\n%+v", defs, want)
	}

	none := &Service{Dir: filepath.Join(dir, "absent")}
	if defs, err := none.Definitions(context.Background()); err != nil || len(defs) != 0 {
		t.Errorf("missing stacks dir: %v %v", defs, err)
	}
}

func ctr(id, project, service, state string) runtime.Container {
	return runtime.Container{ID: id, Name: id, State: state, Compose: &runtime.ComposeRef{Project: project, Service: service}}
}

func TestSummarize(t *testing.T) {
	def := Definition{Name: "app", Dir: "/s/app", File: "compose.yaml", Mode: ModeManaged}
	containers := []runtime.Container{
		ctr("w1", "app", "web", runtime.StateRunning),
		ctr("w2", "app", "web", "exited"),
		ctr("d1", "app", "db", "exited"),
		ctr("o1", "app", "old", runtime.StateRunning), // not in the compose file
		ctr("x1", "other", "web", runtime.StateRunning),
		{ID: "plain", State: runtime.StateRunning},
	}
	st := Summarize(def, []string{"web", "db"}, nil, containers)
	if st.Status != StatusPartial || st.Containers != 4 || st.Running != 2 || len(st.Services) != 3 {
		t.Fatalf("partial: %+v", st)
	}
	if s := st.Services[0]; s.Name != "web" || !s.Defined || s.Running != 1 || len(s.Containers) != 2 {
		t.Errorf("web %+v", s)
	}
	if s := st.Services[2]; s.Name != "old" || s.Defined || s.Running != 1 {
		t.Errorf("old %+v", s)
	}

	containers[2].State = runtime.StateRunning
	if st := Summarize(def, []string{"web", "db"}, nil, containers); st.Status != StatusRunning {
		t.Errorf("running: %s", st.Status)
	}
	// A running orphan service does not make a stopped stack partial.
	if st := Summarize(def, []string{"api"}, nil, containers[3:4]); st.Status != StatusStopped {
		t.Errorf("stopped: %s", st.Status)
	}
	if st := Summarize(def, []string{"web"}, errors.New("yaml: bad"), nil); st.Status != StatusUnknown || st.Error != "yaml: bad" {
		t.Errorf("load error: %+v", st)
	}
	if st := Summarize(Definition{Name: "x", Dir: "/s/x", Mode: ModeExternal}, nil, nil, nil); st.Status != StatusUnknown || st.Error == "" {
		t.Errorf("no file: %+v", st)
	}
	if st := Summarize(Definition{Name: "other", Mode: ModeUnmanaged}, nil, nil, containers); st.Status != StatusUnknown || st.Error != "" || st.Running != 1 {
		t.Errorf("unmanaged: %+v", st)
	}

	if got := Unmanaged([]Definition{def}, containers); !slices.Equal(got, []string{"other"}) {
		t.Errorf("unmanaged %v", got)
	}
}
