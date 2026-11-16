// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"

	composecli "github.com/compose-spec/compose-go/v2/cli"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/identifiers"
)

// Compose reads compose projects like `nerdctl compose` (ADR-0008): compose-go, at the version
// the linked nerdctl uses, with the project options of nerdctl's composer.New — includes,
// extends, profiles and .env interpolation behave the same.
type Compose interface {
	// Services returns the services of a project that run by default (no profile), loading
	// file from dir under the project name.
	Services(ctx context.Context, namespace, project, dir, file string) ([]string, error)
}

// Compose returns compose operations of the local containerd.
func (c *Client) Compose() Compose { return clientCompose{} }

type clientCompose struct{}

// The package composer is not imported: its test dependencies reference a nerdctl-internal
// module (mod/tigron) that `go mod tidy` cannot resolve outside nerdctl's repository.
func (clientCompose) Services(ctx context.Context, _, project, dir, file string) ([]string, error) {
	if err := identifiers.ValidateDockerCompat(project); err != nil {
		return nil, fmt.Errorf("%w: project name: %w", errdefs.ErrInvalidArgument, err)
	}
	// The options of nerdctl's composer.New, in its order.
	opts, err := composecli.NewProjectOptions([]string{file},
		composecli.WithOsEnv,
		composecli.WithWorkingDirectory(dir),
		composecli.WithConfigFileEnv,
		composecli.WithDefaultConfigPath,
		composecli.WithEnvFiles(),
		composecli.WithDotEnv,
		composecli.WithName(project),
		composecli.WithProfiles(nil),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	p, err := opts.LoadProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	return p.ServiceNames(), nil
}
