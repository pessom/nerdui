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

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/runtime"
)

func TestContainerCommand(t *testing.T) {
	h := newRouter(writable())
	rec := send(t, h, http.MethodPost, "/api/v1/environments/local/containers/command",
		`{"image":"nginx:1.27","name":"web","ports":[{"hostPort":8080,"containerPort":80}],"start":true}`)
	var got struct{ Command string }
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || got.Command != "nerdctl run -d --name=web --publish=8080:80/tcp nginx:1.27" {
		t.Fatalf("command: %d %s", rec.Code, rec.Body)
	}
	rec = send(t, h, http.MethodPost, "/api/v1/environments/local/containers/command", `{"image":"alpine","start":false}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Command != "nerdctl create alpine" {
		t.Errorf("without start: %s", rec.Body)
	}
	if rec := send(t, h, http.MethodPost, "/api/v1/environments/local/containers/command", `{"image":"--privileged"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid spec: %d", rec.Code)
	}
}

func TestCreateContainerJob(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}, createID: strings.Repeat("a", 64)}
	srv, m := jobsServer(t, f, authz.AdminOnly{})
	url := srv.URL + "/api/v1/environments/local/containers?namespace=k8s.io"
	if resp, _ := postJSON(t, url, `{"image":"alpine","name":"-bad"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid name: %d", resp.StatusCode)
	}
	resp, body := postJSON(t, url, `{"image":"alpine","name":"app","env":[{"name":"A","value":"1"}],"start":true}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	job := body["job"].(map[string]any)
	if job["title"] != "Create container app" || job["kind"] != "container.create" {
		t.Errorf("job %v", job)
	}
	id := job["id"].(string)
	deadline := time.Now().Add(5 * time.Second)
	for j, _ := m.Get(id); !j.Done(); j, _ = m.Get(id) {
		if time.Now().After(deadline) {
			t.Fatal("create job did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	j, _ := m.Get(id)
	if j.Status != "succeeded" || j.Result["containerId"] != f.createID {
		t.Errorf("finished job %+v", j)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.created) != 1 || f.created[0].Name != "app" || !f.createStart || f.created[0].Env[0].Value != "1" {
		t.Errorf("created %+v start %v", f.created, f.createStart)
	}
}

func TestRecreateContainerJob(t *testing.T) {
	cur := runtime.ContainerSpec{
		ID: strings.Repeat("b", 64), Running: true,
		Spec: runtime.CreateSpec{Image: "nginx:1", Name: "web"},
	}
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}, createID: strings.Repeat("c", 64), spec: cur}
	srv, m := jobsServer(t, f, authz.AdminOnly{})

	var got runtime.ContainerSpec
	if rec := do(t, newRouter(f), "/api/v1/environments/local/containers/web/spec", &got); rec.Code != http.StatusOK || got.Spec.Name != "web" || got.Unsupported == nil {
		t.Fatalf("spec: %d %s", rec.Code, rec.Body)
	}

	url := srv.URL + "/api/v1/environments/local/containers/web/recreate"
	resp, body := postJSON(t, url, `{"image":"nginx:1","name":"web","env":[{"name":"A","value":"2"}],"pull":true}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("recreate: %d %v", resp.StatusCode, body)
	}
	job := body["job"].(map[string]any)
	if job["title"] != "Update container web" || job["kind"] != "container.recreate" {
		t.Errorf("job %v", job)
	}
	id := job["id"].(string)
	deadline := time.Now().Add(5 * time.Second)
	for j, _ := m.Get(id); !j.Done(); j, _ = m.Get(id) {
		if time.Now().After(deadline) {
			t.Fatal("recreate job did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if j, _ := m.Get(id); j.Status != "succeeded" || j.Result["containerId"] != f.createID {
		t.Errorf("finished job %+v", j)
	}
	f.mu.Lock()
	if len(f.recreated) != 1 || f.recreated[0].Env[0].Value != "2" || !f.recreatePull {
		t.Errorf("recreated %+v pull %v", f.recreated, f.recreatePull)
	}
	f.mu.Unlock()

	// Refused: settings that would be lost, stack containers, invalid specs.
	f.spec.Unsupported = []string{"sysctls net.ipv4.ip_forward"}
	if resp, body := postJSON(t, url, `{"image":"nginx:1"}`); resp.StatusCode != http.StatusConflict || !strings.Contains(fmt.Sprint(body), "sysctls") {
		t.Errorf("unsupported: %d %v", resp.StatusCode, body)
	}
	f.spec.Unsupported, f.spec.Stack = nil, "media"
	if resp, _ := postJSON(t, url, `{"image":"nginx:1"}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("stack container: %d", resp.StatusCode)
	}
	if resp, _ := postJSON(t, url, `{"image":"--privileged"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid spec: %d", resp.StatusCode)
	}
}
