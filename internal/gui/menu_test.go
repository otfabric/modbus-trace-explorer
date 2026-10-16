// SPDX-License-Identifier: MIT

package gui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

func TestMenu(t *testing.T) {
	recent := []service.RecentFile{
		{Paths: []string{"/t/a.pcapng"}, Name: "a.pcapng", Exists: true},
		{Paths: []string{"/t/gone.pcap"}, Name: "gone.pcap", Exists: false},
	}
	m := buildMenu(recent, func(MenuAction) {}, func() {}, false)
	for _, label := range []string{"Open…", "a.pcapng", "Clear Recent", "Close Trace", "Export…", "Settings…", "Switch Theme",
		"About Modbus Trace Explorer", "Documentation"} {
		if m.FindByLabel(label) == nil {
			t.Errorf("no %q item", label)
		}
	}
	if m.FindByLabel("gone.pcap") != nil {
		t.Error("a missing recent file is listed")
	}
	empty := buildMenu(nil, func(MenuAction) {}, func() {}, false)
	if empty.FindByLabel("No recent traces") == nil || empty.FindByLabel("Clear Recent") != nil {
		t.Error("empty recent list")
	}
}

func TestGeometry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	def, minimum := Geometry{Width: 1440, Height: 900}, Geometry{Width: 1024, Height: 640}
	if g := LoadGeometry(dir, def, minimum); g != def {
		t.Errorf("no file: %+v", g)
	}
	want := Geometry{Width: 1600, Height: 1000, Maximised: true}
	if err := SaveGeometry(dir, want); err != nil {
		t.Fatal(err)
	}
	if g := LoadGeometry(dir, def, minimum); g != want {
		t.Errorf("saved %+v, loaded %+v", want, g)
	}
	for _, bad := range []string{`{"width":100,"height":100}`, `{"width":99999,"height":900}`, `garbage`} {
		if err := os.WriteFile(filepath.Join(dir, geometryFile), []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if g := LoadGeometry(dir, def, minimum); g != def {
			t.Errorf("%s: %+v", bad, g)
		}
	}
}
