// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/volbackup"
)

// Backup targets of volumes (#59, ADR-0050) are a setting: they hold S3 credentials.
func (h *Handler) backupTargetRoutes(r chi.Router, g *authz.Guard) {
	if h.VolumeBackups == nil {
		return
	}
	r.Method(http.MethodGet, "/backup-targets", g.Protect(authz.SettingsRead, h.listBackupTargets))
	r.Method(http.MethodPost, "/backup-targets", g.Protect(authz.SettingsWrite, h.createBackupTarget))
	r.Method(http.MethodPut, "/backup-targets/{id}", g.Protect(authz.SettingsWrite, h.updateBackupTarget))
	r.Method(http.MethodDelete, "/backup-targets/{id}", g.Protect(authz.SettingsWrite, h.deleteBackupTarget))
	r.Method(http.MethodPost, "/backup-targets/{id}/test", g.Protect(authz.SettingsWrite, h.testBackupTarget))
}

// volumeBackupRoutes are under /environments/{env}, with a writable runtime. Archives hold the
// volume's contents: listing them needs volumes:write like the volume browser (ADR-0045).
func (h *Handler) volumeBackupRoutes(r chi.Router, g *authz.Guard) {
	if h.VolumeBackups == nil {
		return
	}
	r.Method(http.MethodGet, "/volumes/{name}/backups", g.Protect(authz.VolumesWrite, h.listVolumeBackups))
	r.Method(http.MethodPost, "/volumes/{name}/backups", g.Protect(authz.VolumesWrite, h.backUpVolume))
	r.Method(http.MethodPost, "/volumes/{name}/restore", g.Protect(authz.VolumesWrite, h.restoreVolume))
}

func backupTargetID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: backup target %q not found", errdefs.ErrNotFound, chi.URLParam(r, "id"))
	}
	return id, nil
}

// GET /api/v1/backup-targets
func (h *Handler) listBackupTargets(w http.ResponseWriter, r *http.Request) {
	list, err := h.VolumeBackups.Targets(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"targets": list})
}

// POST /api/v1/backup-targets
func (h *Handler) createBackupTarget(w http.ResponseWriter, r *http.Request) {
	var in volbackup.TargetInput
	if !httpjson.Decode(w, r, &in) {
		return
	}
	t, err := h.VolumeBackups.Create(r.Context(), in)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusCreated, t)
}

// PUT /api/v1/backup-targets/{id}; an empty secretKey keeps the stored one.
func (h *Handler) updateBackupTarget(w http.ResponseWriter, r *http.Request) {
	id, err := backupTargetID(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var in volbackup.TargetInput
	if !httpjson.Decode(w, r, &in) {
		return
	}
	t, err := h.VolumeBackups.Update(r.Context(), id, in)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, t)
}

// DELETE /api/v1/backup-targets/{id}; the archives stay.
func (h *Handler) deleteBackupTarget(w http.ResponseWriter, r *http.Request) {
	id, err := backupTargetID(r)
	if err == nil {
		err = h.VolumeBackups.Delete(r.Context(), id)
	}
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/backup-targets/{id}/test writes, lists and deletes a small object: 200
// {"ok", "error"}.
func (h *Handler) testBackupTarget(w http.ResponseWriter, r *http.Request) {
	id, err := backupTargetID(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if _, err := h.VolumeBackups.Target(r.Context(), id); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	res := map[string]any{"ok": true}
	if err := h.VolumeBackups.Test(r.Context(), id); err != nil {
		res = map[string]any{"ok": false, "error": err.Error()}
	}
	httpjson.Write(w, http.StatusOK, res)
}

// GET /api/v1/environments/{env}/volumes/{name}/backups?namespace=&target=  newest first
func (h *Handler) listVolumeBackups(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	target, err := strconv.ParseInt(r.URL.Query().Get("target"), 10, 64)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: target is required", errdefs.ErrInvalidArgument))
		return
	}
	list, err := h.VolumeBackups.List(r.Context(), target, ns, chi.URLParam(r, "name"))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"backups": list})
}

// volumeBackupRequest starts a backup or restore of a volume.
type volumeBackupRequest struct {
	TargetID       int64  `json:"targetId"`
	Key            string `json:"key"`
	Keep           int    `json:"keep"`
	StopContainers bool   `json:"stopContainers"`
}

// POST /api/v1/environments/{env}/volumes/{name}/backups?namespace=  {"targetId", "keep",
// "stopContainers"}: 202 with the job.
func (h *Handler) backUpVolume(w http.ResponseWriter, r *http.Request) {
	h.volumeBackupJob(w, r, false)
}

// POST /api/v1/environments/{env}/volumes/{name}/restore?namespace=  {"targetId", "key",
// "stopContainers"}: replaces the volume's contents (creating the volume when missing); 202
// with the job.
func (h *Handler) restoreVolume(w http.ResponseWriter, r *http.Request) {
	h.volumeBackupJob(w, r, true)
}

func (h *Handler) volumeBackupJob(w http.ResponseWriter, r *http.Request, restore bool) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	var req volumeBackupRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	if _, err := h.VolumeBackups.Target(r.Context(), req.TargetID); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if restore {
		if err := volbackup.ValidKey(req.Key); err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
	}
	if req.Keep < 0 || req.Keep > volbackup.MaxKeep {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: keep must be 0 to %d", errdefs.ErrInvalidArgument, volbackup.MaxKeep))
		return
	}
	env, name, vb := EnvFrom(r.Context()), chi.URLParam(r, "name"), h.VolumeBackups
	rt, opts := env.Runtime, volbackup.Options{Keep: req.Keep, StopContainers: req.StopContainers}
	spec := jobs.Spec{
		Kind: "volume.backup", Title: "Back up volume " + name, Environment: env.ID, Namespace: ns,
		Resource: fmt.Sprintf("env/%s/ns/%s/volume/%s", env.ID, ns, name),
		Action:   string(authz.VolumesWrite), User: principalName(r),
	}
	fn := func(ctx context.Context, out *jobs.Output) error {
		key, err := vb.Backup(ctx, rt, req.TargetID, ns, name, opts, out)
		if key != "" {
			out.SetResult("key", key)
		}
		return err
	}
	if restore {
		spec.Kind, spec.Title = "volume.restore", "Restore volume "+name
		fn = func(ctx context.Context, out *jobs.Output) error {
			return vb.Restore(ctx, rt, req.TargetID, req.Key, ns, name, opts, out)
		}
	}
	job, err := h.Jobs.Start(spec, fn)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusAccepted, map[string]any{"job": job})
}
