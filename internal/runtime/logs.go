// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-FileCopyrightText: The containerd Authors
// SPDX-License-Identifier: AGPL-3.0-only AND Apache-2.0

// Logs adapts the follow logic and getLogPath of nerdctl pkg/cmd/container/logs.go
// (https://github.com/containerd/nerdctl, Apache-2.0, Copyright The containerd Authors).
// container.Logs itself cannot be used in a server: it stops following only on OS signals
// and registers a signal handler on every call (ADR-0014).

package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/identifiers"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/log"
	"github.com/containerd/nerdctl/v2/pkg/api/types/cri"
	"github.com/containerd/nerdctl/v2/pkg/clientutil"
	"github.com/containerd/nerdctl/v2/pkg/labels"
	"github.com/containerd/nerdctl/v2/pkg/labels/k8slabels"
	"github.com/containerd/nerdctl/v2/pkg/logging"
	"github.com/containerd/nerdctl/v2/pkg/timestamp"
)

// LogOptions select the log entries, like `nerdctl logs`.
type LogOptions struct {
	// Follow keeps streaming new entries while the container runs.
	Follow bool
	// Tail limits the output to the last n entries (0 = all).
	Tail uint
	// Since and Until bound the entries by time: RFC 3339 or relative ("10m").
	Since, Until string
}

// LogLine is one log entry.
type LogLine struct {
	// Stream is "stdout" or "stderr".
	Stream string `json:"stream"`
	// Time is when the line was logged; zero when the log driver does not record it
	// (journald).
	Time time.Time `json:"ts,omitzero"`
	// Line is the text without the trailing newline, ANSI sequences preserved.
	Line string `json:"line"`
}

// LogEmitter receives log lines in order; an error stops reading.
type LogEmitter func(LogLine) error

// Logs reads the logs of the container with the exact id and passes each line to emit.
// With Follow it returns when the container stops, ctx ends or emit fails.
func (cc clientContainers) Logs(ctx context.Context, namespace, id string, opts LogOptions, emit LogEmitter) error {
	if err := identifiers.Validate(id); err != nil {
		return fmt.Errorf("%w: container id: %w", errdefs.ErrInvalidArgument, err)
	}
	for name, v := range map[string]string{"since": opts.Since, "until": opts.Until} {
		if v == "" {
			continue
		}
		if _, err := timestamp.GetTimestamp(v, time.Now()); err != nil {
			return fmt.Errorf("%w: %s: %w", errdefs.ErrInvalidArgument, name, err)
		}
	}
	cl, err := cc.c.conn()
	if err != nil {
		return err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	c, err := cl.LoadContainer(ctx, id)
	if err != nil {
		if errdefs.IsNotFound(classify(err)) {
			return fmt.Errorf("%w: no such container: %s", errdefs.ErrNotFound, id)
		}
		return fmt.Errorf("load container: %w", classify(err))
	}
	l, err := c.Labels(ctx)
	if err != nil {
		return fmt.Errorf("container labels: %w", classify(err))
	}
	logPath, err := getLogPath(ctx, c)
	if err != nil {
		return err
	}
	cfg := cc.c.nerdctlFor(namespace)
	dataStore, err := clientutil.DataStore(cfg.DataRoot, cfg.Address)
	if err != nil {
		return err
	}
	ns := l[labels.Namespace]
	if ns == "" {
		ns = namespace
	}

	// stop ends the viewer: closing makes every receive succeed, so it is never sent to.
	stop := make(chan os.Signal)
	var stopOnce sync.Once
	stopViewer := func() { stopOnce.Do(func() { close(stop) }) }
	defer stopViewer()
	go func() {
		select {
		case <-ctx.Done():
			stopViewer()
		case <-stop:
		}
	}()

	follow := opts.Follow
	if follow {
		follow, err = followTask(ctx, c, dataStore, ns, stopViewer)
		if err != nil {
			return err
		}
		if !follow {
			// The task has already stopped: let the logger finish writing first.
			if err := logging.WaitForLogger(dataStore, ns, c.ID()); err != nil {
				log.G(ctx).WithError(err).Warn("failed to wait for logger shutdown")
			}
		}
	}

	viewer, err := logging.InitContainerLogViewer(l, logging.LogViewOptions{
		ContainerID:       c.ID(),
		Namespace:         ns,
		DatastoreRootPath: dataStore,
		LogPath:           logPath,
		Follow:            follow,
		Timestamps:        true,
		Tail:              opts.Tail,
		Since:             opts.Since,
		Until:             opts.Until,
	}, stop, cfg.Experimental)
	if err != nil {
		if strings.Contains(err.Error(), "log type `none`") {
			return fmt.Errorf("%w: logging is disabled for this container (log driver none)", errdefs.ErrFailedPrecondition)
		}
		return fmt.Errorf("logs: %w", err)
	}
	out := &lineSplitter{emit: emit}
	err = viewer.PrintLogsTo(out.writer("stdout"), out.writer("stderr"))
	if ferr := out.flush(); err == nil {
		err = ferr
	}
	if out.err != nil {
		return out.err // emit failed: the client went away
	}
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("logs: %w", err)
	}
	return nil
}

