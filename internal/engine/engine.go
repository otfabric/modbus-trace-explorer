// SPDX-License-Identifier: MIT

// Package engine implements the Service over go-modbus-trace and the
// analysis cache. In M0 it answers the shell's questions; importing,
// querying and exporting follow with M1 to M3.
package engine

import (
	"context"
	"log/slog"
	"runtime"

	"github.com/otfabric/modbus-trace-explorer/internal/buildinfo"
	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// Options configure an Engine.
type Options struct {
	Logger *slog.Logger
}

// Engine is the application's Service.
type Engine struct {
	log *slog.Logger
}

var _ service.Service = (*Engine)(nil)

// New creates an Engine.
func New(o Options) *Engine {
	log := o.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Engine{log: log}
}

// Close releases what the engine holds.
func (e *Engine) Close() error { return nil }

// About returns the versions in this build.
func (e *Engine) About(context.Context) (service.About, error) {
	return service.About{
		Version:       buildinfo.Version,
		GoModbusTrace: buildinfo.ModuleVersion(buildinfo.GoModbusTracePath),
		Wails:         buildinfo.ModuleVersion(buildinfo.WailsPath),
		Go:            runtime.Version(),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
	}, nil
}
