// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package volbackup

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/secretbox"
	"github.com/pessom/nerdui/internal/store"
)

type fakeRT struct {
	runtime.Runtime
	vols    []runtime.Volume
	cs      []runtime.Container
	actions []string
	data    map[string]string // volume contents
	created []string
}

func (f *fakeRT) Volumes() runtime.Volumes       { return fakeVols{f: f} }
func (f *fakeRT) Containers() runtime.Containers { return fakeContainers{f: f} }

type fakeVols struct {
	runtime.Volumes
	f *fakeRT
}

func (v fakeVols) List(context.Context, string, bool) ([]runtime.Volume, error) { return v.f.vols, nil }

func (v fakeVols) Create(_ context.Context, _, name string, _ map[string]string) (runtime.Volume, error) {
	v.f.created = append(v.f.created, name)
	v.f.vols = append(v.f.vols, runtime.Volume{Name: name})
	return runtime.Volume{Name: name}, nil
}

func (v fakeVols) Download(_, name, _ string) (runtime.VolumeDownload, error) {
	v.f.actions = append(v.f.actions, "read "+name)
	return runtime.VolumeDownload{
		Write: func(w io.Writer) error { _, err := io.WriteString(w, v.f.data[name]); return err },
		Close: func() error { return nil },
	}, nil
}

func (v fakeVols) Restore(_, name string, r io.Reader) error {
	b, err := io.ReadAll(r)
	v.f.actions = append(v.f.actions, "restore "+name)
	v.f.data[name] = string(b)
	return err
}

type fakeContainers struct {
	runtime.Containers
	f *fakeRT
}

func (c fakeContainers) List(context.Context, string) ([]runtime.Container, error) {
	return c.f.cs, nil
}

func (c fakeContainers) Do(_ context.Context, _, id string, a runtime.Action, _ runtime.ActionOptions) error {
	c.f.actions = append(c.f.actions, string(a)+" "+id)
	return nil
}

func newService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	box, err := secretbox.LoadOrCreate(filepath.Join(t.TempDir(), secretbox.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Store: st.Write(), Box: box}
}

func TestTargets(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	for _, in := range []TargetInput{
		{Name: "", Kind: KindLocal, Directory: "/backups"},
		{Name: "x", Kind: KindLocal, Directory: "relative"},
		{Name: "x", Kind: KindLocal, Directory: "/"},
		{Name: "x", Kind: "ftp"},
		{Name: "x", Kind: KindS3},
		{Name: "x", Kind: KindS3, S3: &S3Options{Endpoint: "ftp://h", Bucket: "bkt", AccessKey: "a"}, SecretKey: "s"},
		{Name: "x", Kind: KindS3, S3: &S3Options{Endpoint: "h", Bucket: "B_", AccessKey: "a"}, SecretKey: "s"},
		{Name: "x", Kind: KindS3, S3: &S3Options{Endpoint: "h", Bucket: "bkt", AccessKey: "a"}},
	} {
		if _, err := s.Create(ctx, in); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%+v: %v", in, err)
		}
	}
	s3in := TargetInput{Name: "minio", Kind: KindS3, S3: &S3Options{
		Endpoint: "http://minio:9000", Bucket: "backups", Prefix: "/nerdui/", AccessKey: "ak", PathStyle: true,
	}, SecretKey: "sk-secret"}
	tg, err := s.Create(ctx, s3in)
	if err != nil {
		t.Fatal(err)
	}
	if !tg.HasSecret || tg.S3.Prefix != "nerdui" {
		t.Errorf("created %+v", tg)
	}
	row, _ := s.Store.GetBackupTarget(ctx, tg.ID)
	if strings.Contains(row.Secret, "sk-secret") {
		t.Error("the secret key is stored in plain text")
	}
	if _, err := s.Create(ctx, s3in); !errdefs.IsAlreadyExists(err) {
		t.Errorf("duplicate: %v", err)
	}
	// An empty secret keeps the stored one.
	s3in.SecretKey = ""
	s3in.Name = "minio2"
	if tg, err = s.Update(ctx, tg.ID, s3in); err != nil || tg.Name != "minio2" || !tg.HasSecret {
		t.Errorf("update: %+v %v", tg, err)
	}
	// Another kind needs its own settings.
	if _, err := s.Update(ctx, tg.ID, TargetInput{Name: "m", Kind: KindLocal, Directory: "/srv/b"}); err != nil {
		t.Fatal(err)
	}
	s.InUse = func(context.Context, int64) ([]string, error) { return []string{"nightly"}, nil }
	if err := s.Delete(ctx, tg.ID); !errdefs.IsFailedPrecondition(err) || !strings.Contains(err.Error(), "nightly") {
		t.Errorf("in use: %v", err)
	}
	s.InUse = nil
	if err := s.Delete(ctx, tg.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, tg.ID); !errdefs.IsNotFound(err) {
		t.Errorf("again: %v", err)
	}
}

