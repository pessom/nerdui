// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"strings"
	"sync"
	"time"
)

// imageDetailsTTL bounds how long details of an image digest are reused (ADR-0029). Changes made
// through nerdui (pull, remove, prune) reset the namespace at once; this covers the CLI.
const imageDetailsTTL = 5 * time.Minute

// imageDetails is what describing an image costs: platforms and sizes. It depends only on the
// target digest and on what of it is in the store, not on the image name.
type imageDetails struct {
	Platforms   []string
	ContentSize int64
	Size        int64
	at          time.Time
}

// imageCache keeps imageDetails per namespace and digest (#129: listing 300 images read the
// manifests of every platform of every image on each request).
type imageCache struct {
	mu      sync.Mutex
	entries map[string]imageDetails
	now     func() time.Time
}

func newImageCache() *imageCache {
	return &imageCache{entries: map[string]imageDetails{}, now: time.Now}
}

func imageCacheKey(namespace, digest string) string { return namespace + "\x00" + digest }

func (c *imageCache) get(namespace, digest string) (imageDetails, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.entries[imageCacheKey(namespace, digest)]
	if !ok || c.now().Sub(d.at) > imageDetailsTTL {
		return imageDetails{}, false
	}
	return d, true
}

func (c *imageCache) put(namespace, digest string, d imageDetails) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d.at = c.now()
	c.entries[imageCacheKey(namespace, digest)] = d
}

// reset forgets a namespace after nerdui changed its images or content.
func (c *imageCache) reset(namespace string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := namespace + "\x00"
	for k := range c.entries {
		if strings.HasPrefix(k, prefix) {
			delete(c.entries, k)
		}
	}
}
