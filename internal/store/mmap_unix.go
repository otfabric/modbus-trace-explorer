// SPDX-License-Identifier: MIT

//go:build unix

package store

import (
	"os"
	"syscall"
)

// mapFile maps a whole file read-only. An empty file gives an empty slice.
func mapFile(path string) ([]byte, func() error, error) { return mapPrefix(path, -1) }

// mapPrefix maps the first n bytes of a file read-only (n < 0: all of it),
// for a live index over files still being appended to. A file shorter
// than n is an error.
func mapPrefix(path string, n int64) ([]byte, func() error, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	size := fi.Size()
	if n >= 0 {
		if size < n {
			return nil, nil, errShort
		}
		size = n
	}
	if size == 0 {
		return []byte{}, func() error { return nil }, nil
	}
	b, err := syscall.Mmap(int(f.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, err
	}
	return b, func() error { return syscall.Munmap(b) }, nil
}
