// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"testing"
	"time"
)

func TestImageCache(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	c := newImageCache()
	c.now = func() time.Time { return now }

	c.put("default", "sha256:a", imageDetails{Size: 1})
	c.put("other", "sha256:a", imageDetails{Size: 2})
	if d, ok := c.get("default", "sha256:a"); !ok || d.Size != 1 {
		t.Fatalf("get: %v %v", d, ok)
	}
	if _, ok := c.get("default", "sha256:b"); ok {
		t.Error("unknown digest found")
	}

	now = now.Add(imageDetailsTTL + time.Second)
	if _, ok := c.get("default", "sha256:a"); ok {
		t.Error("expired entry used")
	}

	c.put("default", "sha256:a", imageDetails{Size: 3})
	c.reset("default")
	if _, ok := c.get("default", "sha256:a"); ok {
		t.Error("entry survived reset")
	}
	if d, ok := c.get("other", "sha256:a"); ok || d.Size != 0 {
		// "other" was put before the clock moved past the TTL.
		t.Errorf("other namespace: %v %v", d, ok)
	}
	c.put("other", "sha256:a", imageDetails{Size: 4})
	c.reset("default")
	if d, ok := c.get("other", "sha256:a"); !ok || d.Size != 4 {
		t.Errorf("reset touched another namespace: %v %v", d, ok)
	}
}
