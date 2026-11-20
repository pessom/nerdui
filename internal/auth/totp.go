// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // G505: HMAC-SHA1 is what RFC 6238 and every authenticator app use
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters (RFC 6238 defaults, understood by every authenticator app).
const (
	totpPeriod  = 30
	totpDigits  = 6
	totpSkew    = 1 // steps accepted before and after the current one (clock drift)
	totpSecretN = 20
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns a random base32 secret.
func newTOTPSecret() string {
	b := make([]byte, totpSecretN)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return b32.EncodeToString(b)
}

func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// totpCode computes the code of a step (RFC 4226 dynamic truncation).
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // G115: steps are positive
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1_000_000)
}

// verifyTOTP returns the step a code belongs to, if it matches the current step or one next to
// it. Callers must reject steps at or before the last one accepted (replay).
func verifyTOTP(secretB32, code string, now time.Time) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	secret, err := b32.DecodeString(strings.ToUpper(secretB32))
	if err != nil || len(secret) == 0 {
		return 0, false
	}
	cur := totpStep(now)
	for d := -totpSkew; d <= totpSkew; d++ {
		step := cur + int64(d)
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// totpURI is the otpauth:// URI authenticator apps import, usually from a QR code.
func totpURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
