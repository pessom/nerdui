// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// SupportedContainerdMinors are the containerd releases nerdui supports and tests:
// the latest LTS and the latest release (ADR-0012). Update together with the CI matrix.
var SupportedContainerdMinors = []string{"2.3", "2.4"}

// ContainerdSupported reports whether a containerd version (e.g. "v2.3.7") is supported.
func ContainerdSupported(version string) bool {
	mm, ok := majorMinor(version)
	return ok && slices.Contains(SupportedContainerdMinors, mm)
}

// NerdctlStatus is the result of the nerdctl compatibility check (ADR-0011).
type NerdctlStatus struct {
	// Path is the configured binary (--nerdctl-path).
	Path string `json:"path"`
	// Version is the host binary's version, empty when it could not be determined.
	Version string `json:"version,omitempty"`
	// Required is the major.minor that must match, e.g. "2.4" (the linked packages).
	Required string `json:"required"`
	// Compatible is true when major.minor of Version equals Required.
	Compatible bool `json:"compatible"`
	// CheckSkipped is true with --skip-nerdctl-version-check (unsupported configuration).
	CheckSkipped bool `json:"checkSkipped,omitempty"`
	// Error explains why the binary is missing or its version unknown.
	Error string `json:"error,omitempty"`
}

// Writable reports whether mutating operations are allowed.
func (s NerdctlStatus) Writable() bool { return s.Compatible || s.CheckSkipped }

// nerdctlCheckTTL bounds how long a check result is reused, so upgrading nerdctl on the
// host is picked up without restarting nerdui.
const nerdctlCheckTTL = time.Minute

type nerdctlChecker struct {
	path     string
	skip     bool
	required string
	now      func() time.Time
	run      func(ctx context.Context, path string) (string, error)

	mu      sync.Mutex
	last    NerdctlStatus
	checked time.Time
}

func newNerdctlChecker(path string, skip bool) *nerdctlChecker {
	required, ok := majorMinor(NerdctlLibraryVersion())
	if !ok {
		required = "unknown"
	}
	return &nerdctlChecker{path: path, skip: skip, required: required, now: time.Now, run: runNerdctlVersion}
}

func (c *nerdctlChecker) status(ctx context.Context) NerdctlStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.checked.IsZero() && c.now().Sub(c.checked) < nerdctlCheckTTL {
		return c.last
	}
	st := NerdctlStatus{Path: c.path, Required: c.required, CheckSkipped: c.skip}
	out, err := c.run(ctx, c.path)
	switch {
	case err != nil:
		st.Error = err.Error()
	default:
		if v, ok := parseNerdctlVersion(out); ok {
			st.Version = v
			mm, _ := majorMinor(v)
			st.Compatible = mm == c.required
		} else {
			st.Error = fmt.Sprintf("cannot parse %q", strings.TrimSpace(out))
		}
	}
	c.last, c.checked = st, c.now()
	return st
}

func runNerdctlVersion(ctx context.Context, path string) (string, error) {
	bin, err := exec.LookPath(path)
	if err != nil {
		return "", fmt.Errorf("nerdctl binary %q not found: %w", path, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// `nerdctl --version` does not contact containerd, so it works while the daemon is down.
	out, err := exec.CommandContext(ctx, bin, "--version").Output() //nolint:gosec // G204: operator-configured binary
	if err != nil {
		return "", fmt.Errorf("run %s --version: %w", bin, err)
	}
	return string(out), nil
}

var nerdctlVersionRe = regexp.MustCompile(`(?i)nerdctl version v?(\d+\.\d+\.\d+[^\s]*)`)

// parseNerdctlVersion extracts the version from "nerdctl version 2.4.2".
func parseNerdctlVersion(out string) (string, bool) {
	m := nerdctlVersionRe.FindStringSubmatch(out)
	if m == nil {
		return "", false
	}
	return m[1], true
}

var majorMinorRe = regexp.MustCompile(`^v?(\d+)\.(\d+)(?:\.|$|[-+])`)

// majorMinor returns "2.4" for "v2.4.2", "2.4.0-rc.1" and similar.
func majorMinor(v string) (string, bool) {
	m := majorMinorRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return "", false
	}
	return m[1] + "." + m[2], true
}
