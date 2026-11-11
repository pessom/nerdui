package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"
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
