// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"runtime"
	"testing"
)

func TestAbout(t *testing.T) {
	e := New(Options{})
	defer func() { _ = e.Close() }()
	a, err := e.About(context.Background())
	if err != nil || a.Version == "" || a.OS != runtime.GOOS || a.Go == "" {
		t.Errorf("%+v %v", a, err)
	}
}
