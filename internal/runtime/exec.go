// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/identifiers"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// ExecOptions describe an interactive process in a container (#15, ADR-0022).
type ExecOptions struct {
	// Cmd is the command; empty runs bash when the image has it, sh otherwise.
	Cmd []string
	// User is a name or UID[:GID] resolved in the container like `nerdctl exec --user`;
	// empty keeps the container's user.
	User    string
	Workdir string
	// Cols and Rows are the initial terminal size.
	Cols, Rows uint16
}

// TermSize is a terminal size change.
type TermSize struct{ Cols, Rows uint16 }

// defaultShell prefers bash and falls back to sh, so one default fits Debian and Alpine.
var defaultShell = []string{"/bin/sh", "-c", "if command -v bash >/dev/null 2>&1; then exec bash; else exec sh; fi"}

// Exec runs an interactive process with a terminal in the running container with the exact
// id, like `nerdctl exec -it`. Input is read from stdin, terminal output written to out, and
// sizes applied from resize. It returns the exit code when the process ends; canceling ctx
// kills the process (SIGKILL), so nothing outlives the client's session.
func (cc clientContainers) Exec(ctx context.Context, namespace, id string, opts ExecOptions,
	stdin io.Reader, out io.Writer, resize <-chan TermSize) (int, error) {
	if err := identifiers.Validate(id); err != nil {
		return -1, fmt.Errorf("%w: container id: %w", errdefs.ErrInvalidArgument, err)
	}
	cl, err := cc.c.conn()
	if err != nil {
		return -1, err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	ct, err := cl.LoadContainer(ctx, id)
	if err != nil {
		return -1, fmt.Errorf("load container %s: %w", id, classify(err))
	}
	task, err := ct.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return -1, fmt.Errorf("%w: container is not running", errdefs.ErrFailedPrecondition)
		}
		return -1, fmt.Errorf("task of %s: %w", id, classify(err))
	}
	if st, err := task.Status(ctx); err == nil && st.Status != containerd.Running {
		return -1, fmt.Errorf("%w: container is %s", errdefs.ErrFailedPrecondition, st.Status)
	}
	pspec, err := execSpec(ctx, cl, ct, opts)
	if err != nil {
		return -1, err
	}

	execID := "nerdui-exec-" + randomHex(8)
	process, err := task.Exec(ctx, execID, pspec, cio.NewCreator(cio.WithStreams(stdin, out, nil), cio.WithTerminal))
	if err != nil {
		return -1, fmt.Errorf("exec in %s: %w", id, classify(err))
	}
	// Cleanup must run even when ctx is canceled.
	cleanup := context.WithoutCancel(ctx)
	defer func() {
		dctx, cancel := context.WithTimeout(cleanup, 10*time.Second)
		defer cancel()
		_, _ = process.Delete(dctx, containerd.WithProcessKill)
	}()
	statusC, err := process.Wait(ctx)
	if err != nil {
		return -1, fmt.Errorf("wait for exec: %w", classify(err))
	}
	if err := process.Start(ctx); err != nil {
		return -1, fmt.Errorf("start %v: %w", pspec.Args, classify(err))
	}
	for {
		select {
		case st := <-statusC:
			process.IO().Wait() // flush the remaining output
			code, _, err := st.Result()
			if err != nil {
				return -1, err
			}
			return int(code), nil
		case size, ok := <-resize:
			if !ok {
				resize = nil
				continue
			}
			if size.Cols > 0 && size.Rows > 0 {
				_ = process.Resize(ctx, uint32(size.Cols), uint32(size.Rows))
			}
		case <-ctx.Done():
			kctx, cancel := context.WithTimeout(cleanup, 5*time.Second)
			_ = process.Kill(kctx, syscall.SIGKILL)
			cancel()
			return -1, ctx.Err()
		}
	}
}

func execSpec(ctx context.Context, cl *containerd.Client, ct containerd.Container, opts ExecOptions) (*specs.Process, error) {
	spec, err := ct.Spec(ctx)
	if err != nil {
		return nil, fmt.Errorf("spec: %w", classify(err))
	}
	if opts.User != "" {
		info, err := ct.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("container info: %w", classify(err))
		}
		for _, o := range []oci.SpecOpts{oci.WithUser(opts.User), withoutAdditionalGIDs, oci.WithAdditionalGIDs(opts.User)} {
			if err := o(ctx, cl, &info, spec); err != nil {
				return nil, fmt.Errorf("%w: user %q: %w", errdefs.ErrInvalidArgument, opts.User, err)
			}
		}
	}
	p := spec.Process
	p.Terminal = true
	if opts.Cols > 0 && opts.Rows > 0 {
		p.ConsoleSize = &specs.Box{Width: uint(opts.Cols), Height: uint(opts.Rows)}
	}
	p.Args = opts.Cmd
	if len(p.Args) == 0 {
		p.Args = defaultShell
	}
	if opts.Workdir != "" {
		p.Cwd = opts.Workdir
	}
	p.Env = setEnv(p.Env, "TERM", "xterm-256color")
	return p, nil
}

// withoutAdditionalGIDs drops the container's additional groups before they are recomputed
// for the exec user, as nerdctl does.
func withoutAdditionalGIDs(_ context.Context, _ oci.Client, _ *containers.Container, s *oci.Spec) error {
	if s.Process != nil {
		s.Process.User.AdditionalGids = nil
	}
	return nil
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
