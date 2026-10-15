// SPDX-License-Identifier: MIT

//go:build unix

package store

import (
	"os"
	"syscall"
)

// mapFile maps a whole file read-only. An empty file gives an empty slice.
func mapFile(path string) ([]byte, func() error, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if fi.Size() == 0 {
		return []byte{}, func() error { return nil }, nil
	}
	b, err := syscall.Mmap(int(f.Fd()), 0, int(fi.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, err
	}
	return b, func() error { return syscall.Munmap(b) }, nil
}
