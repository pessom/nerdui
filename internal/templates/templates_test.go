// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package templates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store"
)

func TestParseEnv(t *testing.T) {
	got := ParseEnv(`# Port on the host
PORT=8080

# Ignored: a blank line follows

# Superuser
# password
DB_PASSWORD=
export TAG="1.2 # not a comment"
NAME='single' # inline
PORT=9090
not a variable
API_KEY=x # key
`)
	want := []Variable{
		{Name: "PORT", Default: "8080", Description: "Port on the host"},
		{Name: "DB_PASSWORD", Description: "Superuser password", Secret: true},
		{Name: "TAG", Default: "1.2 # not a comment"},
		{Name: "NAME", Default: "single"},
		{Name: "API_KEY", Default: "x", Secret: true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// TestBuiltinTemplates loads every built-in template like `nerdctl compose` with its defaults
// and a value for each required variable.
func TestBuiltinTemplates(t *testing.T) {
	list := Builtin()
	if len(list) < 10 {
		t.Fatalf("%d built-in templates", len(list))
	}
	compose := runtime.New("", "").Compose()
	for _, tpl := range list {
		if tpl.Name == "" || tpl.Description == "" || len(tpl.Categories) == 0 || tpl.Documentation == "" {
			t.Errorf("%s: incomplete metadata %+v", tpl.ID, tpl.Summary)
		}
		var env strings.Builder
		for _, v := range tpl.Variables {
			val := v.Default
			if val == "" {
				val = "value-" + strings.ToLower(v.Name)
			}
			env.WriteString(v.Name + "=" + val + "\n")
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(tpl.Compose), 0o600); err != nil {
			t.Fatal(err)
		}
		svcs, err := compose.Validate(context.Background(), "t-"+tpl.ID, dir, "compose.yaml", nil, []byte(env.String()))
		if err != nil || len(svcs) == 0 {
			t.Errorf("%s: %v %v", tpl.ID, svcs, err)
		}
		// Every ${VAR} of the compose file is described in .env.example.
		for _, v := range tpl.Variables {
			if !strings.Contains(tpl.Compose, "${"+v.Name) {
				t.Errorf("%s: %s is not used", tpl.ID, v.Name)
			}
		}
	}
	if _, err := (&Service{}).Get(context.Background(), SourceBuiltin, "../builtin"); !errdefs.IsNotFound(err) {
		t.Errorf("path: %v", err)
	}
}

func newService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Service{Store: st.Write(), Registry: &Registries{}}
}

func TestRegistries(t *testing.T) {
	ctx := context.Background()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/cat/registry.json":
			_, _ = w.Write([]byte(`{"name":"demo","templates":[
				{"id":"web","name":"Web","description":"d","tags":["web"],"icon_url":"icons/web.svg",
				 "compose_url":"web/compose.yaml","env_url":"/cat/web/.env.example"},
				{"id":"inline","compose":"services:\n  a:\n    image: busybox\n","env":"# x\nX=1\n"},
				{"id":"nofiles"},
				{"id":"web","name":"duplicate"}]}`))
		case "/cat/web/compose.yaml":
			_, _ = w.Write([]byte("services:\n  web:\n    image: nginx:${TAG}\n"))
		case "/cat/web/.env.example":
			_, _ = w.Write([]byte("# Image tag\nTAG=1.27\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s := newService(t)
	reg := srv.URL + "/cat/registry.json"
	if _, err := s.SetRegistryURLs(ctx, []string{"ftp://x"}); !errdefs.IsInvalidArgument(err) {
		t.Errorf("ftp: %v", err)
	}
	urls, err := s.SetRegistryURLs(ctx, []string{" " + reg, reg, "", srv.URL + "/missing.json"})
	if err != nil || len(urls) != 2 {
		t.Fatalf("set: %v %v", urls, err)
	}
	list, errs, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 1 || errs[0].Source != srv.URL+"/missing.json" {
		t.Errorf("errors %+v", errs)
	}
	var fromReg []Summary
	for _, x := range list {
		if x.Source == reg {
			fromReg = append(fromReg, x)
		}
	}
	if len(fromReg) != 2 || fromReg[0].Logo != srv.URL+"/cat/icons/web.svg" || fromReg[0].Categories[0] != "web" || fromReg[1].Name != "inline" {
		t.Errorf("registry templates %+v", fromReg)
	}
	tpl, err := s.Get(ctx, reg, "web")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tpl.Compose, "nginx:${TAG}") || len(tpl.Variables) != 1 || tpl.Variables[0].Description != "Image tag" {
		t.Errorf("web %+v", tpl)
	}
	before := hits.Load()
	if _, _, err := s.List(ctx); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != before+1 { // the registry is cached, the missing one asked again
		t.Errorf("hits %d -> %d", before, hits.Load())
	}
	s.Registry.Now = func() time.Time { return time.Now().Add(time.Hour) }
	if _, err := s.Get(ctx, reg, "inline"); err != nil {
		t.Errorf("inline: %v", err)
	}
	if _, err := s.Get(ctx, reg, "nope"); !errdefs.IsNotFound(err) {
		t.Errorf("nope: %v", err)
	}
	if _, err := s.Get(ctx, "https://not-configured.example/registry.json", "web"); !errdefs.IsNotFound(err) {
		t.Errorf("unknown source: %v", err)
	}
}

func TestCustomTemplates(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	in := Input{Name: " shop ", Description: "the shop", Compose: "services:\n  a:\n    image: busybox\n", Env: "# Password\nDB_PASSWORD=\n"}
	tpl, err := s.Create(ctx, in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Name != "shop" || tpl.Source != SourceCustom || len(tpl.Variables) != 1 || !tpl.Variables[0].Secret {
		t.Errorf("created %+v", tpl)
	}
	if _, err := s.Create(ctx, in, "admin"); !errdefs.IsAlreadyExists(err) {
		t.Errorf("duplicate: %v", err)
	}
	for _, bad := range []Input{{Name: "", Compose: "x"}, {Name: "a"}, {Name: "a", Compose: strings.Repeat("x", MaxFile+1)}} {
		if _, err := s.Create(ctx, bad, ""); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%+v: %v", bad.Name, err)
		}
	}
	in.Name = "shop2"
	if tpl, err = s.Update(ctx, tpl.ID, in); err != nil || tpl.Name != "shop2" {
		t.Errorf("update %+v %v", tpl, err)
	}
	got, err := s.Get(ctx, SourceCustom, tpl.ID)
	if err != nil || got.Compose != in.Compose {
		t.Errorf("get %+v %v", got, err)
	}
	list, _, err := s.List(ctx)
	if err != nil || !slices.ContainsFunc(list, func(x Summary) bool { return x.Source == SourceCustom && x.Name == "shop2" }) {
		t.Errorf("list %v", err)
	}
	if err := s.Delete(ctx, tpl.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{tpl.ID, "x"} {
		if err := s.Delete(ctx, id); !errdefs.IsNotFound(err) {
			t.Errorf("delete %s: %v", id, err)
		}
		if _, err := s.Get(ctx, SourceCustom, id); !errdefs.IsNotFound(err) {
			t.Errorf("get %s: %v", id, err)
		}
	}
}
