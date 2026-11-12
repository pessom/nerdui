// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package runtime wraps the containerd client and nerdctl libraries.
//
// All container operations performed by nerdui go through this package so that
// containers created from the UI stay fully compatible with the nerdctl CLI.
package runtime

import (
	"context"
	"fmt"
	"runtime/debug"
	"slices"
	"sync"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/errdefs"
	"github.com/containerd/errdefs/pkg/errgrpc"
	"github.com/containerd/nerdctl/v2/pkg/config"
	"github.com/containerd/nerdctl/v2/pkg/labels"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// DefaultAddress is the default containerd socket path.
	DefaultAddress = "/run/containerd/containerd.sock"
	// DefaultNamespace is the default containerd namespace used by nerdctl.
	DefaultNamespace = "default"

	nerdctlModule = "github.com/containerd/nerdctl/v2"
)

// ErrNotConnected is returned when containerd cannot be reached.
// It satisfies errdefs.IsUnavailable, so the API maps it to 503 (ADR-0006).
var ErrNotConnected error = notConnected{}

type notConnected struct{}

func (notConnected) Error() string { return "containerd is unavailable" }

// Unavailable marks the error class for errdefs.IsUnavailable.
func (notConnected) Unavailable() {}

// Versions describes the versions of the underlying runtime components.
type Versions struct {
	Containerd         string `json:"containerd"`
	ContainerdRevision string `json:"containerdRevision"`
	// ContainerdSupported is false for versions outside SupportedContainerdMinors (ADR-0012).
	ContainerdSupported bool `json:"containerdSupported"`
	// SupportedContainerd are the containerd minors this build supports (ADR-0017).
	SupportedContainerd []string      `json:"supportedContainerd"`
	NerdctlLibrary      string        `json:"nerdctlLibrary"`
	Nerdctl             NerdctlStatus `json:"nerdctl"`
}

// Client is a lazily connected containerd client.
type Client struct {
	address   string
	namespace string
	nerdctl   *nerdctlChecker
	// nerdctlConfig is nerdctl.toml (data_root, CNI paths, cgroup manager…); Address and
	// Namespace are overridden per call.
	nerdctlConfig config.Config

	mu     sync.Mutex
	client *containerd.Client
}

// New creates a client. The connection is established on first use, so nerdui
// can start even when containerd is temporarily unavailable.
func New(address, namespace string, opts ...Option) *Client {
	c := &Client{
		address: address, namespace: namespace,
		nerdctl:       newNerdctlChecker("nerdctl", false),
		nerdctlConfig: *config.New(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Option configures a Client.
type Option func(*Client)

// WithNerdctl sets the host nerdctl binary and whether its version check is skipped
// (ADR-0003, ADR-0011).
func WithNerdctl(path string, skipVersionCheck bool) Option {
	return func(c *Client) { c.nerdctl = newNerdctlChecker(path, skipVersionCheck) }
}

// WithNerdctlConfig sets nerdctl's configuration (see LoadNerdctlConfig), so data_root,
// CNI paths and other settings match the nerdctl CLI on the host.
func WithNerdctlConfig(cfg *config.Config) Option {
	return func(c *Client) {
		if cfg != nil {
			c.nerdctlConfig = *cfg
		}
	}
}

// Nerdctl reports the host nerdctl binary and whether mutations are allowed (ADR-0011).
func (c *Client) Nerdctl(ctx context.Context) NerdctlStatus { return c.nerdctl.status(ctx) }

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
	v := Versions{
		NerdctlLibrary: NerdctlLibraryVersion(), Nerdctl: c.Nerdctl(ctx),
		SupportedContainerd: SupportedContainerdMinors(),
	}

	cl, err := c.conn()
	if err != nil {
		return v, err
	}
	sv, err := cl.Version(ctx)
	if err != nil {
		return v, fmt.Errorf("get containerd version: %w", classify(err))
	}
	v.Containerd = sv.Version
	v.ContainerdRevision = sv.Revision
	v.ContainerdSupported = ContainerdSupported(sv.Version)
	return v, nil
}

// System returns system-level operations (versions, namespaces).
func (c *Client) System() System { return c }

// Namespaces lists containerd namespaces in sorted order.
func (c *Client) Namespaces(ctx context.Context) ([]string, error) {
	cl, err := c.conn()
	if err != nil {
		return nil, err
	}
	ns, err := cl.NamespaceService().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", classify(err))
	}
	slices.Sort(ns)
	return ns, nil
}

// classify converts gRPC errors to containerd errdefs and marks connectivity problems
// (daemon down, socket missing, no answer in time) as ErrNotConnected. Some containerd
// services already return native errdefs, so both forms are recognized.
func classify(err error) error {
	if err == nil {
		return nil
	}
	native := err
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Unavailable, codes.DeadlineExceeded:
			return fmt.Errorf("%w: %s", ErrNotConnected, s.Message())
		}
		// Only real gRPC statuses are converted: ToNative turns any other error into
		// ErrUnknown and would hide its class.
		native = errgrpc.ToNative(err)
	}
	if errdefs.IsUnavailable(native) || errdefs.IsDeadlineExceeded(native) {
		return fmt.Errorf("%w: %v", ErrNotConnected, native) //nolint:errorlint // classified as unavailable
	}
	return native
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
