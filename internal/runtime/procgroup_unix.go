// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package runtime

import (
	"os/exec"
	"syscall"
	"time"
)

// killGroupOnCancel runs cmd in its own process group and kills the whole group when its
// context ends: nerdctl build starts buildctl, which would otherwise outlive nerdctl and keep
// the output open, so the build would neither stop nor return (ADR-0061).
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 10 * time.Second
}
