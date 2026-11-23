// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	got, err = ComposeArgs("app", "/srv/app", ComposePull, ComposeOptions{Services: []string{"web", "db_1"}})
	want = []string{"compose", "--project-name=app", "--project-directory=/srv/app", "pull", "web", "db_1"}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("pull services: %q %v", got, err)
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
		{"app", "/srv/app", ComposeUp, ComposeOptions{Services: []string{"--build"}}},
		{"app", "/srv/app", ComposeStop, ComposeOptions{Services: []string{"web"}}},
	}
	for _, b := range bad {
		if _, err := ComposeArgs(b.project, b.dir, b.op, b.opts); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%+v: %v", b, err)
		}
	}
}

func TestComposeValidate(t *testing.T) {
	dir := t.TempDir()
	for n, c := range map[string]string{
		"compose.yaml":          "services:\n  web:\n    image: nginx:${TAG}\n",
		"compose.override.yaml": "services:\n  web:\n    environment: [A=1]\n",
		".env":                  "TAG=1.27\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cc := clientCompose{}
	ctx := context.Background()

	// The files on disk.
	if svcs, err := cc.Validate(ctx, "app", dir, "compose.yaml", nil, nil); err != nil || !slices.Equal(svcs, []string{"web"}) {
		t.Fatalf("disk: %v %v", svcs, err)
	}
	// New content is checked with the override and .env of the directory.
	svcs, err := cc.Validate(ctx, "app", dir, "compose.yaml",
		[]byte("services:\n  web:\n    image: nginx\n  db:\n    image: postgres:${PG:?PG is required}\n"), []byte("PG=16\n"))
	slices.Sort(svcs)
	if err != nil || !slices.Equal(svcs, []string{"db", "web"}) {
		t.Errorf("candidate: %v %v", svcs, err)
	}
	// A required variable missing from the candidate .env.
	if _, err := cc.Validate(ctx, "app", dir, "compose.yaml",
		[]byte("services:\n  db:\n    image: postgres:${PG:?PG is required}\n"), []byte("")); !errdefs.IsInvalidArgument(err) || !strings.Contains(err.Error(), "PG is required") {
		t.Errorf("missing variable: %v", err)
	}
	// Broken YAML: the error names compose.yaml, not the temporary file.
	_, err = cc.Validate(ctx, "app", dir, "compose.yaml", []byte("services:\n  web: [\n"), nil)
	if !errdefs.IsInvalidArgument(err) || strings.Contains(err.Error(), ".nerdui-validate") {
		t.Errorf("broken yaml: %v", err)
	}
	// Unknown keys are rejected by the schema.
	if _, err := cc.Validate(ctx, "app", dir, "compose.yaml", []byte("services:\n  web:\n    imagee: nginx\n"), nil); !errdefs.IsInvalidArgument(err) {
		t.Errorf("schema: %v", err)
	}
	// Temporary files are removed.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Errorf("leftovers: %v", entries)
	}
	if _, err := cc.Validate(ctx, "app", dir, "../compose.yaml", nil, nil); !errdefs.IsInvalidArgument(err) {
		t.Errorf("file name: %v", err)
	}
}

func TestComposeVariables(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	for n, c := range map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx:${TAG}\n    environment: [\"PW=${PW:?PW is required}\"]\n",
		".env":         "TAG=1.27\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var asked []string
	c := New("", "", WithComposeVars(func(_ context.Context, stack string) (map[string]string, error) {
		asked = append(asked, stack)
		return map[string]string{"PW": "s3cret", "TAG": "1.29"}, nil
	}))
	cc := clientCompose{c}
	ctx := context.Background()
	if _, err := (clientCompose{}).Validate(ctx, "app", dir, "compose.yaml", nil, nil); err == nil {
		t.Error("PW is not required without variables")
	}
	if svcs, err := cc.Validate(ctx, "app", dir, "compose.yaml", nil, nil); err != nil || !slices.Equal(svcs, []string{"web"}) {
		t.Errorf("validate: %v %v", svcs, err)
	}
	if svcs, err := cc.Services(ctx, "default", "app", dir, file); err != nil || !slices.Equal(svcs, []string{"web"}) {
		t.Errorf("services: %v %v", svcs, err)
	}
	// The variables win over .env.
	p, err := loadProject(ctx, "app", dir, file, withVars(map[string]string{"PW": "x", "TAG": "1.29"}))
	if err != nil {
		t.Fatal(err)
	}
	if img := p.Services["web"].Image; img != "nginx:1.29" {
		t.Errorf("image %q", img)
	}
	if !slices.Equal(asked, []string{"app", "app"}) {
		t.Errorf("asked %v", asked)
	}
	failing := clientCompose{New("", "", WithComposeVars(func(context.Context, string) (map[string]string, error) {
		return nil, errors.New("bad key")
	}))}
	if _, err := failing.Validate(ctx, "app", dir, "compose.yaml", nil, nil); err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Errorf("failing source: %v", err)
	}
}
