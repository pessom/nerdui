// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/config"

	nvolume "github.com/containerd/nerdctl/v2/pkg/cmd/volume"
)

// testVolume creates a volume with files in a temporary nerdctl data root.
func testVolume(t *testing.T) (clientVolumes, string) {
	t.Helper()
	cfg := config.New()
	cfg.DataRoot = t.TempDir()
	cfg.Address = filepath.Join(t.TempDir(), "containerd.sock")
	if err := os.WriteFile(cfg.Address, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(cfg.Address, "default", WithNerdctlConfig(cfg))
	vs, err := nvolume.Store("default", cfg.DataRoot, cfg.Address)
	if err != nil {
		t.Fatal(err)
	}
	vol, err := vs.Create("data", nil)
	if err != nil {
		t.Fatal(err)
	}
	write := func(p, content string) {
		t.Helper()
		full := filepath.Join(vol.Mountpoint, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("app.conf", "listen 80\n")
	write(".env", "TOKEN=x\n")
	write("db/data.bin", "\x00\x01\x02")
	write("db/nested/note.txt", strings.Repeat("é", MaxPreviewBytes)) // cut inside a rune
	if err := os.Symlink("app.conf", filepath.Join(vol.Mountpoint, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc", filepath.Join(vol.Mountpoint, "escape")); err != nil {
		t.Fatal(err)
	}
	return clientVolumes{c}, vol.Mountpoint
}

func TestVolumeListFiles(t *testing.T) {
	cv, _ := testVolume(t)
	list, truncated, err := cv.ListFiles("default", "data", "/")
	if err != nil || truncated {
		t.Fatal(err, truncated)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name+":"+e.Type)
	}
	want := []string{"db:dir", ".env:file", "app.conf:file", "escape:symlink", "link:symlink"}
	if !slices.Equal(names, want) {
		t.Errorf("entries %v, want %v", names, want)
	}
	if list[4].Target != "app.conf" || list[4].TargetType != "file" || list[3].TargetType != "" || list[2].Size != 10 {
		t.Errorf("entries %+v", list)
	}
	if l, _, err := cv.ListFiles("default", "data", "db/../db/nested"); err != nil || len(l) != 1 {
		t.Errorf("nested: %+v %v", l, err)
	}
	for p, check := range map[string]func(error) bool{
		"/missing":   errdefs.IsNotFound,
		"/app.conf":  errdefs.IsInvalidArgument,
		"/escape":    errdefs.IsInvalidArgument,
		"/../../etc": errdefs.IsNotFound, // cleaned to /etc inside the volume
	} {
		if _, _, err := cv.ListFiles("default", "data", p); !check(err) {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, _, err := cv.ListFiles("default", "missing", "/"); !errdefs.IsNotFound(err) {
		t.Errorf("missing volume: %v", err)
	}
	if _, _, err := cv.ListFiles("default", "../x", "/"); !errdefs.IsInvalidArgument(err) {
		t.Errorf("bad volume name: %v", err)
	}
}

func TestVolumePreviewFile(t *testing.T) {
	cv, _ := testVolume(t)
	if f, err := cv.PreviewFile("default", "data", "/link"); err != nil || f.Content != "listen 80\n" || f.Binary || f.Truncated {
		t.Errorf("text via link: %+v %v", f, err)
	}
	if f, err := cv.PreviewFile("default", "data", "db/data.bin"); err != nil || !f.Binary || f.Content != "" {
		t.Errorf("binary: %+v %v", f, err)
	}
	if f, err := cv.PreviewFile("default", "data", "db/nested/note.txt"); err != nil || f.Binary || !f.Truncated || len(f.Content) > MaxPreviewBytes {
		t.Errorf("long text: binary=%v truncated=%v len=%d %v", f.Binary, f.Truncated, len(f.Content), err)
	}
	if _, err := cv.PreviewFile("default", "data", "db"); !errdefs.IsInvalidArgument(err) {
		t.Errorf("directory: %v", err)
	}
	if _, err := cv.PreviewFile("default", "data", "escape/passwd"); !errdefs.IsInvalidArgument(err) {
		t.Errorf("escape: %v", err)
	}
}

func TestVolumeDownload(t *testing.T) {
	cv, _ := testVolume(t)
	d, err := cv.Download("default", "data", "/app.conf")
	if err != nil || d.Name != "app.conf" || d.Size != 10 {
		t.Fatalf("%+v %v", d, err)
	}
	var buf bytes.Buffer
	if err := d.Write(&buf); err != nil || buf.String() != "listen 80\n" {
		t.Errorf("file: %q %v", buf.String(), err)
	}
	_ = d.Close()

	d, err = cv.Download("default", "data", "/")
	if err != nil || d.Name != "data.tar.gz" || d.Size != -1 {
		t.Fatalf("%+v %v", d, err)
	}
	buf.Reset()
	if err := d.Write(&buf); err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	got := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		got[h.Name] = string(b) + h.Linkname
	}
	for name, want := range map[string]string{
		"data/": "", "data/app.conf": "listen 80\n", "data/.env": "TOKEN=x\n", "data/db/": "",
		"data/link": "app.conf", "data/escape": "/etc",
	} {
		if v, ok := got[name]; !ok || v != want {
			t.Errorf("archive entry %s = %q (present %v); entries %v", name, v, ok, slices.Sorted(func(yield func(string) bool) {
				for k := range got {
					if !yield(k) {
						return
					}
				}
			}))
		}
	}

	d, err = cv.Download("default", "data", "db/nested")
	if err != nil || d.Name != "nested.tar.gz" {
		t.Fatalf("%+v %v", d, err)
	}
	_ = d.Close()
	if _, err := cv.Download("default", "data", "escape"); !errdefs.IsInvalidArgument(err) {
		t.Errorf("escape: %v", err)
	}
}
