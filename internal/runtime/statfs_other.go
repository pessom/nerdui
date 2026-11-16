// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package runtime

import "errors"

func statFS(string) (string, uint64, uint64, error) {
	return "", 0, 0, errors.New("statfs is not supported")
}
