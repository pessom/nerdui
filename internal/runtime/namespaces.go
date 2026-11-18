// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/containerd/containerd/v2/pkg/identifiers"
	"github.com/containerd/errdefs"
)

// CreateNamespace creates a containerd namespace (#195). The name follows containerd's
// identifier rules.
func (c *Client) CreateNamespace(ctx context.Context, name string) error {
	if err := identifiers.Validate(name); err != nil {
		return fmt.Errorf("%w: namespace name: %w", errdefs.ErrInvalidArgument, err)
	}
	cl, err := c.conn()
	if err != nil {
		return err
	}
	if err := cl.NamespaceService().Create(ctx, name, nil); err != nil {
		return fmt.Errorf("create namespace %q: %w", name, classify(err))
	}
	return nil
}

// RemoveNamespace removes an empty namespace (#195). Besides containerd's own check
// (containers, images, content, snapshots, leases), nerdctl's volumes and user-defined networks
// must be gone: containerd does not know about them and they would be left behind.
func (c *Client) RemoveNamespace(ctx context.Context, name string) error {
	if err := identifiers.Validate(name); err != nil {
		return fmt.Errorf("%w: namespace name: %w", errdefs.ErrInvalidArgument, err)
	}
	cl, err := c.conn()
	if err != nil {
		return err
	}
	nss, err := c.Namespaces(ctx)
	if err != nil {
		return err
	}
	if !slices.Contains(nss, name) {
		return fmt.Errorf("%w: namespace %q not found", errdefs.ErrNotFound, name)
	}
	var held []string
	if list, err := c.Containers().List(ctx, name); err != nil {
		return err
	} else if n := len(list); n > 0 {
		held = append(held, plural(n, "container"))
	}
	if list, err := c.Images().List(ctx, name); err != nil {
		return err
	} else if n := len(list); n > 0 {
		held = append(held, plural(n, "image"))
	}
	if list, err := c.Volumes().List(ctx, name, false); err != nil {
		return err
	} else if n := len(list); n > 0 {
		held = append(held, plural(n, "volume"))
	}
	if list, err := c.Networks().List(ctx, name); err == nil {
		n := 0
		for _, nw := range list {
			if nw.Removable {
				n++
			}
		}
		if n > 0 {
			held = append(held, plural(n, "network"))
		}
	}
	if len(held) > 0 {
		return fmt.Errorf("%w: namespace %q still has %s; remove them first",
			errdefs.ErrFailedPrecondition, name, strings.Join(held, ", "))
	}
	if err := cl.NamespaceService().Delete(ctx, name); err != nil {
		return fmt.Errorf("remove namespace %q: %w", name, classify(err))
	}
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
