// SPDX-License-Identifier: MIT

// Package applog is the application's log (R23): structured text in a
// small rotating file in the application's data directory. Callers never
// log trace content and, at info level, no addresses or paths.
package applog

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Sizes of the rotation: the current file and this many older ones.
const (
	MaxBytes = 1 << 20
	Keep     = 3
)

// File is a size-rotated log file: name, name.1, ... name.Keep.
type File struct {
	path string
	mu   sync.Mutex
	f    *os.File
	size int64
}

// Open opens (or creates) the log file at path, 0600, in a 0700 directory.
func Open(path string) (*File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &File{path: path, f: f, size: fi.Size()}, nil
}

// Write appends p, rotating first when the file would pass MaxBytes.
func (l *File) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size+int64(len(p)) > MaxBytes && l.size > 0 {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *File) rotate() error {
	if err := l.f.Close(); err != nil {
		return err
	}
	for i := Keep - 1; i >= 1; i-- {
		_ = os.Rename(l.path+"."+itoa(i), l.path+"."+itoa(i+1))
	}
	_ = os.Rename(l.path, l.path+".1")
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	l.f, l.size = f, 0
	return nil
}

func itoa(i int) string { return string(rune('0' + i)) }

// Close closes the file.
func (l *File) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}

// New returns a logger writing text to w at the given level.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}
