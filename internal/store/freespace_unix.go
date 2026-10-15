// SPDX-License-Identifier: MIT

//go:build unix

package store

import "syscall"

// freeSpace returns the bytes available to this user on dir's file system.
func freeSpace(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil //nolint:unconvert // the field types differ between platforms
}
