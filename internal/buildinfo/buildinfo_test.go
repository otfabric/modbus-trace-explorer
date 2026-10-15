// SPDX-License-Identifier: MIT

package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestModuleVersion(t *testing.T) {
	if v := ModuleVersion("example.com/not-a-dependency"); v != "unknown" {
		t.Errorf("unknown module: %q", v)
	}
	info := &debug.BuildInfo{Deps: []*debug.Module{
		{Path: "a.example/x", Version: "v1.2.3"},
		{Path: "a.example/y", Version: "v0.1.0", Replace: &debug.Module{Path: "../y", Version: "(devel)"}},
	}}
	for path, want := range map[string]string{"a.example/x": "v1.2.3", "a.example/y": "(devel)", "a.example/z": "unknown"} {
		if got := depVersion(info, path); got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
	if Version == "" {
		t.Error("empty version")
	}
}
