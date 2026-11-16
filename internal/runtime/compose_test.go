// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/containerd/errdefs"
)

func TestComposeDescribe(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"compose.yaml": `name: blog
services:
  web:
    build: .
    volumes:
      - ./data:/data
      - /srv/shared:/shared
      - cache:/cache
    env_file: web.env
  db:
    image: postgres
    secrets: [pw]
  debug:
    image: alpine
    profiles: [debug]
secrets:
  pw:
    file: ./secrets/pw.txt
volumes:
  cache: {}
`,
		"compose.override.yaml": "services:\n  db:\n    environment: [A=1]\n",
		".env":                  "X=1\n",
		"web.env":               "Y=2\n",
	}
	for n, c := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := clientCompose{}.Describe(context.Background(), dir, filepath.Join(dir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(p.Services)
	if p.Name != "blog" || !slices.Equal(p.Services, []string{"db", "web"}) || len(p.Files) != 2 {
		t.Errorf("project %+v", p)
	}
	want := []string{
		dir, // build context
		filepath.Join(dir, "data"),
		filepath.Join(dir, "secrets", "pw.txt"),
		filepath.Join(dir, "web.env"),
	}
	if !slices.Equal(p.LocalPaths, want) {
		t.Errorf("local paths %q, want %q", p.LocalPaths, want)
	}

	// Without name:, the project is named after the directory (normalized).
	plain := filepath.Join(t.TempDir(), "My_Stack")
	if err := os.MkdirAll(plain, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, "compose.yaml"), []byte("services:\n  web:\n    image: nginx\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err = clientCompose{}.Describe(context.Background(), plain, filepath.Join(plain, "compose.yaml"))
	if err != nil || p.Name != "my_stack" || len(p.LocalPaths) != 0 {
		t.Errorf("plain %+v %v", p, err)
	}
	if _, err := (clientCompose{}).Services(context.Background(), "default", "Bad Name", plain, filepath.Join(plain, "compose.yaml")); !errdefs.IsInvalidArgument(err) {
		t.Errorf("bad project name: %v", err)
	}
}

func TestComposeArgs(t *testing.T) {
	got, err := ComposeArgs("app", "/srv/app", ComposeUp, ComposeOptions{Pull: true, RemoveOrphans: true})
	want := []string{"compose", "--project-name=app", "--project-directory=/srv/app", "up", "--detach", "--pull=always", "--remove-orphans"}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("up: %q %v", got, err)
	}
	got, err = ComposeArgs("app", "/srv/app", ComposeDown, ComposeOptions{Volumes: true})
	want = []string{"compose", "--project-name=app", "--project-directory=/srv/app", "down", "--volumes"}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("down: %q %v", got, err)
	}
	for _, op := range []string{ComposeStart, ComposeStop, ComposeRestart, ComposePull} {
		if got, err := ComposeArgs("app", "/srv/app", op, ComposeOptions{}); err != nil || got[len(got)-1] != op {
			t.Errorf("%s: %q %v", op, got, err)
		}
	}
	bad := []struct {
		project, dir, op string
		opts             ComposeOptions
	}{
		{"--help", "/srv/app", ComposeUp, ComposeOptions{}},
		{"app", "srv/app", ComposeUp, ComposeOptions{}},
		{"app", "/srv/app", "rm", ComposeOptions{}},
		{"app", "/srv/app", ComposeUp, ComposeOptions{Volumes: true}},
		{"app", "/srv/app", ComposeStop, ComposeOptions{Pull: true}},
		{"app", "/srv/app", ComposeRestart, ComposeOptions{RemoveOrphans: true}},
	}
	for _, b := range bad {
		if _, err := ComposeArgs(b.project, b.dir, b.op, b.opts); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%+v: %v", b, err)
		}
	}
}
