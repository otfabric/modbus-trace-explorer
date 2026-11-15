// SPDX-License-Identifier: MIT

package applog

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "app.log")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	log := New(l, slog.LevelInfo)
	line := strings.Repeat("x", 10_000)
	for range 500 {
		log.Info("message", "pad", line)
	}
	log.Debug("not at info level")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, path + ".1", path + ".2", path + ".3"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if fi.Size() > MaxBytes || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %d bytes, %v", p, fi.Size(), fi.Mode().Perm())
		}
	}
	if _, err := os.Stat(path + ".4"); !os.IsNotExist(err) {
		t.Error("kept more than Keep files")
	}
	b, _ := os.ReadFile(path)
	if bytes.Contains(b, []byte("not at info level")) {
		t.Error("debug logged at info level")
	}
	if fi, _ := os.Stat(filepath.Dir(path)); fi.Mode().Perm() != 0o700 {
		t.Errorf("dir %v", fi.Mode().Perm())
	}
}

func TestOpenErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(file, "app.log")); err == nil {
		t.Error("a log below a file opened")
	}
	if _, err := Open(dir); err == nil {
		t.Error("a directory opened as a log")
	}
}

// A reopened log continues where it was and counts what is there already.
func TestOpenCountsExistingSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, make([]byte, MaxBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if _, err := l.Write([]byte("next\n")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "next\n" {
		t.Errorf("no rotation: the log holds %d bytes", len(b))
	}
	if fi, err := os.Stat(path + ".1"); err != nil || fi.Size() != MaxBytes {
		t.Errorf("rotated file: %v, %v", fi, err)
	}
}

// When rotation cannot close the file, the write fails instead of losing the
// line silently.
func TestWriteAfterCloseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, make([]byte, MaxBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write([]byte("late\n")); err == nil {
		t.Error("a write to a closed log succeeded")
	}
}

// R23: an error logged at info level keeps its operation and cause but
// not the paths of the file system errors in it, wrapped or joined.
func TestErrHasNoPaths(t *testing.T) {
	dir := t.TempDir()
	_, open := os.Open(filepath.Join(dir, "home", "user", "trace.pcapng"))
	rename := os.Rename(filepath.Join(dir, "a"), filepath.Join(dir, "a", "b"))
	mkdir := os.Mkdir(dir, 0o700)
	for name, c := range map[string]struct {
		err  error
		want string
	}{
		"path":   {open, "open <path>: "},
		"link":   {rename, "rename <path> <path>: "},
		"wrap":   {fmt.Errorf("store: col: %w", open), "store: col: open <path>: "},
		"join":   {errors.Join(open, fmt.Errorf("evict: %w", mkdir)), "open <path>: "},
		"prefix": {&fs.PathError{Op: "remove", Path: dir, Err: errors.Join(&fs.PathError{Op: "unlinkat", Path: filepath.Join(dir, "x"), Err: fs.ErrPermission})}, "remove <path>: unlinkat <path>: "},
		"none":   {errors.New("store: more than 65535 client addresses"), "store: more than 65535 client addresses"},
	} {
		var buf bytes.Buffer
		New(&buf, slog.LevelInfo).Warn("failed", Err(c.err))
		if c.err == nil || strings.Contains(buf.String(), dir) || !strings.Contains(buf.String(), c.want) {
			t.Errorf("%s: %q, want %q and no %s", name, buf.String(), c.want, dir)
		}
	}
	if a := Err(nil); a.Value.String() != "" {
		t.Errorf("nil: %v", a)
	}
}