func TestBackupAndRestore(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	dir := t.TempDir()
	tg, err := s.Create(ctx, TargetInput{Name: "local", Kind: KindLocal, Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Test(ctx, tg.ID); err != nil {
		t.Fatalf("test: %v", err)
	}
	rt := &fakeRT{
		vols: []runtime.Volume{{Name: "pgdata", Containers: []runtime.VolumeMounter{{ID: "c1"}, {ID: "c2"}}}},
		cs: []runtime.Container{
			{ID: "c1", Name: "db", State: runtime.StateRunning},
			{ID: "c2", Name: "old", State: "exited"},
			{ID: "c3", Name: "web", State: runtime.StateRunning},
		},
		data: map[string]string{"pgdata": "v1"},
	}
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	var out bytes.Buffer
	for i := range 4 {
		rt.data["pgdata"] = "v" + string(rune('1'+i))
		if _, err := s.Backup(ctx, rt, tg.ID, "default", "pgdata", Options{Keep: 3, StopContainers: i == 0}, &out); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Hour)
	}
	if !slices.Equal(rt.actions[:3], []string{"stop c1", "read pgdata", "start c1"}) {
		t.Errorf("actions %v", rt.actions)
	}
	list, err := s.List(ctx, tg.ID, "default", "pgdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Key != "default/pgdata/pgdata-20261123-130000.tar.gz" || list[2].Key != "default/pgdata/pgdata-20261123-110000.tar.gz" {
		t.Errorf("list %+v", list)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "default", "pgdata", "pgdata-20261123-130000.tar.gz")); string(b) != "v4" {
		t.Errorf("archive %q", b)
	}

	// The test object is gone; a missing volume is not found.
	if _, err := os.Stat(filepath.Join(dir, ".nerdui-test")); err == nil {
		if l, _ := s.List(ctx, tg.ID, "", ".nerdui-test"); len(l) != 0 {
			t.Errorf("test objects left: %v", l)
		}
	}
	if _, err := s.Backup(ctx, rt, tg.ID, "default", "nope", Options{}, &out); !errdefs.IsNotFound(err) {
		t.Errorf("missing volume: %v", err)
	}

	// Restore refuses a volume in use unless containers are stopped.
	key := list[2].Key
	if err := s.Restore(ctx, rt, tg.ID, key, "default", "pgdata", Options{}, &out); !errdefs.IsFailedPrecondition(err) {
		t.Errorf("in use: %v", err)
	}
	rt.actions = nil
	if err := s.Restore(ctx, rt, tg.ID, key, "default", "pgdata", Options{StopContainers: true}, &out); err != nil {
		t.Fatal(err)
	}
	if rt.data["pgdata"] != "v2" || !slices.Equal(rt.actions, []string{"stop c1", "restore pgdata", "start c1"}) {
		t.Errorf("restored %q, actions %v", rt.data["pgdata"], rt.actions)
	}
	// Into a new volume, which is created.
	if err := s.Restore(ctx, rt, tg.ID, key, "default", "pgdata-copy", Options{}, &out); err != nil {
		t.Fatal(err)
	}
	if rt.data["pgdata-copy"] != "v2" || !slices.Equal(rt.created, []string{"pgdata-copy"}) {
		t.Errorf("copy %q, created %v", rt.data["pgdata-copy"], rt.created)
	}
	for _, k := range []string{"", "../x.tar.gz", "default/pgdata/../../x.tar.gz", "default/x.tar.gz", "default/pgdata/x.tar", "/default/pgdata/x.tar.gz"} {
		if err := s.Restore(ctx, rt, tg.ID, k, "default", "pgdata", Options{}, &out); !errdefs.IsInvalidArgument(err) {
			t.Errorf("key %q: %v", k, err)
		}
	}
	if err := s.Restore(ctx, rt, tg.ID, "default/pgdata/none.tar.gz", "default", "pgdata", Options{}, &out); !errdefs.IsNotFound(err) {
		t.Errorf("missing archive: %v", err)
	}
}

func TestParseEndpoint(t *testing.T) {
	for in, want := range map[string]string{
		"s3.amazonaws.com":    "s3.amazonaws.com https",
		"http://minio:9000":   "minio:9000 http",
		"https://minio:9000/": "minio:9000 https",
		"ftp://x":             "error",
		"https://x/path":      "error",
		"https://user:pw@x":   "error",
		"":                    "error",
	} {
		h, secure, err := parseEndpoint(in)
		got := h + " http"
		if secure {
			got = h + " https"
		}
		if err != nil {
			got = "error"
		}
		if got != want {
			t.Errorf("%q: %s, want %s", in, got, want)
		}
	}
}
