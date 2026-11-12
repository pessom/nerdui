// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

func historyRouter(t *testing.T, hist EventHistory) http.Handler {
	t.Helper()
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{
			Envs: environment.NewRegistry(environment.Local(&fakeRuntime{}, "default")),
			Jobs: jobs.NewManager(), EventHistory: hist,
		}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	return r
}

func TestEventHistory(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	base := time.Date(2027, 3, 1, 10, 0, 0, 0, time.UTC)
	for i := range 5 {
		ns := "default"
		if i == 4 {
			ns = "k8s.io"
		}
		if err := st.Write().InsertEvent(context.Background(), db.InsertEventParams{
			Environment: "local", Ts: base.Add(time.Duration(i) * time.Second).UnixMilli(), Namespace: ns,
			Topic: "/tasks/exit", Kind: "task", Action: "exit", ObjectID: fmt.Sprint("c", i), ObjectName: "web",
			Attributes: `{"exitStatus":"0"}`,
		}); err != nil {
			t.Fatal(err)
		}
	}
	h := historyRouter(t, st.Read())
	get := func(path string) (int, struct {
		Events    []HistoryEvent
		More      bool
		Recording bool
	}) {
		rec := send(t, h, http.MethodGet, "/api/v1/environments/local/events"+path, "")
		var out struct {
			Events    []HistoryEvent
			More      bool
			Recording bool
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	code, page := get("?limit=2")
	if code != http.StatusOK || len(page.Events) != 2 || !page.More || !page.Recording || page.Events[0].ObjectID != "c3" {
		t.Fatalf("default namespace page: %d %+v", code, page)
	}
	if !page.Events[0].Timestamp.Equal(base.Add(3*time.Second)) || page.Events[0].Attributes["exitStatus"] != "0" {
		t.Errorf("event %+v", page.Events[0])
	}
	_, next := get(fmt.Sprintf("?limit=2&before=%d", page.Events[1].ID))
	if len(next.Events) != 2 || next.More || next.Events[1].ObjectID != "c0" {
		t.Errorf("next page %+v", next)
	}
	if _, all := get("?namespace=*"); len(all.Events) != 5 {
		t.Errorf("all namespaces: %d", len(all.Events))
	}
	if _, obj := get("?namespace=*&object=c4"); len(obj.Events) != 1 {
		t.Errorf("object filter: %d", len(obj.Events))
	}
	if code, _ := get("?limit=0"); code != http.StatusBadRequest {
		t.Errorf("limit 0: %d", code)
	}
	if code, _ := get("?before=x"); code != http.StatusBadRequest {
		t.Errorf("bad cursor: %d", code)
	}

	// Recording disabled.
	rec := send(t, historyRouter(t, nil), http.MethodGet, "/api/v1/environments/local/events", "")
	if rec.Code != http.StatusOK || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("disabled: %d %s", rec.Code, rec.Body)
	}
	var off struct{ Recording bool }
	_ = json.Unmarshal(rec.Body.Bytes(), &off)
	if off.Recording {
		t.Error("recording reported while disabled")
	}
}
