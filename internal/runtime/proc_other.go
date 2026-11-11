// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package runtime

import "time"

// processStartTime is only available on Linux hosts.
func processStartTime(uint32) *time.Time { return nil }
