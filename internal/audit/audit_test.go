// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package audit

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/store"
)

func TestAuthAndRetention(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Date(2027, 3, 1, 10, 0, 0, 0, time.UTC)
	rec := &Recorder{Store: st.Write(), Retention: 24 * time.Hour, Now: func() time.Time { return now }}

	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	r.RemoteAddr = "203.0.113.7:4000"
	rec.Auth(r, ActionLogin, "mallory", false, "invalid username or password")
	now = now.Add(48 * time.Hour)
	rec.Auth(r, ActionLogin, "alice", true, "")

	got, err := rec.List(ctx, Filter{})
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[1].User != "mallory" || got[1].Result != ResultFailed || got[1].ClientIP != "203.0.113.7" || got[0].Result != ResultOK {
		t.Errorf("entries %+v", got)
	}
	if got, _ := rec.List(ctx, Filter{Since: now.Add(-time.Hour)}); len(got) != 1 {
		t.Errorf("since %+v", got)
	}

	// Run prunes once at start.
	ctx2, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { rec.Run(ctx2); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := rec.List(ctx, Filter{}); len(got) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if got, _ := rec.List(ctx, Filter{}); len(got) != 1 || got[0].User != "alice" {
		t.Errorf("after prune %+v", got)
	}
}

func TestWriteCSVEscapesFormulas(t *testing.T) {
	var b bytes.Buffer
	if err := WriteCSV(&b, []Entry{{At: time.Unix(0, 0).UTC(), User: "=cmd|' /C calc'!A0", Object: "-2+3", Result: ResultOK}}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&b).ReadAll()
	if err != nil || len(rows) != 2 || rows[1][1] != "'=cmd|' /C calc'!A0" || rows[1][9] != "'-2+3" {
		t.Errorf("%q %v", rows, err)
	}
}

// TestAPIKeyName records which API key made a request (#80); the search finds it.
func TestAPIKeyName(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rec := &Recorder{Store: st.Write()}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/environments/local/stacks/app/up", nil)
	rec.Audit(r, authz.AuditRecord{
		Subject: authz.Subject{Username: "admin", APIKey: true, KeyName: "ci (nerdui_abc123…)"},
		Action:  authz.StacksDeploy, Status: http.StatusAccepted,
	})
	got, err := rec.List(ctx, Filter{Query: "nerdui_abc123"})
	if err != nil || len(got) != 1 || !got[0].APIKey || got[0].APIKeyName != "ci (nerdui_abc123…)" {
		t.Fatalf("%+v %v", got, err)
	}
}
