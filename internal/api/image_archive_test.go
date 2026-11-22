// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pessom/nerdui/internal/runtime"
)

func TestSaveImages(t *testing.T) {
	rt := writable()
	rt.images = []runtime.Image{{Name: "docker.io/library/nginx:1.27"}, {Name: "ghcr.io/acme/app:2"}}
	h := newRouter(rt)

	rec := send(t, h, http.MethodGet, "/api/v1/environments/local/images/save?namespace=k8s.io&ref=docker.io/library/nginx:1.27", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "TAR:docker.io/library/nginx:1.27" || rt.gotNS != "k8s.io" ||
		rec.Header().Get("Content-Type") != "application/x-tar" ||
		rec.Header().Get("Content-Disposition") != `attachment; filename="nginx_1.27.tar"` {
		t.Errorf("save: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	rec = send(t, h, http.MethodGet, "/api/v1/environments/local/images/save?ref=docker.io/library/nginx:1.27&ref=ghcr.io/acme/app:2", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), `"images.tar"`) {
		t.Errorf("two images: %d %v", rec.Code, rec.Header())
	}
	if rec := send(t, h, http.MethodGet, "/api/v1/environments/local/images/save?ref=missing:1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("missing image: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodGet, "/api/v1/environments/local/images/save", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("no image: %d %s", rec.Code, rec.Body)
	}

	// A failure after the archive started aborts the response.
	rt.saveErr = errors.New("disk read error")
	defer func() {
		if r := recover(); r != http.ErrAbortHandler { //nolint:errorlint // recover returns the value passed to panic
			t.Errorf("recovered %v, want ErrAbortHandler", r)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/environments/local/images/save?ref=ghcr.io/acme/app:2", nil))
}

func TestLoadImages(t *testing.T) {
	rt := writable()
	h := newRouter(rt)
	load := func(ct, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments/local/images/load?namespace=k8s.io", strings.NewReader(body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rec := load("application/x-tar", "archive bytes")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"images":["docker.io/library/app:1"]`) ||
		string(rt.loaded) != "archive bytes" || rt.gotNS != "k8s.io" {
		t.Errorf("load: %d %s", rec.Code, rec.Body)
	}
	if rec := load("application/json", "{}"); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("json: %d", rec.Code)
	}
	if rec := load("application/octet-stream", "bad"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad archive: %d %s", rec.Code, rec.Body)
	}
}

func TestArchiveName(t *testing.T) {
	for in, want := range map[string]string{
		"docker.io/library/nginx:1.27":        "nginx_1.27.tar",
		"ghcr.io/acme/app@sha256:abc":         "app_sha256_abc.tar",
		"import-2026-11-22@sha256:0123456789": "import-2026-11-22_sha256_0123456789.tar",
		"..":                                  "image.tar",
	} {
		if got := archiveName([]string{in}); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
