// SPDX-License-Identifier: MIT

// Package policy holds tests over the dependency graph (A12): the engine
// and everything it is built from make no network connections and no
// Modbus I/O. Only the Wails shell (cmd/, internal/gui) links a network
// stack, which the Wails runtime uses to serve the window its assets.
package policy

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

type pkg struct {
	ImportPath string
	Standard   bool
	CgoFiles   []string
}

// The packages that must stay local and pure Go.
var local = []string{
	"github.com/otfabric/modbus-trace-explorer/internal/engine/...",
	"github.com/otfabric/modbus-trace-explorer/internal/service/...",
	"github.com/otfabric/modbus-trace-explorer/internal/store/...",
	"github.com/otfabric/modbus-trace-explorer/internal/buildinfo/...",
}

// Allowed from the network area of the standard library: address types
// and URL parsing, which open nothing.
var allowedNet = map[string]bool{"net/netip": true, "net/url": true}

func TestNoNetworkNoModbusNoCgo(t *testing.T) {
	out, err := exec.Command("go", append([]string{"list", "-deps", "-json"}, local...)...).Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		switch {
		case (p.ImportPath == "net" || strings.HasPrefix(p.ImportPath, "net/")) && !allowedNet[p.ImportPath]:
			t.Errorf("%s: the engine makes no network connections (R17)", p.ImportPath)
		case p.ImportPath == "github.com/otfabric/go-modbus" || strings.HasPrefix(p.ImportPath, "github.com/otfabric/go-modbus/"):
			t.Errorf("%s: no Modbus I/O", p.ImportPath)
		case strings.HasPrefix(p.ImportPath, "github.com/wailsapp/"):
			t.Errorf("%s: only internal/gui and cmd/ import Wails", p.ImportPath)
		case !p.Standard && len(p.CgoFiles) > 0:
			t.Errorf("%s: cgo outside the Wails shell", p.ImportPath)
		}
	}
}
