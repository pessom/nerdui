// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// statFS returns an identifier of the filesystem holding path and its total and available
// bytes (for unprivileged users, like df).
func statFS(path string) (id string, total, free uint64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return "", 0, 0, err
	}
	bsize := uint64(st.Bsize) //nolint:gosec // G115: block sizes are positive
	return fmt.Sprintf("%x:%x", st.Fsid.Val[0], st.Fsid.Val[1]), st.Blocks * bsize, st.Bavail * bsize, nil
}
