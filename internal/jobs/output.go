// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package jobs

import (
	"bytes"
	"sync"
)

// clearLine is what progress writers (containerd pkg/progress, used by nerdctl) emit to redraw:
// move up one line, erase it, return to column 0.
var clearLine = []byte("\x1b[1A\x1b[2K\r")

// Output is the terminal output of a job (ADR-0018). Live subscribers receive the raw bytes;
// the stored copy applies progress redraws, so it holds the screen as a terminal would show
// it instead of every frame: a long pull keeps one progress table, not thousands. The stored
// copy is capped; older lines are dropped first.
type Output struct {
	mu        sync.Mutex
	lines     [][]byte // completed lines, each without "\n"
	partial   []byte   // current line
	size      int      // bytes in lines and partial
	max       int
	truncated bool
	onWrite   func(p []byte) // called with the raw bytes under mu, in order
	result    map[string]string
}

// SetResult records a value of the job's outcome, e.g. the ID of a created container; it
// is part of the job once it finishes.
func (o *Output) SetResult(key, value string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.result == nil {
		o.result = map[string]string{}
	}
	o.result[key] = value
}

func newOutput(limit int) *Output { return &Output{max: limit} }

// Write appends terminal output. It never fails, so job code can ignore write errors.
func (o *Output) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.apply(p)
	if o.onWrite != nil {
		o.onWrite(bytes.Clone(p))
	}
	return len(p), nil
}

func (o *Output) apply(p []byte) {
	for len(p) > 0 {
		i := bytes.Index(p, clearLine)
		chunk := p
		if i >= 0 {
			chunk = p[:i]
		}
		o.appendText(chunk)
		if i < 0 {
			break
		}
		o.eraseLine()
		p = p[i+len(clearLine):]
	}
	for o.size > o.max && len(o.lines) > 0 {
		o.size -= len(o.lines[0]) + 1
		o.lines = o.lines[1:]
		o.truncated = true
	}
}

func (o *Output) appendText(p []byte) {
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			o.partial = append(o.partial, p...)
			o.size += len(p)
			return
		}
		o.partial = append(o.partial, p[:i]...)
		o.size += i + 1
		o.lines = append(o.lines, o.partial)
		o.partial = nil
		p = p[i+1:]
	}
}

// eraseLine applies clearLine: the cursor moves up to the previous line and clears it, so
// the current (partial) line and the previous line are both gone.
func (o *Output) eraseLine() {
	o.size -= len(o.partial)
	o.partial = nil
	if n := len(o.lines); n > 0 {
		o.size -= len(o.lines[n-1]) + 1
		o.lines = o.lines[:n-1]
	}
}

// snapshotLocked returns the stored screen as terminal text.
func (o *Output) snapshotLocked() string {
	var b bytes.Buffer
	if o.truncated {
		b.WriteString("\x1b[2m… earlier output dropped …\x1b[0m\r\n")
	}
	for _, l := range o.lines {
		b.Write(l)
		b.WriteByte('\n')
	}
	b.Write(o.partial)
	return b.String()
}

// String returns the stored screen.
func (o *Output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.snapshotLocked()
}
