// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/notify"
	"github.com/pessom/nerdui/internal/store"
)

func TestNotificationChannels(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	status := http.StatusOK
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		bodies = append(bodies, r.URL.Path+" "+r.Header.Get("X-Nerdui-Signature")+" "+string(b))
		w.WriteHeader(status)
	}))
	t.Cleanup(hook.Close)

	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	envs := environment.NewRegistry(environment.Local(&fakeRuntime{}, "default"))
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		(&Handler{
			Envs: envs, Jobs: jobs.NewManager(), Notifications: st.Write(),
			Notifier: &notify.Service{Store: st.Write(), Host: "vps", Client: hook.Client()},
		}).Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})

	body := `{"name":"ops","type":"webhook","config":{"url":"` + hook.URL + `/h","secret":"s3"},"events":["job.failed"],"enabled":true}`
	rec := send(t, r, http.MethodPost, "/api/v1/notifications/channels", body)
	var ch NotificationChannelView
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &ch) != nil {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if ch.Config.Secret != "" || len(ch.Secrets) != 1 || ch.Secrets[0] != "secret" || ch.Config.URL == "" {
		t.Errorf("created %+v", ch)
	}
	if strings.Contains(rec.Body.String(), "s3") {
		t.Errorf("secret returned: %s", rec.Body)
	}
	if rec := send(t, r, http.MethodPost, "/api/v1/notifications/channels", body); rec.Code != http.StatusConflict {
		t.Errorf("duplicate name: %d", rec.Code)
	}
	for _, bad := range []string{
		`{"name":"x","type":"sms","config":{},"events":[]}`,
		`{"name":"x","type":"webhook","config":{"url":"file:///etc/passwd"},"events":[]}`,
		`{"name":"x","type":"webhook","config":{"url":"https://example.com"},"events":["nope"]}`,
	} {
		if rec := send(t, r, http.MethodPost, "/api/v1/notifications/channels", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}

	// Saving without the secret keeps it; the stored channel's test uses it.
	id := "/api/v1/notifications/channels/" + jsonNumber(ch.ID)
	upd := `{"name":"ops2","type":"webhook","config":{"url":"` + hook.URL + `/h"},"events":["job.failed","container.died"],"enabled":true}`
	if rec := send(t, r, http.MethodPut, id, upd); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"secrets":["secret"]`) {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	test := `{"id":` + jsonNumber(ch.ID) + `,"name":"ops2","type":"webhook","config":{"url":"` + hook.URL + `/h"},"events":[],"enabled":true}`
	if rec := send(t, r, http.MethodPost, "/api/v1/notifications/test", test); rec.Code != http.StatusOK {
		t.Fatalf("test: %d %s", rec.Code, rec.Body)
	}
	mu.Lock()
	if len(bodies) != 1 || !strings.Contains(bodies[0], "sha256=") || !strings.Contains(bodies[0], `"kind":"test"`) {
		t.Errorf("test delivery %v", bodies)
	}
	status = http.StatusInternalServerError
	mu.Unlock()
	if rec := send(t, r, http.MethodPost, "/api/v1/notifications/test", test); rec.Code != http.StatusBadGateway {
		t.Errorf("failing channel: %d %s", rec.Code, rec.Body)
	}

	var list struct {
		Channels []NotificationChannelView `json:"channels"`
		Kinds    []string                  `json:"kinds"`
	}
	if rec := do(t, r, "/api/v1/notifications/channels", &list); rec.Code != http.StatusOK || len(list.Channels) != 1 ||
		list.Channels[0].Name != "ops2" || list.Channels[0].LastError == "" || list.Channels[0].LastSentAt == nil || len(list.Kinds) == 0 {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodDelete, id, ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	if rec := send(t, r, http.MethodDelete, id, ""); rec.Code != http.StatusNotFound {
		t.Errorf("delete again: %d", rec.Code)
	}
	if rec := do(t, newRouter(&fakeRuntime{}), "/api/v1/notifications/channels", nil); rec.Code != http.StatusNotImplemented {
		t.Errorf("disabled: %d", rec.Code)
	}
}

func jsonNumber(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
