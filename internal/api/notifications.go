// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/notify"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// NotificationStore stores notification channels (#86); *db.Queries implements it.
type NotificationStore interface {
	ListNotificationChannels(ctx context.Context) ([]db.NotificationChannel, error)
	GetNotificationChannel(ctx context.Context, id int64) (db.NotificationChannel, error)
	CreateNotificationChannel(ctx context.Context, arg db.CreateNotificationChannelParams) (db.NotificationChannel, error)
	UpdateNotificationChannel(ctx context.Context, arg db.UpdateNotificationChannelParams) (db.NotificationChannel, error)
	DeleteNotificationChannel(ctx context.Context, id int64) (int64, error)
}

// NotificationChannelView is a channel without its secrets. Keep in sync with api/openapi.yaml.
type NotificationChannelView struct {
	ID     int64         `json:"id"`
	Name   string        `json:"name"`
	Type   string        `json:"type"`
	Config notify.Config `json:"config"`
	// Secrets names the write-only config fields that are set.
	Secrets    []string   `json:"secrets"`
	Events     []string   `json:"events"`
	Enabled    bool       `json:"enabled"`
	LastSentAt *time.Time `json:"lastSentAt,omitempty"`
	LastError  string     `json:"lastError,omitempty"`
}

// NotificationChannelInput creates or replaces a channel. Empty secrets keep the stored ones
// while the type is unchanged. Keep in sync with api/openapi.yaml.
type NotificationChannelInput struct {
	Name    string        `json:"name"`
	Type    string        `json:"type"`
	Config  notify.Config `json:"config"`
	Events  []string      `json:"events"`
	Enabled bool          `json:"enabled"`
}

// NotificationTestInput sends a test through a channel's settings before they are saved; with
// an ID, empty secrets come from that stored channel.
type NotificationTestInput struct {
	NotificationChannelInput
	ID int64 `json:"id,omitempty"`
}

func (h *Handler) notificationRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/notifications/channels", g.Protect(authz.SettingsRead, h.listNotificationChannels))
	r.Method(http.MethodPost, "/notifications/channels", g.Protect(authz.SettingsWrite, h.createNotificationChannel))
	r.Method(http.MethodPut, "/notifications/channels/{id}", g.Protect(authz.SettingsWrite, h.updateNotificationChannel))
	r.Method(http.MethodDelete, "/notifications/channels/{id}", g.Protect(authz.SettingsWrite, h.deleteNotificationChannel))
	r.Method(http.MethodPost, "/notifications/test", g.Protect(authz.SettingsWrite, h.testNotificationChannel))
}

func (h *Handler) notificationsDisabled(w http.ResponseWriter) bool {
	if h.Notifications == nil || h.Notifier == nil {
		httpjson.WriteError(w, http.StatusNotImplemented, httpjson.CodeUnavailable, "notifications are not available")
		return true
	}
	return false
}

func channelView(ch notify.Channel) NotificationChannelView {
	cfg, secrets := ch.Config.Masked(ch.Type)
	v := NotificationChannelView{
		ID: ch.ID, Name: ch.Name, Type: ch.Type, Config: cfg, Secrets: secrets,
		Events: ch.Events, Enabled: ch.Enabled, LastError: ch.LastError,
	}
	if v.Events == nil {
		v.Events = []string{}
	}
	if !ch.LastSentAt.IsZero() {
		t := ch.LastSentAt
		v.LastSentAt = &t
	}
	return v
}

// GET /api/v1/notifications/channels
func (h *Handler) listNotificationChannels(w http.ResponseWriter, r *http.Request) {
	if h.notificationsDisabled(w) {
		return
	}
	rows, err := h.Notifications.ListNotificationChannels(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	out := make([]NotificationChannelView, 0, len(rows))
	for _, row := range rows {
		ch, err := notify.FromRow(row)
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		out = append(out, channelView(ch))
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"channels": out, "kinds": notify.Kinds})
}

// channelFrom validates the input; stored, when given, supplies empty secrets of the same type.
func channelFrom(in NotificationChannelInput, stored *db.NotificationChannel) (notify.Channel, error) {
	ch := notify.Channel{Name: in.Name, Type: in.Type, Config: in.Config, Events: in.Events, Enabled: in.Enabled}
	if ch.Events == nil {
		ch.Events = []string{}
	}
	if stored != nil {
		ch.ID = stored.ID
		if stored.Type == in.Type {
			old, err := notify.FromRow(*stored)
			if err != nil {
				return notify.Channel{}, err
			}
			ch.Config.KeepSecrets(in.Type, old.Config)
		}
	}
	return ch, ch.Validate()
}

