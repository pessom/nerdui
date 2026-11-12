// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package httpjson contains JSON request/response helpers shared by HTTP handlers,
// including the error envelope from ADR-0006:
//
//	{"error": {"code": "not_found", "message": "..."}}
package httpjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"
)

// Error codes (ADR-0006). Keep in sync with api/openapi.yaml.
const (
	CodeInvalidArgument = "invalid_argument"
	CodeUnauthenticated = "unauthenticated"
	CodeForbidden       = "forbidden"
	CodeCSRF            = "csrf_failed"
	CodeNotFound        = "not_found"
	CodeConflict        = "conflict"
	CodeRateLimited     = "rate_limited"
	CodeUnsupportedType = "unsupported_media_type"
	CodeInternal        = "internal"
	// CodePasswordChangeRequired: the session may only change its temporary password (#74).
	CodePasswordChangeRequired = "password_change_required"
	CodeUnavailable            = "unavailable"
	// CodeNerdctlIncompatible: the host nerdctl is missing or its version does not match
	// (ADR-0011); nerdui is read-only.
	CodeNerdctlIncompatible = "nerdctl_incompatible"
)

// MaxBodyBytes limits JSON request bodies.
const MaxBodyBytes = 1 << 20

// ErrorBody is the error envelope.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail describes an API error.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Write writes v as JSON with the given status.
func Write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes the error envelope.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	Write(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message}})
}

// WriteRateLimited writes 429 with a Retry-After header.
func WriteRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	secs := int(retryAfter.Round(time.Second) / time.Second)
	w.Header().Set("Retry-After", strconv.Itoa(max(1, secs)))
	WriteError(w, http.StatusTooManyRequests, CodeRateLimited, "too many attempts, try again later")
}

// Decode reads a JSON body into v. It requires Content-Type application/json (which also
// prevents cross-site form posts), limits the body size and rejects unknown fields.
// On failure it writes the error response and returns false.
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		WriteError(w, http.StatusUnsupportedMediaType, CodeUnsupportedType, "Content-Type must be application/json")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		WriteError(w, http.StatusBadRequest, CodeInvalidArgument, fmt.Sprintf("invalid JSON body: %v", err))
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, CodeInvalidArgument, "request body must contain a single JSON object")
		return false
	}
	return true
}
