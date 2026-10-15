// SPDX-License-Identifier: MIT

//go:build windows

package store

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
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
	size := fi.Size()
	if size == 0 {
		return []byte{}, func() error { return nil }, nil
	}
	h, err := windows.CreateFileMapping(windows.Handle(f.Fd()), nil, windows.PAGE_READONLY, uint32(size>>32), uint32(size), nil)
	if err != nil {
		return nil, nil, err
	}
	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_READ, 0, 0, uintptr(size))
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, nil, err
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(addr)), int(size))
	return b, func() error {
		err := windows.UnmapViewOfFile(addr)
		if cerr := windows.CloseHandle(h); err == nil {
			err = cerr
		}
		return err
	}, nil
}
