// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestParseLogLine(t *testing.T) {
	ts := time.Date(2027, 3, 1, 10, 0, 0, 123, time.UTC)
	for in, want := range map[string]LogLine{
		"2027-03-01T10:00:00.000000123Z hello world": {Stream: "stdout", Time: ts, Line: "hello world"},
		"2027-03-01T13:00:00.000000123+03:00 x\r":    {Stream: "stdout", Time: ts, Line: "x"},
		"2027-03-01T10:00:00.000000123Z ":            {Stream: "stdout", Time: ts, Line: ""},
		"plain journald line":                        {Stream: "stdout", Line: "plain journald line"},
		"":                                           {Stream: "stdout", Line: ""},
	} {
		if got := parseLogLine("stdout", in); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %+v, want %+v", in, got, want)
		}
	}
}

func TestLineSplitter(t *testing.T) {
	var mu sync.Mutex
	var got []LogLine
	s := &lineSplitter{emit: func(l LogLine) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, l)
		return nil
	}}
	out, errw := s.writer("stdout"), s.writer("stderr")
	// Chunks split anywhere, as from journalctl pipes; streams buffer separately.
	_, _ = out.Write([]byte("one\ntw"))
	_, _ = errw.Write([]byte("oops"))
	_, _ = out.Write([]byte("o\nthree"))
	_, _ = errw.Write([]byte("\n"))
	if err := s.flush(); err != nil {
		t.Fatal(err)
	}
	want := []LogLine{
		{Stream: "stdout", Line: "one"}, {Stream: "stdout", Line: "two"},
		{Stream: "stderr", Line: "oops"}, {Stream: "stdout", Line: "three"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}

	gone := errors.New("gone")
	s = &lineSplitter{emit: func(LogLine) error { return gone }}
	if _, err := s.writer("stdout").Write([]byte("a\n")); !errors.Is(err, gone) {
		t.Errorf("emit error not returned: %v", err)
	}
	if _, err := s.writer("stderr").Write([]byte("b\n")); !errors.Is(err, gone) {
		t.Errorf("later writes must fail too: %v", err)
	}
}
