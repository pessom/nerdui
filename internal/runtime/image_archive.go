// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/containerd/containerd/v2/core/transfer"
	tarchive "github.com/containerd/containerd/v2/core/transfer/archive"
	transferimage "github.com/containerd/containerd/v2/core/transfer/image"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
)

// MaxSaveImages limits the images of one archive.
const MaxSaveImages = 50

// CheckSave verifies that every image named in names exists, so that a download can fail with
// a status before the archive starts.
func (ci clientImages) CheckSave(ctx context.Context, namespace string, names []string) error {
	if len(names) == 0 || len(names) > MaxSaveImages {
		return fmt.Errorf("%w: 1 to %d images are required", errdefs.ErrInvalidArgument, MaxSaveImages)
	}
	cl, err := ci.c.conn()
	if err != nil {
		return err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	for _, name := range names {
		if _, err := cl.ImageService().Get(ctx, name); err != nil {
			return fmt.Errorf("image %s: %w", name, classify(err))
		}
	}
	return nil
}

// Save writes the images named in names, for the host's platform, as one tar archive that
// `nerdctl load` and `docker load` read, like `nerdctl save` (#46).
func (ci clientImages) Save(ctx context.Context, namespace string, names []string, w io.Writer) error {
	if err := ci.CheckSave(ctx, namespace, names); err != nil {
		return err
	}
	cl, err := ci.c.conn()
	if err != nil {
		return err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	var refs []transferimage.StoreOpt
	for _, name := range slices.Compact(slices.Sorted(slices.Values(names))) {
		refs = append(refs, transferimage.WithExtraReference(name))
	}
	// containerd copies the archive to w in a goroutine that can outlive Transfer: wait until
	// it closed the stream, and never let it write to w after Save returned.
	sw := &stoppableWriter{w: w, closed: make(chan struct{})}
	defer sw.stop()
	err = cl.Transfer(ctx,
		transferimage.NewStore("", refs...),
		tarchive.NewImageExportStream(sw, "", tarchive.WithPlatform(platforms.DefaultSpec())),
	)
	if err == nil {
		select {
		case <-sw.closed:
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	if err != nil {
		return fmt.Errorf("save images: %w", classify(err))
	}
	return nil
}

// Load imports the images of a tar archive (from `nerdctl save` or `docker save`, optionally
// gzip-compressed) for the host's platform and unpacks them, like `nerdctl load` (#46). It
// returns the names of the images it created or updated. Images without a name are named
// import-<date>@<digest>, as nerdctl names them.
func (ci clientImages) Load(ctx context.Context, namespace string, r io.Reader) ([]string, error) {
	cl, err := ci.c.conn()
	if err != nil {
		return nil, err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	started := time.Now()
	platform := platforms.DefaultSpec()
	// containerd reads r in a goroutine that can outlive Transfer: stop it before returning.
	tctx, cancel := context.WithCancel(ctx)
	sr := &stoppableReader{r: r}
	defer sr.stop()
	defer cancel()
	err = cl.Transfer(tctx,
		tarchive.NewImageImportStream(sr, ""),
		transferimage.NewStore("",
			transferimage.WithPlatforms(platform),
			transferimage.WithUnpack(platform, ci.c.nerdctlFor(namespace).Snapshotter),
			transferimage.WithDigestRef("import", true, true),
			transferimage.WithNamedPrefix("import-"+started.Format("2006-01-02"), true),
		),
		transfer.WithProgress(func(transfer.Progress) {}),
	)
	if err != nil {
		if notArchive(err) {
			return nil, fmt.Errorf("%w: not an image archive: %s", errdefs.ErrInvalidArgument, err.Error())
		}
		return nil, fmt.Errorf("load images: %w", classify(err))
	}
	list, err := cl.ImageService().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", classify(err))
	}
	var names []string
	for _, img := range list {
		// Store timestamps may be a little behind the clock of nerdui.
		if !img.UpdatedAt.Before(started.Add(-time.Second)) {
			names = append(names, img.Name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// notArchive reports whether a load failed because the upload is not a (complete) image
// archive rather than because of the host.
func notArchive(err error) bool {
	msg := err.Error()
	for _, s := range []string{"archive/tar", "unexpected EOF", "gzip:", "index.json", "manifest.json", "invalid character"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// stoppableReader reads r until stop; stop waits for a read in progress.
type stoppableReader struct {
	mu      sync.Mutex
	r       io.Reader
	stopped bool
}

func (s *stoppableReader) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return 0, io.EOF
	}
	return s.r.Read(p)
}

func (s *stoppableReader) stop() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
}

// stoppableWriter writes to w until stop; stop waits for a write in progress. closed is closed
// when the writer of the stream closes it.
type stoppableWriter struct {
	mu      sync.Mutex
	w       io.Writer
	stopped bool
	once    sync.Once
	closed  chan struct{}
}

func (s *stoppableWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return 0, io.ErrClosedPipe
	}
	return s.w.Write(p)
}

func (s *stoppableWriter) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func (s *stoppableWriter) stop() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
}
