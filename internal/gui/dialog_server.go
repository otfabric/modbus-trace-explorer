// SPDX-License-Identifier: MIT

//go:build server

package gui

import (
	"os"
	"path/filepath"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// Built with -tags server (Wails' server mode), the window is a browser
// page served on localhost: the README's screenshots and demo video are
// made this way (docs/media/record-demo.mjs). It is never shipped. There
// are no native dialogs: Save writes into $MTE_DEMO_SAVE_DIR under the
// proposed name, and Open offers nothing (traces are given on the command
// line).

func saveDialog(name, _ string) (string, error) {
	dir := os.Getenv("MTE_DEMO_SAVE_DIR")
	if dir == "" {
		return "", service.Errorf(service.CodeUnavailable, "no Save dialog in server mode: set MTE_DEMO_SAVE_DIR")
	}
	return filepath.Join(dir, filepath.Base(name)), nil
}

func openDialog() ([]string, error) { return nil, nil }
