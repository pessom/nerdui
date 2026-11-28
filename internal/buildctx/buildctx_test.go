// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package buildctx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/errdefs"
)

type entry struct {
	name, body, link string
	typ              byte
	mode             int64
}

func archive(t *testing.T, gz bool, entries ...entry) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	var tw *tar.Writer
	var zw *gzip.Writer
	if gz {
		zw = gzip.NewWriter(&buf)
		tw = tar.NewWriter(zw)
	} else {
		tw = tar.NewWriter(&buf)
	}
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: mode, Size: int64(len(e.body)), Linkname: e.link}
		if typ != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if zw != nil {
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return &buf
}

func TestExtract(t *testing.T) {
	for _, gz := range []bool{false, true} {
		dir := t.TempDir()
		err := Extract(dir, archive(t, gz,
			entry{name: "./", typ: tar.TypeDir},
			entry{name: "Dockerfile", body: "FROM alpine\n"},
			entry{name: "src/app/main.go", body: "package main\n"},
			entry{name: "run.sh", body: "#!/bin/sh\n", mode: 0o755},
			entry{name: "latest", typ: tar.TypeSymlink, link: "src/app"},
			entry{name: "copy.go", typ: tar.TypeLink, link: "src/app/main.go"},
			entry{name: "dev", typ: tar.TypeChar},
		), 0)
		if err != nil {
			t.Fatalf("gzip=%v: %v", gz, err)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "src/app/main.go")); string(b) != "package main\n" {
			t.Errorf("gzip=%v: main.go %q", gz, b)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "copy.go")); string(b) != "package main\n" {
			t.Errorf("gzip=%v: hard link %q", gz, b)
		}
		if fi, err := os.Stat(filepath.Join(dir, "run.sh")); err != nil || fi.Mode().Perm()&0o100 == 0 {
			t.Errorf("gzip=%v: run.sh not executable: %v", gz, fi)
		}
		if l, _ := os.Readlink(filepath.Join(dir, "latest")); l != "src/app" {
			t.Errorf("gzip=%v: link %q", gz, l)
		}
		if _, err := os.Lstat(filepath.Join(dir, "dev")); err == nil {
			t.Errorf("gzip=%v: device file created", gz)
		}
	}
}

func TestExtractStaysInside(t *testing.T) {
	outside := t.TempDir()
	cases := map[string][]entry{
		"dot-dot":       {{name: "../escape", body: "x"}},
		"nested dotdot": {{name: "a/../../escape", body: "x"}},
		"absolute":      {{name: "/tmp/escape", body: "x"}},
		"through link":  {{name: "out", typ: tar.TypeSymlink, link: outside}, {name: "out/escape", body: "x"}},
		"hard link out": {{name: "h", typ: tar.TypeLink, link: "../../etc/passwd"}},
	}
	for name, entries := range cases {
		dir := t.TempDir()
		if err := Extract(dir, archive(t, false, entries...), 0); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(outside, "escape")); err == nil {
			t.Fatalf("%s: wrote outside the context", name)
		}
	}
}

func TestExtractLimits(t *testing.T) {
	dir := t.TempDir()
	big := archive(t, false, entry{name: "a", body: "0123456789"}, entry{name: "b", body: "0123456789"})
	if err := Extract(dir, big, 15); !errdefs.IsInvalidArgument(err) {
		t.Errorf("size limit: %v", err)
	}
	if err := Extract(t.TempDir(), archive(t, false), 0); !errdefs.IsInvalidArgument(err) {
		t.Errorf("empty archive: %v", err)
	}
	if err := Extract(t.TempDir(), bytes.NewBufferString("not an archive at all, just text"), 0); !errdefs.IsInvalidArgument(err) {
		t.Errorf("garbage: %v", err)
	}
}

func TestWriteDockerfile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteDockerfile(dir, "FROM alpine\nRUN true\n"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "Dockerfile")); string(b) != "FROM alpine\nRUN true\n" {
		t.Errorf("Dockerfile %q", b)
	}
	if err := WriteDockerfile(t.TempDir(), "  \n"); !errdefs.IsInvalidArgument(err) {
		t.Errorf("empty: %v", err)
	}
}
