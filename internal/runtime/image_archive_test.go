// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestNotArchive(t *testing.T) {
	for msg, want := range map[string]bool{
		"archive/tar: invalid tar header":                   true,
		"rpc error: code = Unknown desc = unexpected EOF":   true,
		"failed to unpack: no space left on device":         false,
		"failed to read index.json: file does not exist":    true,
		"connection error: desc = transport: error reading": false,
	} {
		if got := notArchive(errors.New(msg)); got != want {
			t.Errorf("%s: %v", msg, got)
		}
	}
}

func TestStoppableStreams(t *testing.T) {
	r := &stoppableReader{r: strings.NewReader("abc")}
	buf := make([]byte, 2)
	if n, err := r.Read(buf); n != 2 || err != nil {
		t.Fatalf("read %d %v", n, err)
	}
	r.stop()
	if n, err := r.Read(buf); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("read after stop: %d %v", n, err)
	}

	var out strings.Builder
	w := &stoppableWriter{w: &out, closed: make(chan struct{})}
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	_ = w.Close()
	<-w.closed
	w.stop()
	if _, err := w.Write([]byte("y")); err == nil || out.String() != "x" {
		t.Errorf("write after stop: %v, %q", err, out.String())
	}
}
