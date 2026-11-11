// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package environment

import (
	"errors"
	"testing"

	"github.com/pessom/nerdui/internal/runtime"
)

type rt struct{ closed *int }

func (r rt) System() runtime.System { return nil }
func (r rt) Close() error           { *r.closed++; return nil }

var _ runtime.Runtime = rt{}

func TestRegistry(t *testing.T) {
	closed := 0
	local := Local(rt{&closed}, "default")
	other := &Environment{ID: "nas", Name: "NAS", Kind: "agent", Runtime: rt{&closed}}
	r := NewRegistry(local, other)

	if e, ok := r.Get(LocalID); !ok || e != local || e.DefaultNamespace != "default" {
		t.Errorf("Get(local) = %+v, %v", e, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Error("missing environment found")
	}
	if l := r.List(); len(l) != 2 || l[0] != local || l[1] != other {
		t.Errorf("List order = %+v", l)
	}
	if err := r.Add(&Environment{ID: "nas"}); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate: %v", err)
	}
	if err := r.Close(); err != nil || closed != 2 {
		t.Errorf("Close: %v, closed=%d", err, closed)
	}
}

func TestNewRegistryPanicsOnDuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic on duplicate IDs")
		}
	}()
	closed := 0
	NewRegistry(Local(rt{&closed}, "default"), Local(rt{&closed}, "default"))
}
