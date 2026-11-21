// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sort"
)

// RedeployOptions modify Redeploy.
type RedeployOptions struct {
	// Services limits the redeploy to these services; empty means all.
	Services []string
	// RemoveOrphans removes containers of services no longer in the compose file.
	RemoveOrphans bool
}

// Redeploy pulls the images of a stack and recreates the services whose image changed, then
// brings the stack (or the given services) up (ADR-0042).
//
// `nerdctl compose up` recreates a container only when the service's configuration changed; a
// tag that now points to a newer image is not a change (nerdctl 2.3 and 2.4), and
// `--force-recreate` would also recreate the dependencies of the services. So the images'
// digests are compared before and after the pull, and the containers of changed services are
// removed: `up` creates them again and leaves every other container as it is. Containers
// created by nerdctl 2.4+ also record their image digest, which catches images pulled earlier
// without recreating.
func Redeploy(ctx context.Context, rt Runtime, namespace, project, dir string, opts RedeployOptions, out io.Writer) error {
	compose := rt.Compose()
	scope := func(c Container) bool {
		return c.Compose != nil && c.Compose.Project == project &&
			(len(opts.Services) == 0 || slices.Contains(opts.Services, c.Compose.Service))
	}
	before, err := imageDigests(ctx, rt, namespace)
	if err != nil {
		return err
	}
	if err := compose.Run(ctx, namespace, project, dir, ComposePull, ComposeOptions{Services: opts.Services}, out); err != nil {
		return err
	}
	after, err := imageDigests(ctx, rt, namespace)
	if err != nil {
		return err
	}
	list, err := rt.Containers().List(ctx, namespace)
	if err != nil {
		return err
	}
	var stale []Container
	changed := map[string]bool{}
	for _, c := range list {
		if !scope(c) {
			continue
		}
		now := after[c.Image]
		if now == "" {
			continue // built locally or not pulled: compose decides
		}
		recorded := c.Labels[imageDigestLabel]
		if before[c.Image] != now || recorded != "" && recorded != now {
			stale = append(stale, c)
			changed[c.Compose.Service] = true
		}
	}
	if len(stale) > 0 {
		names := make([]string, 0, len(changed))
		for s := range changed {
			names = append(names, s)
		}
		sort.Strings(names)
		_, _ = fmt.Fprintf(out, "\r\nNew images for %v: recreating their containers\r\n", names)
		for _, c := range stale {
			if err := rt.Containers().Do(ctx, namespace, c.ID, ActionRemove, ActionOptions{Force: true}); err != nil {
				return fmt.Errorf("remove %s: %w", c.Name, err)
			}
		}
	} else {
		_, _ = fmt.Fprintf(out, "\r\nNo new images\r\n")
	}
	return compose.Run(ctx, namespace, project, dir, ComposeUp, ComposeOptions{Services: opts.Services, RemoveOrphans: opts.RemoveOrphans}, out)
}

// imageDigests maps image names of a namespace to their target digests.
func imageDigests(ctx context.Context, rt Runtime, namespace string) (map[string]string, error) {
	list, err := rt.Images().List(ctx, namespace)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(list))
	for _, img := range list {
		out[img.Name] = img.Digest
	}
	return out, nil
}
