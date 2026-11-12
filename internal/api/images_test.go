// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/authz"
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

func TestTagImage(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}, imageErr: map[string]error{
		"missing": fmt.Errorf("%w: no such image: missing", errdefs.ErrNotFound),
	}}
	h := newRouter(f)
	rec := send(t, h, http.MethodPost, "/api/v1/environments/local/images/tag?namespace=k8s.io", `{"source":"nginx:1.27","target":"reg.local/nginx:1.27"}`)
	if rec.Code != http.StatusNoContent || len(f.tags) != 1 || f.tags[0] != [3]string{"k8s.io", "nginx:1.27", "reg.local/nginx:1.27"} {
		t.Fatalf("tag: %d %s %v", rec.Code, rec.Body, f.tags)
	}
	if rec := send(t, h, http.MethodPost, "/api/v1/environments/local/images/tag", `{"source":"missing","target":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("missing source: %d %s", rec.Code, rec.Body)
	}
}

func TestRemoveImages(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}, imageErr: map[string]error{
		"nginx:1.27": fmt.Errorf("%w: conflict: unable to delete nginx:1.27 (must be forced) - image is being used by stopped container abc", errdefs.ErrConflict),
	}}
	h := newRouter(f)
	rec := send(t, h, http.MethodPost, "/api/v1/environments/local/images/remove", `{"references":["alpine:3","nginx:1.27"],"force":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
	var got struct{ Results []RemoveImagesResult }
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Results) != 2 || !got.Results[0].OK || got.Results[1].OK || got.Results[1].Error.Code != "conflict" {
		t.Errorf("results %+v", got.Results)
	}
	if len(f.removed) != 2 || f.removed[0] != "default/alpine:3/true" {
		t.Errorf("removed %v", f.removed)
	}
	if rec := send(t, h, http.MethodPost, "/api/v1/environments/local/images/remove", `{"references":[]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty: %d", rec.Code)
	}
	ro := newRouter(&fakeRuntime{nerdctl: runtime.NerdctlStatus{Version: "2.3.5", Required: "2.4"}})
	if rec := send(t, ro, http.MethodPost, "/api/v1/environments/local/images/remove", `{"references":["a"]}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("read-only: %d", rec.Code)
	}
}

func TestPushJob(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}}
	srv, m := jobsServer(t, f, authz.AdminOnly{})
	resp, body := postJSON(t, srv.URL+"/api/v1/environments/local/images/push", `{"reference":"reg.local/app:1","allPlatforms":true}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("push: %d %v", resp.StatusCode, body)
	}
	job := body["job"].(map[string]any)
	if job["kind"] != "image.push" || job["title"] != "Push reg.local/app:1" {
		t.Errorf("job %v", job)
	}
	id := job["id"].(string)
	deadline := time.Now().Add(5 * time.Second)
	for j, _ := m.Get(id); !j.Done(); j, _ = m.Get(id) {
		if time.Now().After(deadline) {
			t.Fatal("push job did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pushes) != 1 || !f.pushes[0].opts.AllPlatforms || f.pushes[0].ref != "reg.local/app:1" {
		t.Errorf("pushes %+v", f.pushes)
	}
}
