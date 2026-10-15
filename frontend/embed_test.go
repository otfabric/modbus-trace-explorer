// SPDX-License-Identifier: MIT

package frontend

import (
	"testing"
	"testing/fstest"
)

func TestBuilt(t *testing.T) {
	if Built(fstest.MapFS{}) {
		t.Error("an empty tree is not a build")
	}
	if !Built(fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}) {
		t.Error("a tree with index.html is a build")
	}
	_ = Wails()
}
