// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package secretbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreate(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sub", FileName)
	b, err := LoadOrCreate(file)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	sealed, err := b.Seal("s3cret", "A")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "s3cret") || !strings.HasPrefix(sealed, "v1:") {
		t.Errorf("sealed = %q", sealed)
	}
	again, err := LoadOrCreate(file) // the same key is read back
	if err != nil {
		t.Fatal(err)
	}
	if got, err := again.Open(sealed, "A"); err != nil || got != "s3cret" {
		t.Errorf("Open = %q, %v", got, err)
	}
	if _, err := again.Open(sealed, "B"); !errors.Is(err, ErrDecrypt) {
		t.Errorf("other aad: %v", err)
	}
	other, err := LoadOrCreate(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(sealed, "A"); !errors.Is(err, ErrDecrypt) {
		t.Errorf("other key: %v", err)
	}
	for _, bad := range []string{"", "plain", "v1:!!", "v1:AAAA"} {
		if _, err := b.Open(bad, "A"); !errors.Is(err, ErrDecrypt) {
			t.Errorf("Open(%q): %v", bad, err)
		}
	}
}

func TestLoadBadKey(t *testing.T) {
	file := filepath.Join(t.TempDir(), FileName)
	for _, content := range []string{"zz", "abcd"} {
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreate(file); err == nil {
			t.Errorf("key %q accepted", content)
		}
	}
}
