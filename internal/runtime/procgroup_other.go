// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !unix

package runtime

import (
	"os/exec"
	"time"
)

// killGroupOnCancel only bounds the wait where process groups are not available.
func killGroupOnCancel(cmd *exec.Cmd) { cmd.WaitDelay = 10 * time.Second }
