// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"errors"
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
