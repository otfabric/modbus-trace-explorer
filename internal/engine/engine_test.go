// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"runtime"
	"testing"
)

func TestAbout(t *testing.T) {
	e, err := New(Options{CacheDir: t.TempDir(), ConfigDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Shutdown() }()
	a, err := e.About(context.Background())
	if err != nil || a.Version == "" || a.OS != runtime.GOOS || a.Go == "" {
		t.Errorf("%+v %v", a, err)
	}
}
