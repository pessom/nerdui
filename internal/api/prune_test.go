// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/runtime"
)

func TestPrunePreview(t *testing.T) {
	f := &fakeRuntime{}
	rec := send(t, newRouter(f), http.MethodGet, "/api/v1/environments/local/system/prune?namespace=k8s.io&allImages=true", "")
	var p runtime.PrunePreview
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != http.StatusOK || f.gotNS != "k8s.io" || len(p.Containers) != 1 || p.ImagesSize != 100 {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, newRouter(f), http.MethodGet, "/api/v1/environments/local/system/prune?allImages=maybe", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad flag: %d", rec.Code)
	}
}

func TestPruneJob(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}}
	srv, m := jobsServer(t, f, authz.AdminOnly{})
	url := srv.URL + "/api/v1/environments/local/system/prune"
	if resp, _ := postJSON(t, url, `{}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("nothing selected: %d", resp.StatusCode)
	}
	resp, body := postJSON(t, url, `{"containers":true,"images":true,"allImages":true}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("prune: %d %v", resp.StatusCode, body)
	}
	id := body["job"].(map[string]any)["id"].(string)
	deadline := time.Now().Add(5 * time.Second)
	for j, _ := m.Get(id); !j.Done(); j, _ = m.Get(id) {
		if time.Now().After(deadline) {
			t.Fatal("prune job did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.prunes) != 1 || !f.prunes[0].Containers || !f.prunes[0].AllImages || f.prunes[0].Networks {
		t.Errorf("prunes %+v", f.prunes)
	}
	// A caller allowed only images:write does not see the prune job.
	other := jobsServerWith(t, f, onlyActions{authz.JobsRead, authz.ImagesWrite}, m)
	r, _ := http.Get(other.URL + "/api/v1/jobs") //nolint:noctx // test
	var list struct{ Jobs []map[string]any }
	_ = json.NewDecoder(r.Body).Decode(&list)
	_ = r.Body.Close()
	if len(list.Jobs) != 0 {
		t.Errorf("prune job visible without system:prune: %v", list.Jobs)
	}
	if j, ok := m.Get(id); !ok || j.Kind != "system.prune" {
		t.Errorf("job %+v", j)
	}
}
