// SPDX-License-Identifier: MIT

// Package gui binds the engine to the Wails window: the frontend calls the
// methods of App, and App tells the window which trace files the operating
// system handed over (a drop onto the window, "open with", the command
// line). It is the only package that imports Wails.
package gui

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// OpenEvent is the name of the Wails event that carries the paths of trace
// files to open ([]string), from a drop or from "open with".
const OpenEvent = "explorer:open"

// TraceExtensions are the file types the application opens, also the
// operating system's file associations.
var TraceExtensions = []string{".pcap", ".pcapng", ".cap", ".gz"}

// App is the Wails service of the application. Its exported methods are
// the frontend's whole view of the backend.
type App struct {
	svc service.Service

	mu      sync.Mutex
	pending []string // paths handed over before the frontend asked for them
}

// New creates the Wails service for svc. initial are the trace paths of
// the command line.
func New(svc service.Service, initial []string) *App {
	return &App{svc: svc, pending: Usable(initial)}
}

// MarshalError encodes the error of a bound method for the frontend as
// {"code","message"}.
func MarshalError(err error) []byte {
	var se *service.Error
	if !errors.As(err, &se) {
		se = &service.Error{Code: service.CodeInternal, Message: err.Error()}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			se.Code = service.CodeCanceled
		}
	}
	b, merr := json.Marshal(se)
	if merr != nil {
		return nil
	}
	return b
}

// About returns the versions of the application and its libraries.
func (a *App) About(ctx context.Context) (service.About, error) { return a.svc.About(ctx) }

// PendingFiles returns, once, the trace paths the operating system handed
// over before the window was ready (command line, "open with" at launch).
func (a *App) PendingFiles() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.pending
	a.pending = nil
	if p == nil {
		p = []string{}
	}
	return p
}

// ChooseTraces lets the user pick trace files with the native dialog; it
// returns nothing when the user cancels. Several files are one trace.
func (a *App) ChooseTraces() ([]string, error) {
	app := application.Get()
	if app == nil {
		return nil, service.Errorf(service.CodeInternal, "no application")
	}
	paths, err := app.Dialog.OpenFile().SetTitle("Open trace").CanChooseFiles(true).
		AddFilter("Packet traces", "*.pcap;*.pcapng;*.cap;*.gz").AddFilter("All files", "*.*").
		PromptForMultipleSelection()
	if err != nil {
		return nil, service.Wrap(service.CodeInternal, err, "open dialog")
	}
	return Usable(paths), nil
}

// OpenExternal opens an https link in the user's browser.
func (a *App) OpenExternal(link string) error {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return service.Errorf(service.CodeInvalidArgument, "only https links can be opened")
	}
	app := application.Get()
	if app == nil {
		return service.Errorf(service.CodeInternal, "no application")
	}
	return app.Browser.OpenURL(u.String())
}

// Handed is called with paths the operating system handed over while the
// application runs (a drop, "open with"); they are kept until the frontend
// asks and announced with OpenEvent.
func (a *App) Handed(paths []string) {
	paths = Usable(paths)
	if len(paths) == 0 {
		return
	}
	if app := application.Get(); app != nil {
		app.Event.Emit(OpenEvent, paths)
		return
	}
	a.mu.Lock()
	a.pending = append(a.pending, paths...)
	a.mu.Unlock()
}

// Usable keeps the paths that name regular files, made absolute with
// symbolic links resolved: the application opens only files the user chose,
// and never a directory or a device.
func Usable(paths []string) []string {
	out := []string{}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		if fi, err := os.Stat(abs); err == nil && fi.Mode().IsRegular() {
			out = append(out, abs)
		}
	}
	return out
}
