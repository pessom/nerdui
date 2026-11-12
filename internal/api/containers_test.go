// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/runtime"
)

func TestListContainers(t *testing.T) {
	rt := &fakeRuntime{containers: []runtime.Container{{
		ID: "abc", Name: "web", Namespace: "default", State: runtime.StateRunning,
		CreatedAt: time.Unix(100, 0).UTC(), Ports: []runtime.Port{{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}},
		Compose: &runtime.ComposeRef{Project: "media", Service: "web"}, Labels: map[string]string{},
	}}}
	var got struct {
		Namespace  string
		Containers []runtime.Container
	}
	rec := do(t, newRouter(rt), "/api/v1/environments/local/containers", &got)
	if rec.Code != http.StatusOK || got.Namespace != "default" || len(got.Containers) != 1 || rt.gotNS != "default" {
		t.Fatalf("%d %s (ns %q)", rec.Code, rec.Body, rt.gotNS)
	}
	c := got.Containers[0]
	if c.Name != "web" || c.Compose.Project != "media" || c.Ports[0].HostPort != 8080 {
		t.Errorf("container: %+v", c)
	}

	do(t, newRouter(rt), "/api/v1/environments/local/containers?namespace=k8s.io", &got)
	if rt.gotNS != "k8s.io" {
		t.Errorf("namespace not passed: %q", rt.gotNS)
	}
	if rec := do(t, newRouter(rt), "/api/v1/environments/local/containers?namespace=../x", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid namespace: %d", rec.Code)
	}
	if rec := do(t, newRouter(&fakeRuntime{err: runtime.ErrNotConnected}), "/api/v1/environments/local/containers", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("containerd down: %d", rec.Code)
	}
}

func TestListContainersEmptyIsArray(t *testing.T) {
	rec := do(t, newRouter(&fakeRuntime{containers: []runtime.Container{}}), "/api/v1/environments/local/containers", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"containers\":[],\"namespace\":\"default\"}\n" {
		t.Errorf("%d %q", rec.Code, rec.Body)
	}
}

func TestInspectContainer(t *testing.T) {
	rt := &fakeRuntime{inspect: map[string]any{"web": map[string]any{"Id": "abc", "Name": "web"}}}
	var got map[string]any
	rec := do(t, newRouter(rt), "/api/v1/environments/local/containers/web/inspect?namespace=k8s.io&size=true", &got)
	if rec.Code != http.StatusOK || got["Id"] != "abc" || rt.gotNS != "k8s.io" || !rt.gotSize {
		t.Fatalf("%d %s (ns %q, size %v)", rec.Code, rec.Body, rt.gotNS, rt.gotSize)
	}
	for path, want := range map[string]int{
		"/api/v1/environments/local/containers/nope/inspect":         http.StatusNotFound,
		"/api/v1/environments/local/containers/web/inspect?size=yes": http.StatusBadRequest,
	} {
		if rec := do(t, newRouter(rt), path, nil); rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
	if rec := do(t, newRouter(&fakeRuntime{err: runtime.ErrNotConnected}), "/api/v1/environments/local/containers/web/inspect", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("containerd down: %d", rec.Code)
	}
}
