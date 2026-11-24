// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/secretbox"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/volbackup"
)

func TestVolumeBackups(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck // test
	box, err := secretbox.LoadOrCreate(filepath.Join(t.TempDir(), secretbox.FileName))
	if err != nil {
		t.Fatal(err)
	}
	rt := writable()
	rt.volumes = []runtime.Volume{{Name: "data", Containers: []runtime.VolumeMounter{}}}
	m := jobs.NewManager()
	defer m.Close()
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{
			Envs: environment.NewRegistry(environment.Local(rt, "default")), Jobs: m,
			VolumeBackups: &volbackup.Service{Store: st.Write(), Box: box},
		}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, func(*http.Request) (authz.Subject, bool) {
			return authz.Subject{Role: authz.RoleAdmin}, true
		}, nil))
	})

	dir := t.TempDir()
	rec := send(t, r, http.MethodPost, "/api/v1/backup-targets", fmt.Sprintf(`{"name":"local","kind":"local","directory":%q}`, dir))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create target: %d %s", rec.Code, rec.Body)
	}
	var target volbackup.Target
	_ = json.Unmarshal(rec.Body.Bytes(), &target)
	if rec := send(t, r, http.MethodPost, fmt.Sprintf("/api/v1/backup-targets/%d/test", target.ID), ""); rec.Code != http.StatusOK || !json.Valid(rec.Body.Bytes()) {
		t.Errorf("test: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPost, "/api/v1/backup-targets/99/test", ""); rec.Code != http.StatusNotFound {
		t.Errorf("test missing: %d", rec.Code)
	}

	base := "/api/v1/environments/local/volumes/data"
	if rec := send(t, r, http.MethodPost, base+"/backups", fmt.Sprintf(`{"targetId":%d}`, target.ID)); rec.Code != http.StatusAccepted {
		t.Fatalf("backup: %d %s", rec.Code, rec.Body)
	}
	if j := waitJob(t, m, "volume.backup"); j.Status != jobs.StatusSucceeded {
		t.Fatalf("backup job %+v", j)
	}
	var list struct{ Backups []volbackup.Archive }
	if rec := do(t, r, fmt.Sprintf("%s/backups?target=%d", base, target.ID), &list); rec.Code != http.StatusOK || len(list.Backups) != 1 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodGet, base+"/backups", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("list without target: %d", rec.Code)
	}

	if rec := send(t, r, http.MethodPost, base+"/restore", fmt.Sprintf(`{"targetId":%d,"key":"../x.tar.gz"}`, target.ID)); rec.Code != http.StatusBadRequest {
		t.Errorf("bad key: %d", rec.Code)
	}
	body := fmt.Sprintf(`{"targetId":%d,"key":%q}`, target.ID, list.Backups[0].Key)
	if rec := send(t, r, http.MethodPost, base+"/restore", body); rec.Code != http.StatusAccepted {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body)
	}
	if j := waitJob(t, m, "volume.restore"); j.Status != jobs.StatusSucceeded {
		t.Fatalf("restore job %+v", j)
	}
	if len(rt.restored) != 1 || rt.restored[0] != "default/data:GZ" {
		t.Errorf("restored %v", rt.restored)
	}
	if rec := send(t, r, http.MethodDelete, fmt.Sprintf("/api/v1/backup-targets/%d", target.ID), ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete target: %d %s", rec.Code, rec.Body)
	}
}
