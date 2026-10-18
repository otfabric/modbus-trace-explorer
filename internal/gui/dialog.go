// SPDX-License-Identifier: MIT

//go:build !server

package gui

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// saveFilters are the Save dialog's file types per export kind.
var saveFilters = map[string][2]string{
	"mbcap":  {"MBCAP capture", "*.mbcap"},
	"jsonl":  {"JSON Lines", "*.jsonl"},
	"csv":    {"CSV", "*.csv"},
	"pcapng": {"pcapng trace", "*.pcapng"},
	"json":   {"JSON", "*.json"},
}

// saveDialog is the native Save dialog; "" when the user cancels.
func saveDialog(name, kind string) (string, error) {
	app := application.Get()
	if app == nil {
		return "", service.Errorf(service.CodeInternal, "no application")
	}
	d := app.Dialog.SaveFile().SetFilename(name).CanCreateDirectories(true)
	if f, ok := saveFilters[kind]; ok {
		d = d.AddFilter(f[0], f[1])
	}
	path, err := d.AddFilter("All files", "*.*").PromptForSingleSelection()
	if err != nil {
		return "", service.Wrap(service.CodeInternal, err, "save dialog")
	}
	return path, nil
}

// openDialog is the native Open dialog for traces; nothing when cancelled.
func openDialog() ([]string, error) {
	app := application.Get()
	if app == nil {
		return nil, service.Errorf(service.CodeInternal, "no application")
	}
	paths, err := app.Dialog.OpenFile().SetTitle("Open trace").CanChooseFiles(true).
		AddFilter("Packet traces and MBCAP captures", "*.pcap;*.pcapng;*.cap;*.gz;*.mbcap;*.jsonl;*.csv").AddFilter("All files", "*.*").
		PromptForMultipleSelection()
	if err != nil {
		return nil, service.Wrap(service.CodeInternal, err, "open dialog")
	}
	return paths, nil
}
