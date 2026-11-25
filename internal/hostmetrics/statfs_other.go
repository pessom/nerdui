// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package hostmetrics

import "errors"

// statFS is only implemented on Linux, where nerdui runs.
func statFS(string) (string, uint64, uint64, error) {
	return "", 0, 0, errors.New("filesystem usage is only read on Linux")
}
