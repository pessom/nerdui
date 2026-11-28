// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
)

// Images, volumes and networks of every namespace carry their namespace (#276).
func TestListAllNamespaces(t *testing.T) {
	rt := &fakeRuntime{
		ns:       []string{"default", "k8s.io"},
		images:   []runtime.Image{{Name: "nginx:1"}},
		volumes:  []runtime.Volume{{Name: "data"}},
		networks: []runtime.Network{{Name: "bridge"}},
	}
	for _, kind := range []string{"images", "volumes", "networks"} {
		t.Run(kind, func(t *testing.T) {
			rec := do(t, newRouter(rt), "/api/v1/environments/local/"+kind+"?namespace=*", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			var got map[string]json.RawMessage
			_ = json.Unmarshal(rec.Body.Bytes(), &got)
			var items []struct{ Namespace string }
			_ = json.Unmarshal(got[kind], &items)
			if string(got["namespace"]) != `"*"` || len(items) != 2 || items[0].Namespace != "default" || items[1].Namespace != "k8s.io" {
				t.Errorf("%s", rec.Body)
			}

			rec = do(t, newRouter(rt), "/api/v1/environments/local/"+kind+"?namespace=k8s.io", nil)
			_ = json.Unmarshal(rec.Body.Bytes(), &got)
			_ = json.Unmarshal(got[kind], &items)
			if len(items) != 1 || items[0].Namespace != "k8s.io" {
				t.Errorf("one namespace: %s", rec.Body)
			}

			down := &fakeRuntime{err: runtime.ErrNotConnected}
			if rec := do(t, newRouter(down), "/api/v1/environments/local/"+kind+"?namespace=*", nil); rec.Code != http.StatusServiceUnavailable {
				t.Errorf("containerd down: %d", rec.Code)
			}
		})
	}
}

// A stack definition repeats in every namespace; with all of them only deployed ones show.
func TestDeployedStacks(t *testing.T) {
	list := []stacks.Stack{
		{Name: "app", Namespace: "default"},
		{Name: "app", Namespace: "dev", Containers: 2},
		{Name: "idle", Namespace: "default"},
		{Name: "idle", Namespace: "dev"},
		{Name: "both", Namespace: "default", Containers: 1},
		{Name: "both", Namespace: "dev", Containers: 1},
	}
	var got []string
	for _, s := range deployedStacks(list, "default") {
		got = append(got, s.Namespace+"/"+s.Name)
	}
	want := []string{"dev/app", "default/idle", "default/both", "dev/both"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
