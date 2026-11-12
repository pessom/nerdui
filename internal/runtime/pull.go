// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"io"

	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/log"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	nimage "github.com/containerd/nerdctl/v2/pkg/cmd/image"
	"github.com/containerd/nerdctl/v2/pkg/platformutil"
	"github.com/containerd/nerdctl/v2/pkg/referenceutil"
	"github.com/sirupsen/logrus"
)

// PullOptions are the options of an image pull (#43).
type PullOptions struct {
	// Platform pulls one platform (e.g. linux/arm64); empty means the host platform.
	Platform string
	// AllPlatforms pulls every platform of a multi-platform image (no unpack).
	AllPlatforms bool
}

// Pull pulls an image like `nerdctl pull` (always contacting the registry) and writes the
// progress to out as terminal output. nerdctl's registry configuration applies: hosts.d,
// credentials of the user running nerdui (docker config), insecure registries.
func (ci clientImages) Pull(ctx context.Context, namespace, ref string, opts PullOptions, out io.Writer) error {
	if _, err := referenceutil.Parse(ref); err != nil || ref == "" {
		return fmt.Errorf("%w: invalid image reference %q", errdefs.ErrInvalidArgument, ref)
	}
	if opts.AllPlatforms && opts.Platform != "" {
		return fmt.Errorf("%w: platform and all platforms are mutually exclusive", errdefs.ErrInvalidArgument)
	}
	var requested []string
	if opts.Platform != "" {
		requested = []string{opts.Platform}
	}
	plats, err := platformutil.NewOCISpecPlatformSlice(opts.AllPlatforms, requested)
	if err != nil {
		return fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	cl, err := ci.c.conn()
	if err != nil {
		return err
	}
	ctx = withJobLogger(namespaces.WithNamespace(ctx, namespace), out)

	cfg := ci.c.nerdctlFor(namespace)
	_, _ = fmt.Fprintf(out, "Pulling %s\r\n", ref)
	err = nimage.Pull(ctx, cl, ref, types.ImagePullOptions{
		Stdout:                 out,
		Stderr:                 out,
		ProgressOutputToStdout: true,
		GOptions:               types.GlobalCommandOptions(*cfg),
		VerifyOptions:          types.ImageVerifyOptions{Provider: "none"},
		OCISpecPlatform:        plats,
		Mode:                   "always",
	})
	if err != nil {
		return fmt.Errorf("pull %s: %w", ref, classify(err))
	}
	return nil
}

// withJobLogger sends nerdctl's context-logger warnings (e.g. skipped TLS verification) to the
// job output as well.
func withJobLogger(ctx context.Context, out io.Writer) context.Context {
	l := logrus.New()
	l.SetOutput(out)
	l.SetLevel(logrus.InfoLevel)
	l.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true, ForceColors: true})
	return log.WithLogger(ctx, logrus.NewEntry(l))
}
