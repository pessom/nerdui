// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Password policy.
const (
	MinPasswordLen = 12   // characters
	MaxPasswordLen = 1024 // bytes; bounds hashing cost
)

// Argon2Params are the argon2id cost parameters; they are stored in every hash so they can
// be raised later without breaking existing passwords.
type Argon2Params struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
	SaltLen uint32
	KeyLen  uint32
}

// DefaultArgon2 follows the OWASP recommendation (19 MiB, 2 iterations, 1 thread), which is
// affordable on small ARM boards.
var DefaultArgon2 = Argon2Params{Memory: 19 * 1024, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}

// ErrWeakPassword is returned for passwords that violate the policy.
var ErrWeakPassword = fmt.Errorf("password must be %d to %d characters long", MinPasswordLen, MaxPasswordLen)

var errMalformedHash = errors.New("malformed password hash")

// ValidatePassword checks the password policy.
func ValidatePassword(p string) error {
	if utf8.RuneCountInString(p) < MinPasswordLen || len(p) > MaxPasswordLen {
		return ErrWeakPassword
	}
	return nil
}

// HashPassword returns a PHC-formatted argon2id hash:
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>.
func HashPassword(password string, p Argon2Params) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches the encoded hash, and whether the hash
// uses weaker parameters than want (so callers can transparently rehash).
func VerifyPassword(password, encoded string, want Argon2Params) (ok, needsRehash bool, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, false, errMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, false, errMalformedHash
	}
	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return false, false, errMalformedHash
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, false, errMalformedHash
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return false, false, errMalformedHash
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, uint32(len(key))) //nolint:gosec // len(key) is small
	if subtle.ConstantTimeCompare(got, key) != 1 {
		return false, false, nil
	}
	weaker := p.Memory < want.Memory || p.Time < want.Time || p.Threads < want.Threads ||
		uint32(len(key)) < want.KeyLen || uint32(len(salt)) < want.SaltLen //nolint:gosec // lengths are small
	return true, weaker, nil
}
