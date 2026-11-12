// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pessom/nerdui/internal/runtime"
)

func TestListImages(t *testing.T) {
	f := &fakeRuntime{images: []runtime.Image{{
		Name: "docker.io/library/nginx:1.27", Repository: "nginx", Tag: "1.27", Digest: "sha256:abc",
		Platforms: []string{"linux/amd64"}, Size: 100, ContentSize: 40, Containers: 2, Labels: map[string]string{},
	}}}
	var got struct {
		Namespace string
		Images    []runtime.Image
	}
	rec := do(t, newRouter(f), "/api/v1/environments/local/images?namespace=k8s.io", &got)
	if rec.Code != http.StatusOK || got.Namespace != "k8s.io" || f.gotNS != "k8s.io" ||
		len(got.Images) != 1 || got.Images[0].Containers != 2 || got.Images[0].Size != 100 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"contentSize":40`) {
		t.Errorf("body %s", rec.Body)
	}

	rec = do(t, newRouter(&fakeRuntime{err: runtime.ErrNotConnected}), "/api/v1/environments/local/images", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("containerd down: %d %s", rec.Code, rec.Body)
	}
	rec = do(t, newRouter(&fakeRuntime{}), "/api/v1/environments/local/images?namespace=bad/ns", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad namespace: %d %s", rec.Code, rec.Body)
	}
}
