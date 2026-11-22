// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestVolumeFiles(t *testing.T) {
	rt := writable()
	h := newRouter(rt)
	base := "/api/v1/environments/local/volumes/data/files"

	rec := send(t, h, http.MethodGet, base+"?namespace=k8s.io&path=/", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"app.conf"`) || rt.gotNS != "k8s.io" {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodGet, base+"?path=/app.conf", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("list a file: %d", rec.Code)
	}
	if rec := send(t, h, http.MethodGet, "/api/v1/environments/local/volumes/other/files?path=/", ""); rec.Code != http.StatusNotFound {
		t.Errorf("missing volume: %d", rec.Code)
	}
	if rec := send(t, h, http.MethodGet, base+"/preview?path=/app.conf", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"content":"listen 80\n"`) {
		t.Errorf("preview: %d %s", rec.Code, rec.Body)
	}

	rec = send(t, h, http.MethodGet, base+"/download?path=/app.conf", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "listen 80\n" || rec.Header().Get("Content-Length") != "10" ||
		rec.Header().Get("Content-Type") != "application/octet-stream" ||
		rec.Header().Get("Content-Disposition") != `attachment; filename="app conf\".txt"` {
		t.Errorf("download file: %d %v %q", rec.Code, rec.Header(), rec.Body)
	}
	rec = send(t, h, http.MethodGet, base+"/download?path=/", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "GZ" || rec.Header().Get("Content-Type") != "application/gzip" ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "data.tar.gz") {
		t.Errorf("download volume: %d %v", rec.Code, rec.Header())
	}
}
