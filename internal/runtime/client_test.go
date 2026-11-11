// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestContainerName(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"nil labels", nil, ""},
		{"no name label", map[string]string{"foo": "bar"}, ""},
		{"nerdctl name", map[string]string{"nerdctl/name": "web"}, "web"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainerName(tt.labels); got != tt.want {
				t.Errorf("ContainerName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionsWithoutSocket(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "missing.sock"), DefaultNamespace)
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	v, err := c.Versions(ctx)
	if err == nil {
		t.Fatal("expected error for missing socket")
	}
	if v.Containerd != "" {
		t.Errorf("Containerd = %q, want empty", v.Containerd)
	}
	if v.NerdctlLibrary == "" {
		t.Error("NerdctlLibrary must always be set")
	}
	if !errdefs.IsUnavailable(err) {
		t.Errorf("missing socket error %v is not classified as unavailable", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	if _, err := c.Namespaces(ctx2); !errdefs.IsUnavailable(err) {
		t.Errorf("Namespaces without socket: %v", err)
	}
}

func TestAccessors(t *testing.T) {
	c := New("/run/x.sock", "k8s.io")
	if c.Address() != "/run/x.sock" || c.Namespace() != "k8s.io" {
		t.Errorf("got %q %q", c.Address(), c.Namespace())
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close on unconnected client: %v", err)
	}
}

func TestClassify(t *testing.T) {
	for _, tt := range []struct {
		name        string
		err         error
		unavailable bool
		notFound    bool
	}{
		{"grpc unavailable", status.Error(codes.Unavailable, "connection refused"), true, false},
		{"grpc deadline", status.Error(codes.DeadlineExceeded, "waiting for connections"), true, false},
		{"native deadline", fmt.Errorf("list: %w", context.DeadlineExceeded), true, false},
		{"native unavailable", errdefs.ErrUnavailable, true, false},
		{"grpc not found", status.Error(codes.NotFound, "no such container"), false, true},
		{"plain", errors.New("boom"), false, false},
	} {
		got := classify(tt.err)
		if errdefs.IsUnavailable(got) != tt.unavailable || errdefs.IsNotFound(got) != tt.notFound {
			t.Errorf("%s: classify(%v) = %v", tt.name, tt.err, got)
		}
	}
	if classify(nil) != nil {
		t.Error("classify(nil) != nil")
	}
}
