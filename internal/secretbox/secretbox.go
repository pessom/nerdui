// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package secretbox encrypts secrets stored in nerdui's database with AES-256-GCM under a key
// kept in a file next to it (#35, ADR-0049).
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileName of the key inside the data directory.
const FileName = "secret.key"

// prefix marks values encrypted by this version of the format.
const prefix = "v1:"

// ErrDecrypt means a value cannot be decrypted: a different key or damaged data.
var ErrDecrypt = errors.New("cannot decrypt a stored secret: the key file does not match the database")

// Box encrypts and decrypts values.
type Box struct{ aead cipher.AEAD }

// New returns a box for a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("the secret key must have 32 bytes, not %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// LoadOrCreate reads the hex key of file, creating it with a random key (mode 0600) when it
// does not exist.
func LoadOrCreate(file string) (*Box, error) {
	b, err := os.ReadFile(file) //nolint:gosec // G304: the configured key file
	if errors.Is(err, fs.ErrNotExist) {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: the configured key file
		if err != nil {
			return nil, fmt.Errorf("create the secret key: %w", err)
		}
		_, werr := f.WriteString(hex.EncodeToString(key) + "\n")
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(file)
			return nil, fmt.Errorf("write the secret key: %w", werr)
		}
		return New(key)
	}
	if err != nil {
		return nil, fmt.Errorf("read the secret key: %w", err)
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, fmt.Errorf("secret key %s: not hex: %w", file, err)
	}
	return New(key)
}

// Seal encrypts plain; aad binds the result to its place (e.g. the variable's name), so a value
// cannot be moved to another row.
func (b *Box) Seal(plain, aad string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plain), []byte(aad))
	return prefix + base64.RawStdEncoding.EncodeToString(out), nil
}

// Open decrypts a value of Seal with the same aad.
func (b *Box) Open(sealed, aad string) (string, error) {
	rest, ok := strings.CutPrefix(sealed, prefix)
	if !ok {
		return "", ErrDecrypt
	}
	raw, err := base64.RawStdEncoding.DecodeString(rest)
	n := b.aead.NonceSize()
	if err != nil || len(raw) < n {
		return "", ErrDecrypt
	}
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], []byte(aad))
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}
