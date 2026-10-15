// SPDX-License-Identifier: MIT

package applog

import (
	"bytes"
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
