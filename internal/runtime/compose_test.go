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
