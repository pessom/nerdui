// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/composer"
	"github.com/containerd/nerdctl/v2/pkg/composer/serviceparser"
)

// Compose reads compose projects with nerdctl's composer (ADR-0008), so a stack is loaded
// exactly as `nerdctl compose` loads it: includes, extends, profiles, .env interpolation.
type Compose interface {
	// Services returns the services of a project that run by default (no profile), loading
	// file from dir under the project name.
	Services(ctx context.Context, namespace, project, dir, file string) ([]string, error)
}

// Compose returns compose operations of the local containerd.
func (c *Client) Compose() Compose { return clientCompose{c} }

type clientCompose struct{ c *Client }

// errReadOnly guards the composer callbacks: loading a project must not create anything.
var errReadOnly = errors.New("read-only project load")

func (cc clientCompose) Services(_ context.Context, namespace, project, dir, file string) ([]string, error) {
	cl, err := cc.c.conn()
	if err != nil {
		return nil, err
	}
	cfg := cc.c.nerdctlFor(namespace)
	bin, global := cc.c.nerdctlInvocation(cfg)
	comp, err := composer.New(composer.Options{
		Project:          project,
		ProjectDirectory: dir,
		ConfigPaths:      []string{file},
		NerdctlCmd:       bin,
		NerdctlArgs:      global,
		NetworkExists:    func(string) (bool, error) { return false, errReadOnly },
		VolumeExists:     func(string) (bool, error) { return false, errReadOnly },
		EnsureImage: func(context.Context, string, string, string, *serviceparser.Service, bool) error {
			return errReadOnly
		},
	}, cl, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	names, err := comp.ServiceNames()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	return names, nil
}
