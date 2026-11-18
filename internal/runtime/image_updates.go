// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"

	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/imgutil/dockerconfigresolver"
	"github.com/containerd/nerdctl/v2/pkg/referenceutil"
)

// RemoteDigest returns the digest the reference's tag points to in its registry (#53,
// ADR-0030): one HEAD request of the manifest, which Docker Hub does not count as a pull.
// nerdctl's registry configuration applies, as for Pull: hosts.d, the credentials of the user
// running nerdui and insecure registries.
func (ci clientImages) RemoteDigest(ctx context.Context, ref string) (string, error) {
	parsed, err := referenceutil.Parse(ref)
	if err != nil || ref == "" {
		return "", fmt.Errorf("%w: invalid image reference %q", errdefs.ErrInvalidArgument, ref)
	}
	if parsed.Digest != "" {
		return "", fmt.Errorf("%w: %s is pinned to a digest", errdefs.ErrInvalidArgument, ref)
	}
	cfg := ci.c.nerdctlFor(ci.c.namespace)
	resolve := func(opts ...dockerconfigresolver.Opt) (string, error) {
		opts = append(opts, dockerconfigresolver.WithHostsDirs(cfg.HostsDir))
		r, err := dockerconfigresolver.New(ctx, parsed.Domain, opts...)
		if err != nil {
			return "", err
		}
		_, desc, err := r.Resolve(ctx, parsed.String())
		if err != nil {
			return "", err
		}
		return desc.Digest.String(), nil
	}
	d, err := resolve(dockerconfigresolver.WithSkipVerifyCerts(cfg.InsecureRegistry))
	if err != nil && cfg.InsecureRegistry {
		// Like nerdctl pull --insecure-registry: fall back to plain HTTP.
		d, err = resolve(dockerconfigresolver.WithSkipVerifyCerts(true), dockerconfigresolver.WithPlainHTTP(true))
	}
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", ref, err)
	}
	return d, nil
}
