// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package httpjson

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/containerd/errdefs"
)

// Status maps an error from containerd/nerdctl (errdefs) to an HTTP status and error code
// (ADR-0006). Unknown errors are internal.
func Status(err error) (status int, code string) {
	switch {
	case errdefs.IsNotFound(err):
		return http.StatusNotFound, CodeNotFound
	case errdefs.IsAlreadyExists(err), errdefs.IsConflict(err), errdefs.IsFailedPrecondition(err):
		return http.StatusConflict, CodeConflict
	case errdefs.IsInvalidArgument(err), errdefs.IsOutOfRange(err):
		return http.StatusBadRequest, CodeInvalidArgument
	case errdefs.IsPermissionDenied(err), errdefs.IsUnauthorized(err):
		return http.StatusForbidden, CodeForbidden
	case errdefs.IsUnavailable(err), errdefs.IsDeadlineExceeded(err), errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable, CodeUnavailable
	case errdefs.IsNotImplemented(err):
		return http.StatusNotImplemented, CodeUnavailable
	default:
		return http.StatusInternalServerError, CodeInternal
	}
}

// WriteErr writes err using Status. Client-side errors carry their message; internal errors
// are logged and answered with a generic message so details do not leak.
func WriteErr(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	status, code := Status(err)
	if status == http.StatusInternalServerError {
		if logger != nil {
			logger.Error("internal error", "method", r.Method, "path", r.URL.Path, "error", err)
		}
		WriteError(w, status, code, "internal error")
		return
	}
	WriteError(w, status, code, err.Error())
}
