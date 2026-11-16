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

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store/db"
)

// memRegistry is an in-memory stacks table.
type memRegistry struct{ rows []db.Stack }

func (m *memRegistry) ListStacks(context.Context) ([]db.Stack, error) { return m.rows, nil }

func (m *memRegistry) RegisterStack(_ context.Context, a db.RegisterStackParams) error {
	m.rows = append(m.rows, db.Stack{Name: a.Name, Path: a.Path, Mode: a.Mode, CreatedAt: a.Now})
	return nil
}

func (m *memRegistry) DeleteStack(_ context.Context, name string) (int64, error) {
	for i, r := range m.rows {
		if r.Name == name {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

// fakeDescriber names a project after its directory and reports local paths per directory.
type fakeDescriber struct{ local map[string][]string }

func (f fakeDescriber) Describe(_ context.Context, dir, file string) (runtime.ComposeProject, error) {
	if filepath.Base(dir) == "broken" {
		return runtime.ComposeProject{}, errors.New("yaml: bad")
	}
	return runtime.ComposeProject{
		Name: filepath.Base(dir), Services: []string{"web"}, Files: []string{file}, LocalPaths: f.local[dir],
	}, nil
}

func TestDiscoverAndAdopt(t *testing.T) {
	stacksDir := filepath.Join(t.TempDir(), "stacks")
	write(t, filepath.Join(stacksDir, "app", "compose.yaml"))
	scanRoot := t.TempDir()
	write(t, filepath.Join(scanRoot, "media", "compose.yaml"))
	write(t, filepath.Join(scanRoot, "media", ".env"))
	write(t, filepath.Join(scanRoot, "group", "blog", "docker-compose.yml"))
	write(t, filepath.Join(scanRoot, "group", "deep", "x", "compose.yaml")) // depth 3: not scanned
	write(t, filepath.Join(scanRoot, "broken", "compose.yaml"))
	write(t, filepath.Join(scanRoot, "app", "compose.yaml")) // same name as a managed stack
	write(t, filepath.Join(scanRoot, ".git", "compose.yaml"))
	media := filepath.Join(scanRoot, "media")
	blog := filepath.Join(scanRoot, "group", "blog")

	reg := &memRegistry{}
	d := fakeDescriber{local: map[string][]string{blog: {filepath.Join(blog, "data")}}}
	s := &Service{Dir: stacksDir, Registry: reg, Writer: reg, ScanDirs: []string{scanRoot, stacksDir}}
	ctx := context.Background()

	found, err := s.Discover(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range found {
		names = append(names, c.Name)
	}
	if !slices.Equal(names, []string{"app", "broken", "blog", "media"}) {
		t.Fatalf("discovered %v", names)
	}
	if !found[0].Taken || found[1].Error == "" || len(found[2].LocalPaths) != 1 || found[3].ComposeFile != "compose.yaml" {
		t.Errorf("candidates %+v", found)
	}

	// In place: registered, then no longer discovered.
	def, err := s.Adopt(ctx, d, AdoptRequest{Name: "media", Path: media, Mode: AdoptInPlace})
	if err != nil || def.Mode != ModeExternal || def.Dir != media || len(reg.rows) != 1 {
		t.Fatalf("adopt in place: %+v %v", def, err)
	}
	if found, _ := s.Discover(ctx, d); slices.ContainsFunc(found, func(c Candidate) bool { return c.Path == media }) {
		t.Error("an adopted directory is still discovered")
	}
	if _, err := s.Adopt(ctx, d, AdoptRequest{Name: "media2", Path: media, Mode: AdoptInPlace}); !errdefs.IsAlreadyExists(err) {
		t.Errorf("same directory twice: %v", err)
	}
	if _, err := s.Adopt(ctx, d, AdoptRequest{Name: "app", Path: blog, Mode: AdoptInPlace}); !errdefs.IsAlreadyExists(err) {
		t.Errorf("taken name: %v", err)
	}

	// Copy: refused when the project uses files of its directory.
	if _, err := s.Adopt(ctx, d, AdoptRequest{Name: "blog", Path: blog, Mode: AdoptCopy}); !errdefs.IsFailedPrecondition(err) {
		t.Errorf("copy with local paths: %v", err)
	}
	other := filepath.Join(scanRoot, "app")
	def, err = s.Adopt(ctx, d, AdoptRequest{Name: "copied", Path: other, Mode: AdoptCopy})
	if err != nil || def.Mode != ModeManaged || def.Dir != filepath.Join(stacksDir, "copied") {
		t.Fatalf("copy: %+v %v", def, err)
	}
	if b, err := os.ReadFile(filepath.Join(stacksDir, "copied", "compose.yaml")); err != nil || string(b) != "services: {}\n" {
		t.Errorf("copied file: %q %v", b, err)
	}
	if entries, _ := os.ReadDir(stacksDir); len(entries) != 2 {
		t.Errorf("stacks dir has leftovers: %v", entries)
	}

	bad := []AdoptRequest{
		{Name: "x", Path: "relative", Mode: AdoptInPlace},
		{Name: "bad name!", Path: media, Mode: AdoptInPlace},
		{Name: "x", Path: filepath.Join(scanRoot, "group"), Mode: AdoptInPlace}, // no compose file
		{Name: "x", Path: filepath.Join(stacksDir, "app"), Mode: AdoptInPlace},  // a stack already
		{Name: "x", Path: blog, Mode: "move"},
	}
	for _, r := range bad {
		if _, err := s.Adopt(ctx, d, r); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%+v: %v", r, err)
		}
	}

	if err := s.Forget(ctx, "media"); err != nil || len(reg.rows) != 0 {
		t.Errorf("forget: %v %v", err, reg.rows)
	}
	if _, err := os.Stat(filepath.Join(media, "compose.yaml")); err != nil {
		t.Errorf("forget must keep files: %v", err)
	}
	if err := s.Forget(ctx, "app"); !errdefs.IsFailedPrecondition(err) {
		t.Errorf("forget managed: %v", err)
	}
	if err := s.Forget(ctx, "nope"); !errdefs.IsNotFound(err) {
		t.Errorf("forget missing: %v", err)
	}
}
