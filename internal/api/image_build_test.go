// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
)

func buildServer(t *testing.T, f *fakeRuntime) (string, *jobs.Manager, string) {
	t.Helper()
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	dir := t.TempDir()
	subject := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(f, "default")), Jobs: m, BuildDir: dir}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, subject, nil))
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv.URL + "/api/v1/environments/local/images", m, dir
}

func tarOf(t *testing.T, files map[string]string, links map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	for name, target := range links {
		if err := tw.WriteHeader(&tar.Header{Name: name, Linkname: target, Typeflag: tar.TypeSymlink}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// postArchive sends a multipart archive build.
func postArchive(t *testing.T, url, request string, archive []byte) (*http.Response, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	rq, _ := mw.CreateFormField("request")
	_, _ = rq.Write([]byte(request))
	ar, _ := mw.CreateFormFile("archive", "context.tar")
	_, _ = ar.Write(archive)
	_ = mw.Close()
	resp, err := http.Post(url, mw.FormDataContentType(), &body) //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func jobID(t *testing.T, body map[string]any) string {
	t.Helper()
	job, ok := body["job"].(map[string]any)
	if !ok {
		t.Fatalf("no job: %v", body)
	}
	return job["id"].(string)
}

func TestBuildFromDockerfile(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}}
	base, m, dir := buildServer(t, f)

	resp, body := postJSON(t, base+"/build?namespace=k8s.io", `{
		"source": "dockerfile", "dockerfile": "FROM alpine\nRUN echo hi\n",
		"tags": ["registry.local/app:1", "registry.local/app:latest"],
		"args": {"VERSION": "1"}, "noCache": true, "push": true
	}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("build: %d %v", resp.StatusCode, body)
	}
	j := waitDone(t, m, jobID(t, body))
	if j.Status != jobs.StatusSucceeded || j.Kind != "image.build" || j.Title != "Build registry.local/app:1" {
		t.Fatalf("job %+v", j)
	}
	if len(f.builds) != 1 {
		t.Fatalf("builds %+v", f.builds)
	}
	b := f.builds[0]
	if b.ns != "k8s.io" || b.dockerfile != "FROM alpine\nRUN echo hi\n" || !b.opts.NoCache || b.opts.Args["VERSION"] != "1" {
		t.Errorf("build %+v", b)
	}
	var pushed []string
	for _, p := range f.pushes {
		pushed = append(pushed, p.ref)
	}
	if !slices.Equal(pushed, []string{"registry.local/app:1", "registry.local/app:latest"}) {
		t.Errorf("pushed %v", pushed)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("build directory left: %v", entries)
	}
}

func TestBuildValidation(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}}
	base, _, dir := buildServer(t, f)
	for name, req := range map[string]string{
		"no tags":         `{"source":"dockerfile","dockerfile":"FROM alpine"}`,
		"empty file":      `{"source":"dockerfile","dockerfile":"  ","tags":["a"]}`,
		"unknown source":  `{"source":"ftp","tags":["a"]}`,
		"archive as json": `{"source":"archive","tags":["a"]}`,
		"bad git url":     `{"source":"git","url":"file:///etc","tags":["a"]}`,
		"bad arg":         `{"source":"dockerfile","dockerfile":"FROM a","tags":["a"],"args":{"A B":"1"}}`,
	} {
		resp, body := postJSON(t, base+"/build", req)
		if name == "bad git url" {
			// Without Git stacks, Git builds are not available at all.
			if resp.StatusCode != http.StatusNotImplemented {
				t.Errorf("%s: %d %v", name, resp.StatusCode, body)
			}
			continue
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d %v", name, resp.StatusCode, body)
		}
	}
	if len(f.builds) != 0 {
		t.Errorf("built: %+v", f.builds)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("build directories left: %v", entries)
	}
}

func TestBuildFromArchive(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}}
	base, m, dir := buildServer(t, f)

	archive := tarOf(t, map[string]string{"docker/Dockerfile": "FROM busybox\n", "app/main.sh": "echo\n"}, nil)
	resp, body := postArchive(t, base+"/build", `{"source":"archive","file":"docker/Dockerfile","tags":["app:1"]}`, archive)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("build: %d %v", resp.StatusCode, body)
	}
	if j := waitDone(t, m, jobID(t, body)); j.Status != jobs.StatusSucceeded {
		t.Fatalf("job %+v", j)
	}
	if len(f.builds) != 1 || f.builds[0].dockerfile != "FROM busybox\n" || f.builds[0].opts.Dockerfile != "docker/Dockerfile" {
		t.Fatalf("builds %+v", f.builds)
	}

	// An entry outside the context is refused before a job starts.
	resp, body = postArchive(t, base+"/build", `{"source":"archive","tags":["app:1"]}`, tarOf(t, map[string]string{"../x": "y"}, nil))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("escaping archive: %d %v", resp.StatusCode, body)
	}

	// A Dockerfile that links to a file of the host fails the job without building.
	resp, body = postArchive(t, base+"/build", `{"source":"archive","tags":["app:2"]}`,
		tarOf(t, nil, map[string]string{"Dockerfile": "/etc/hosts"}))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("linked Dockerfile: %d %v", resp.StatusCode, body)
	}
	j := waitDone(t, m, jobID(t, body))
	if j.Status != jobs.StatusFailed || !strings.Contains(j.Error, "inside the context") {
		t.Errorf("linked Dockerfile job %+v", j)
	}
	if len(f.builds) != 1 {
		t.Errorf("built with a linked Dockerfile: %+v", f.builds)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("build directories left: %v", entries)
	}
}

func TestBuilderStatus(t *testing.T) {
	f := &fakeRuntime{builder: runtime.BuilderStatus{Error: "buildkitd is not running"}}
	base, _, _ := buildServer(t, f)
	resp, err := http.Get(base + "/builder") //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got runtime.BuilderStatus
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if resp.StatusCode != http.StatusOK || got.Available || got.Error != "buildkitd is not running" {
		t.Errorf("%d %+v", resp.StatusCode, got)
	}
}
