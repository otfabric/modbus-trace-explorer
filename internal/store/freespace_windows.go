// SPDX-License-Identifier: MIT

//go:build windows

package store

import "golang.org/x/sys/windows"

// freeSpace returns the bytes available to this user on dir's volume.
func freeSpace(dir string) (int64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free, total, all uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &all); err != nil {
		return 0, err
	}
	return int64(free), nil
}
