// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package runtime wraps the containerd client and nerdctl libraries.
//
// All container operations performed by nerdui go through this package so that
// containers created from the UI stay fully compatible with the nerdctl CLI.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/nerdctl/v2/pkg/labels"
)

const (
	// DefaultAddress is the default containerd socket path.
	DefaultAddress = "/run/containerd/containerd.sock"
	// DefaultNamespace is the default containerd namespace used by nerdctl.
	DefaultNamespace = "default"

	nerdctlModule = "github.com/containerd/nerdctl/v2"
)

// ErrNotConnected is returned when a containerd client cannot be created.
var ErrNotConnected = errors.New("containerd is not connected")

// Versions describes the versions of the underlying runtime components.
type Versions struct {
	Containerd         string `json:"containerd"`
	ContainerdRevision string `json:"containerdRevision"`
	NerdctlLibrary     string `json:"nerdctlLibrary"`
}

// Client is a lazily connected containerd client.
type Client struct {
	address   string
	namespace string

	mu     sync.Mutex
	client *containerd.Client
}

// New creates a client. The connection is established on first use, so nerdui
// can start even when containerd is temporarily unavailable.
func New(address, namespace string) *Client {
	return &Client{address: address, namespace: namespace}
}

// Address returns the containerd socket address.
func (c *Client) Address() string { return c.address }

// Namespace returns the default containerd namespace.
func (c *Client) Namespace() string { return c.namespace }

func (c *Client) conn() (*containerd.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		return c.client, nil
	}
	cl, err := containerd.New(c.address, containerd.WithDefaultNamespace(c.namespace))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrNotConnected, c.address, err)
	}
	c.client = cl
	return cl, nil
}

// Versions returns containerd server and nerdctl library versions.
func (c *Client) Versions(ctx context.Context) (Versions, error) {
	v := Versions{NerdctlLibrary: NerdctlLibraryVersion()}

	cl, err := c.conn()
	if err != nil {
		return v, err
	}
	sv, err := cl.Version(ctx)
	if err != nil {
		return v, fmt.Errorf("get containerd version: %w", err)
	}
	v.Containerd = sv.Version
	v.ContainerdRevision = sv.Revision
	return v, nil
}

// Close closes the underlying connection, if any.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil
	}
	err := c.client.Close()
	c.client = nil
	return err
}

// ContainerName returns the nerdctl container name stored in labels.
func ContainerName(lbls map[string]string) string {
	return lbls[labels.Name]
}

// NerdctlLibraryVersion returns the version of the nerdctl module linked into the binary.
func NerdctlLibraryVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range bi.Deps {
		if dep.Path == nerdctlModule {
			if dep.Replace != nil {
				return dep.Replace.Version
			}
			return dep.Version
		}
	}
	return "unknown"
}