func (h *Handler) channelParam(w http.ResponseWriter, r *http.Request) (db.NotificationChannel, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: notification channel %q not found", errdefs.ErrNotFound, chi.URLParam(r, "id")))
		return db.NotificationChannel{}, false
	}
	row, err := h.Notifications.GetNotificationChannel(r.Context(), id)
	if store.IsNotFound(err) {
		err = fmt.Errorf("%w: notification channel %d not found", errdefs.ErrNotFound, id)
	}
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return db.NotificationChannel{}, false
	}
	return row, true
}

func encodeChannel(ch notify.Channel) (cfg, events string) {
	c, _ := json.Marshal(ch.Config) //nolint:gosec // G117: stored in the database on purpose; the API returns Config.Masked
	e, _ := json.Marshal(ch.Events)
	return string(c), string(e)
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (h *Handler) writeChannel(w http.ResponseWriter, r *http.Request, status int, row db.NotificationChannel, err error) {
	if store.IsUniqueViolation(err) {
		httpjson.WriteError(w, http.StatusConflict, httpjson.CodeConflict, "a notification channel with this name exists")
		return
	}
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	ch, err := notify.FromRow(row)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, status, channelView(ch))
}

// POST /api/v1/notifications/channels  NotificationChannelInput
func (h *Handler) createNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if h.notificationsDisabled(w) {
		return
	}
	var in NotificationChannelInput
	if !httpjson.Decode(w, r, &in) {
		return
	}
	ch, err := channelFrom(in, nil)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	cfg, events := encodeChannel(ch)
	row, err := h.Notifications.CreateNotificationChannel(r.Context(), db.CreateNotificationChannelParams{
		Name: ch.Name, Type: ch.Type, Config: cfg, Events: events, Enabled: boolInt(ch.Enabled), Now: time.Now().Unix(),
	})
	if err == nil {
		h.log().Info("notification channel created", "channel", ch.Name, "type", ch.Type)
	}
	h.writeChannel(w, r, http.StatusCreated, row, err)
}

// PUT /api/v1/notifications/channels/{id}  NotificationChannelInput
func (h *Handler) updateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if h.notificationsDisabled(w) {
		return
	}
	stored, ok := h.channelParam(w, r)
	if !ok {
		return
	}
	var in NotificationChannelInput
	if !httpjson.Decode(w, r, &in) {
		return
	}
	ch, err := channelFrom(in, &stored)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	cfg, events := encodeChannel(ch)
	row, err := h.Notifications.UpdateNotificationChannel(r.Context(), db.UpdateNotificationChannelParams{
		ID: stored.ID, Name: ch.Name, Type: ch.Type, Config: cfg, Events: events, Enabled: boolInt(ch.Enabled), Now: time.Now().Unix(),
	})
	if err == nil {
		h.log().Info("notification channel saved", "channel", ch.Name, "type", ch.Type)
	}
	h.writeChannel(w, r, http.StatusOK, row, err)
}

// DELETE /api/v1/notifications/channels/{id}
func (h *Handler) deleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if h.notificationsDisabled(w) {
		return
	}
	stored, ok := h.channelParam(w, r)
	if !ok {
		return
	}
	if _, err := h.Notifications.DeleteNotificationChannel(r.Context(), stored.ID); err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	h.log().Info("notification channel deleted", "channel", stored.Name)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/notifications/test  NotificationTestInput
//
// Sends a test message now. A delivery failure is 502 with the channel's error.
func (h *Handler) testNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if h.notificationsDisabled(w) {
		return
	}
	var in NotificationTestInput
	if !httpjson.Decode(w, r, &in) {
		return
	}
	var stored *db.NotificationChannel
	if in.ID != 0 {
		row, err := h.Notifications.GetNotificationChannel(r.Context(), in.ID)
		if store.IsNotFound(err) {
			err = fmt.Errorf("%w: notification channel %d not found", errdefs.ErrNotFound, in.ID)
		}
		if err != nil {
			httpjson.WriteErr(w, r, h.Logger, err)
			return
		}
		stored = &row
	}
	ch, err := channelFrom(in.NotificationChannelInput, stored)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	if err := h.Notifier.Send(r.Context(), ch, notify.TestEvent()); err != nil {
		httpjson.WriteError(w, http.StatusBadGateway, httpjson.CodeUnavailable, "the channel did not accept the message: "+err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]bool{"sent": true})
}
