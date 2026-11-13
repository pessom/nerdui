// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"io"
)

// Runtime is everything nerdui can do with one environment (ADR-0006). It is split into
// small interfaces so handlers depend only on what they use and can be tested with fakes.
// The local implementation is *Client; a remote agent implements the same interface in v2.
//
// New capabilities (Containers, Images, Volumes, Networks, Stacks, Events) are added here
// together with the features that need them.
type Runtime interface {
	System() System
	Containers() Containers
	Images() Images
	Registries() Registries
	Events() EventSource
	Close() error
}

// System covers host-level information of an environment.
type System interface {
	// Versions reports containerd and nerdctl versions.
	Versions(ctx context.Context) (Versions, error)
	// Namespaces lists containerd namespaces.
	Namespaces(ctx context.Context) ([]string, error)
	// Nerdctl reports the nerdctl compatibility check; mutations require Writable().
	Nerdctl(ctx context.Context) NerdctlStatus
	// Info describes the host, runtime components and containerd plugins (#66).
	Info(ctx context.Context) (Info, error)
	// PrunePreview estimates what Prune removes; Prune runs nerdctl's prunes (#52).
	PrunePreview(ctx context.Context, namespace string, allImages bool) (PrunePreview, error)
	Prune(ctx context.Context, namespace string, opts PruneOptions, out io.Writer) error
}

var _ Runtime = (*Client)(nil)
