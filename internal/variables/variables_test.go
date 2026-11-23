// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package variables

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/secretbox"
	"github.com/pessom/nerdui/internal/store"
)

func newService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	box, err := secretbox.LoadOrCreate(filepath.Join(t.TempDir(), secretbox.FileName))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	return &Service{Store: st.Write(), Box: box, now: func() time.Time { return now }}, st
}

func ptr(s string) *string { return &s }

func TestVariables(t *testing.T) {
	ctx := context.Background()
	s, st := newService(t)

	put := func(stack, name string, in Input) Variable {
		t.Helper()
		v, err := s.Put(ctx, stack, name, in, "admin")
		if err != nil {
			t.Fatalf("put %s/%s: %v", stack, name, err)
		}
		return v
	}
	put(Global, "TZ", Input{Value: ptr("Europe/Moscow")})
	put(Global, "DB_PASSWORD", Input{Value: ptr("global-pw"), Secret: true})
	v := put("shop", "DB_PASSWORD", Input{Value: ptr("shop-pw"), Secret: true})
	if v.Value != "" || !v.Secret || v.UpdatedBy != "admin" {
		t.Errorf("secret returned: %+v", v)
	}
	put("shop", "TAG", Input{Value: ptr("1.2")})

	// Values are encrypted in the database.
	rows, err := st.Read().ListVariables(ctx, "shop")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if strings.Contains(r.Value, "shop-pw") || strings.Contains(r.Value, "1.2") {
			t.Errorf("plain value stored: %+v", r)
		}
	}

	list, err := s.List(ctx, "shop")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "DB_PASSWORD" || list[0].Value != "" || list[1].Value != "1.2" {
		t.Errorf("list %+v", list)
	}

	got, err := s.Values(ctx, "shop")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"TZ": "Europe/Moscow", "DB_PASSWORD": "shop-pw", "TAG": "1.2"}
	if !maps.Equal(got, want) {
		t.Errorf("values %v", got)
	}
	if got, _ := s.Values(ctx, "blog"); !maps.Equal(got, map[string]string{"TZ": "Europe/Moscow", "DB_PASSWORD": "global-pw"}) {
		t.Errorf("blog values %v", got)
	}

	// Without a value the stored one is kept; a secret needs its value to become visible.
	put("shop", "TAG", Input{Secret: true})
	if got, _ := s.Values(ctx, "shop"); got["TAG"] != "1.2" {
		t.Errorf("kept value %q", got["TAG"])
	}
	if _, err := s.Put(ctx, "shop", "TAG", Input{}, "admin"); !errdefs.IsInvalidArgument(err) {
		t.Errorf("unhide without value: %v", err)
	}
	if _, err := s.Put(ctx, "shop", "NEW", Input{}, "admin"); !errdefs.IsInvalidArgument(err) {
		t.Errorf("new without value: %v", err)
	}
	if v := put("shop", "TAG", Input{Value: ptr("1.3")}); v.Value != "1.3" || v.Secret {
		t.Errorf("unhidden %+v", v)
	}

	if err := s.Delete(ctx, "shop", "TAG"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "shop", "TAG"); !errdefs.IsNotFound(err) {
		t.Errorf("delete again: %v", err)
	}
	if err := s.DeleteStack(ctx, "shop"); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(ctx, "shop"); len(list) != 0 {
		t.Errorf("after DeleteStack: %+v", list)
	}
	if err := s.DeleteStack(ctx, Global); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(ctx, Global); len(list) != 2 {
		t.Errorf("globals removed: %+v", list)
	}

	// Another key cannot read the values.
	other, err := secretbox.LoadOrCreate(filepath.Join(t.TempDir(), secretbox.FileName))
	if err != nil {
		t.Fatal(err)
	}
	s.Box = other
	if _, err := s.Values(ctx, "blog"); !errors.Is(err, secretbox.ErrDecrypt) {
		t.Errorf("other key: %v", err)
	}
}

func TestPutValidation(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	for _, name := range []string{"", "1A", "A-B", "A B", "PATH", "path", "LD_PRELOAD", "COMPOSE_FILE", "NERDCTL_TOML", "DOCKER_CONFIG", strings.Repeat("A", 129)} {
		if _, err := s.Put(ctx, Global, name, Input{Value: ptr("x")}, ""); !errdefs.IsInvalidArgument(err) {
			t.Errorf("name %q: %v", name, err)
		}
	}
	for _, value := range []string{strings.Repeat("x", MaxValue+1), "a\x00b", "\xff"} {
		if _, err := s.Put(ctx, Global, "A", Input{Value: ptr(value)}, ""); !errdefs.IsInvalidArgument(err) {
			t.Errorf("value %q…: %v", value[:min(len(value), 8)], err)
		}
	}
	for _, name := range []string{"A", "_x", "db_password", "MY_PATH"} {
		if _, err := s.Put(ctx, Global, name, Input{Value: ptr("multi\nline")}, ""); err != nil {
			t.Errorf("name %q: %v", name, err)
		}
	}
}