// followTask reports whether the container's task is running and, if so, stops the viewer
// once the task exits and the logger has written its last entries.
func followTask(ctx context.Context, c containerd.Container, dataStore, ns string, stopViewer func()) (bool, error) {
	task, err := c.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(classify(err)) {
			return false, nil
		}
		return false, fmt.Errorf("load task: %w", classify(err))
	}
	status, err := task.Status(ctx)
	if err != nil {
		return false, fmt.Errorf("task status: %w", classify(err))
	}
	if status.Status != containerd.Running {
		return false, nil
	}
	waitCh, err := task.Wait(ctx)
	if err != nil {
		return false, fmt.Errorf("wait for task: %w", classify(err))
	}
	go func() {
		select {
		case <-waitCh:
		case <-ctx.Done():
			return
		}
		if err := logging.WaitForLogger(dataStore, ns, c.ID()); err != nil {
			log.G(ctx).WithError(err).Error("failed to wait for logger shutdown")
		}
		stopViewer()
	}()
	return true, nil
}

// getLogPath returns the log path of containers created via CRI (empty otherwise).
func getLogPath(ctx context.Context, c containerd.Container) (string, error) {
	extensions, err := c.Extensions(ctx)
	if err != nil {
		return "", fmt.Errorf("get extensions for container %s: %w", c.ID(), classify(err))
	}
	var meta cri.ContainerMetadata
	if md := extensions[k8slabels.ContainerMetadataExtension]; md != nil {
		if err := meta.UnmarshalJSON(md.GetValue()); err != nil {
			return "", fmt.Errorf("unmarshal extensions for container %s: %w", c.ID(), err)
		}
	}
	return meta.LogPath, nil
}

// lineSplitter turns the viewers' writes into LogLines. Viewers may write from two
// goroutines (journald pipes stdout and stderr separately) and in arbitrary chunks.
type lineSplitter struct {
	mu   sync.Mutex
	emit LogEmitter
	buf  map[string]*bytes.Buffer
	err  error
}

type streamWriter struct {
	s      *lineSplitter
	stream string
}

func (s *lineSplitter) writer(stream string) streamWriter { return streamWriter{s, stream} }

func (w streamWriter) Write(p []byte) (int, error) {
	s := w.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	if s.buf == nil {
		s.buf = map[string]*bytes.Buffer{}
	}
	b := s.buf[w.stream]
	if b == nil {
		b = &bytes.Buffer{}
		s.buf[w.stream] = b
	}
	b.Write(p)
	for {
		i := bytes.IndexByte(b.Bytes(), '\n')
		if i < 0 {
			break
		}
		line := string(b.Next(i + 1)[:i])
		if err := s.emit(parseLogLine(w.stream, line)); err != nil {
			s.err = err
			return 0, err
		}
	}
	return len(p), nil
}

// flush emits unterminated last lines.
func (s *lineSplitter) flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stream := range []string{"stdout", "stderr"} {
		if b := s.buf[stream]; b != nil && b.Len() > 0 && s.err == nil {
			s.err = s.emit(parseLogLine(stream, b.String()))
			b.Reset()
		}
	}
	return s.err
}

// parseLogLine splits the "2006-01-02T15:04:05.999999999Z07:00 " prefix written by the
// json-file and cri viewers; lines without it (journald) keep a zero time.
func parseLogLine(stream, s string) LogLine {
	s = strings.TrimSuffix(s, "\r")
	if i := strings.IndexByte(s, ' '); i > 0 {
		if t, err := time.Parse(time.RFC3339Nano, s[:i]); err == nil {
			return LogLine{Stream: stream, Time: t.UTC(), Line: s[i+1:]}
		}
	}
	return LogLine{Stream: stream, Line: s}
}
