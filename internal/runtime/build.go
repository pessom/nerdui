// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"
)

// BuildOptions are the options of an image build (#45, ADR-0061).
type BuildOptions struct {
	// Tags name the image; at least one is required.
	Tags []string
	// Dockerfile is the path of the Dockerfile relative to the context; empty means
	// "Dockerfile".
	Dockerfile string
	// Target is the stage to build; empty means the last one.
	Target string
	// Args are the build arguments (--build-arg).
	Args map[string]string
	// NoCache builds every step again.
	NoCache bool
	// Pull pulls newer base images.
	Pull bool
	// Platform builds for one platform; empty means the host's.
	Platform string
}

// BuilderStatus tells whether BuildKit can build in a namespace.
type BuilderStatus struct {
	Available bool `json:"available"`
	// Host is the BuildKit address used.
	Host string `json:"host,omitempty"`
	// Error explains why no BuildKit is available.
	Error string `json:"error,omitempty"`
}

var (
	buildArgName  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	buildTarget   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	buildPlatform = regexp.MustCompile(`^[a-z0-9]+(/[a-z0-9]+){1,2}$`)
)

// BuildArgs returns the arguments of `nerdctl build` for a context directory. Every value is
// validated, and the context comes last after "--", so no value is read as a flag.
func BuildArgs(dir string, opts BuildOptions) ([]string, error) {
	if len(opts.Tags) == 0 {
		return nil, fmt.Errorf("%w: at least one tag is required", errdefs.ErrInvalidArgument)
	}
	args := []string{"build", "--progress=plain"}
	for _, t := range opts.Tags {
		if err := validRef(t); err != nil {
			return nil, err
		}
		args = append(args, "--tag="+t)
	}
	if f := opts.Dockerfile; f != "" {
		if !filepath.IsLocal(f) {
			return nil, fmt.Errorf("%w: the Dockerfile must be inside the context: %q", errdefs.ErrInvalidArgument, f)
		}
		args = append(args, "--file="+filepath.Join(dir, f))
	}
	if t := opts.Target; t != "" {
		if !buildTarget.MatchString(t) {
			return nil, fmt.Errorf("%w: invalid target %q", errdefs.ErrInvalidArgument, t)
		}
		args = append(args, "--target="+t)
	}
	for _, k := range slices.Sorted(maps.Keys(opts.Args)) {
		if !buildArgName.MatchString(k) {
			return nil, fmt.Errorf("%w: invalid build argument name %q", errdefs.ErrInvalidArgument, k)
		}
		args = append(args, "--build-arg="+k+"="+opts.Args[k])
	}
	if p := opts.Platform; p != "" {
		if !buildPlatform.MatchString(p) {
			return nil, fmt.Errorf("%w: invalid platform %q", errdefs.ErrInvalidArgument, p)
		}
		args = append(args, "--platform="+p)
	}
	if opts.NoCache {
		args = append(args, "--no-cache")
	}
	if opts.Pull {
		args = append(args, "--pull")
	}
	return append(args, "--", dir), nil
}

// ShownBuildArgs returns build arguments for the log without the context directory and with
// the values of build arguments hidden: they may carry tokens.
func ShownBuildArgs(args []string) []string {
	shown := slices.Clone(args)
	if n := len(shown); n >= 2 && shown[n-2] == "--" {
		shown = shown[:n-2]
	}
	for i, a := range shown {
		if arg, ok := strings.CutPrefix(a, "--build-arg="); ok {
			name, _, _ := strings.Cut(arg, "=")
			shown[i] = "--build-arg=" + name + "=…"
		}
	}
	return shown
}

// Build builds an image from the context directory dir with `nerdctl build`, like the CLI of
// this nerdui build (ADR-0003, ADR-0061): nerdctl finds BuildKit and loads the result into
// the namespace. Output goes to out; cancelling ctx stops the build.
func (ci clientImages) Build(ctx context.Context, namespace, dir string, opts BuildOptions, out io.Writer) error {
	defer ci.c.images.reset(namespace) // new image (ADR-0029)
	args, err := BuildArgs(dir, opts)
	if err != nil {
		return err
	}
	cfg := ci.c.nerdctlFor(namespace)
	bin, global := ci.c.nerdctlInvocation(cfg)
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("%w: nerdctl binary %q not found", errdefs.ErrFailedPrecondition, bin)
	}
	_, _ = fmt.Fprintf(out, "\x1b[1m$ nerdctl %s .\x1b[0m\r\n", strings.Join(ShownBuildArgs(args), " "))
	cmd := exec.CommandContext(ctx, bin, slices.Concat(global, args)...) //nolint:gosec // G204: validated arguments, no shell (ADR-0061)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = out, out
	killGroupOnCancel(cmd)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("nerdctl build failed (exit code %d)", exit.ExitCode())
		}
		return fmt.Errorf("nerdctl build: %w", err)
	}
	return nil
}

// buildkitCandidates are the addresses nerdctl tries for a rootful containerd, in its order
// (buildkitutil.getBuildkitHostCandidates).
func buildkitCandidates(namespace string) []string {
	if h := os.Getenv("BUILDKIT_HOST"); h != "" {
		return []string{h}
	}
	return []string{
		"unix:///run/buildkit-" + namespace + "/buildkitd.sock",
		"unix:///run/buildkit-default/buildkitd.sock",
		"unix:///run/buildkit/buildkitd.sock",
	}
}

// BuilderStatus reports whether a BuildKit daemon answers for namespace, the way
// `nerdctl build` looks for one.
func (ci clientImages) BuilderStatus(ctx context.Context, namespace string) BuilderStatus {
	buildctl, err := exec.LookPath("buildctl")
	if err != nil {
		return BuilderStatus{Error: "buildctl is not installed: building images needs BuildKit (buildctl and buildkitd)"}
	}
	var last string
	for _, host := range buildkitCandidates(namespace) {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, err := exec.CommandContext(pctx, buildctl, "--addr="+host, "debug", "workers").CombinedOutput() //nolint:gosec // G204: fixed arguments
		cancel()
		if err == nil {
			return BuilderStatus{Available: true, Host: host}
		}
		last = strings.TrimSpace(string(out))
		if last == "" {
			last = err.Error()
		}
	}
	return BuilderStatus{Error: "buildkitd is not running (" + last + ")"}
}
