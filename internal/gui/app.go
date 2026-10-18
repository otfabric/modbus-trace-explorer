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

// Wails events the window listens to.
const (
	// OpenEvent carries the paths of trace files to open ([]string), from
	// a drop or from "open with".
	OpenEvent = "explorer:open"
	// JobEvent carries a service.Event: job progress and completion.
	JobEvent = "explorer:event"
)

// TraceExtensions are the file types the application opens, also the
// operating system's file associations.
var TraceExtensions = []string{".pcap", ".pcapng", ".cap", ".gz", ".mbcap"}

// App is the Wails service of the application. Its exported methods are
// the frontend's whole view of the backend.
type App struct {
	svc service.Service

	mu      sync.Mutex
	pending []string // paths handed over before the frontend asked for them
	cancel  context.CancelFunc
	done    chan struct{}
	smoke   func(error) // the smoke test's report (A15), nil outside it
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

// ServiceStartup starts forwarding the engine's events to the window.
func (a *App) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	ctx, cancel := context.WithCancel(context.Background())
	events, err := a.svc.Subscribe(ctx)
	if err != nil {
		cancel()
		return err
	}
	done := make(chan struct{})
	a.mu.Lock()
	a.cancel, a.done = cancel, done
	a.mu.Unlock()
	go func() {
		defer close(done)
		for ev := range events {
			if app := application.Get(); app != nil {
				app.Event.Emit(JobEvent, ev)
			}
		}
	}()
	return nil
}

// ServiceShutdown stops forwarding events.
func (a *App) ServiceShutdown() error {
	a.mu.Lock()
	cancel, done := a.cancel, a.done
	a.cancel, a.done = nil, nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	return nil
}

// About returns the versions of the application and its libraries.
func (a *App) About(ctx context.Context) (service.About, error) { return a.svc.About(ctx) }

// Open prepares a trace. The paths must be ones the operating system
// handed over (dialog, drop, "open with", command line) or from the recent
// files list; they are checked again here.
func (a *App) Open(ctx context.Context, paths []string) (service.FileInfo, error) {
	usable := Usable(paths)
	if len(usable) != len(paths) || len(paths) == 0 {
		return service.FileInfo{}, service.Errorf(service.CodeNotFound, "the trace file is not there (or is not a regular file)")
	}
	info, err := a.svc.Open(ctx, usable)
	if err == nil {
		a.RefreshMenu() // Open Recent follows the list
	}
	return info, err
}

// Close releases an open trace.
func (a *App) Close(ctx context.Context, id string) error { return a.svc.Close(ctx, id) }

// Recent lists the recently opened traces.
func (a *App) Recent(ctx context.Context) ([]service.RecentFile, error) { return a.svc.Recent(ctx) }

// Scan starts the first pass.
func (a *App) Scan(ctx context.Context, id string) (service.JobRef, error) {
	return a.svc.Scan(ctx, id)
}

// ScanResult returns the Flows step's content.
func (a *App) ScanResult(ctx context.Context, id string) (service.ScanResult, error) {
	return a.svc.ScanResult(ctx, id)
}

// ImportCapture starts the import of an MBCAP capture.
func (a *App) ImportCapture(ctx context.Context, id string) (service.JobRef, error) {
	return a.svc.ImportCapture(ctx, id)
}

// Prepare decompresses an analysed gzip trace again.
func (a *App) Prepare(ctx context.Context, id string) (service.JobRef, error) {
	return a.svc.Prepare(ctx, id)
}

// Analyze starts the analysis.
func (a *App) Analyze(ctx context.Context, id string, cfg service.AnalyzeConfig) (service.JobRef, error) {
	return a.svc.Analyze(ctx, id, cfg)
}

// CancelJob cancels a job.
func (a *App) CancelJob(ctx context.Context, id string) error { return a.svc.CancelJob(ctx, id) }

// Overview returns the file strip.
func (a *App) Overview(ctx context.Context, id string) (service.Overview, error) {
	return a.svc.Overview(ctx, id)
}

// Timeline returns a window of the timeline.
func (a *App) Timeline(ctx context.Context, id string, q service.TimelineQuery) (service.TimelinePage, error) {
	return a.svc.Timeline(ctx, id, q)
}

// Detail returns one row's detail.
func (a *App) Detail(ctx context.Context, id string, seq uint64) (service.Detail, error) {
	return a.svc.Detail(ctx, id, seq)
}

// Devices aggregates per server and unit.
func (a *App) Devices(ctx context.Context, id string, f service.Filter) ([]service.DeviceStats, error) {
	return a.svc.Devices(ctx, id, f)
}

// Connections lists the connections.
func (a *App) Connections(ctx context.Context, id string, f service.Filter) ([]service.ConnStats, error) {
	return a.svc.Connections(ctx, id, f)
}

// Patterns finds the recurring requests.
func (a *App) Patterns(ctx context.Context, id string, f service.Filter) ([]service.Pattern, error) {
	return a.svc.Patterns(ctx, id, f)
}

// Coverage maps a device's data space.
func (a *App) Coverage(ctx context.Context, id string, q service.CoverageQuery) (service.CoverageMap, error) {
	return a.svc.Coverage(ctx, id, q)
}

// AddressHistory starts reading one address's values.
func (a *App) AddressHistory(ctx context.Context, id string, q service.HistoryQuery) (service.JobRef, error) {
	return a.svc.AddressHistory(ctx, id, q)
}

// Quality lists the quality conditions.
func (a *App) Quality(ctx context.Context, id string) (service.Quality, error) {
	return a.svc.Quality(ctx, id)
}

// Locate finds a row of the timeline.
func (a *App) Locate(ctx context.Context, id string, q service.LocateQuery) (service.Location, error) {
	return a.svc.Locate(ctx, id, q)
}

// Diagnostics returns the "Copy diagnostics" text.
func (a *App) Diagnostics(ctx context.Context, id string) (string, error) {
	return a.svc.Diagnostics(ctx, id)
}

// ParseFilter parses the filter box.
func (a *App) ParseFilter(ctx context.Context, id, text string) (service.FilterParse, error) {
	return a.svc.ParseFilter(ctx, id, text)
}

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

// PreviewMBCAP binds Service.PreviewMBCAP.
func (a *App) PreviewMBCAP(ctx context.Context, id string, x service.MBCAPExport) (service.MBCAPPreview, error) {
	return a.svc.PreviewMBCAP(ctx, id, x)
}

// ExportMBCAP binds Service.ExportMBCAP.
func (a *App) ExportMBCAP(ctx context.Context, id string, x service.MBCAPExport, path string) (service.JobRef, error) {
	return a.svc.ExportMBCAP(ctx, id, x, path)
}

// ExportTimeline binds Service.ExportTimeline.
func (a *App) ExportTimeline(ctx context.Context, id string, x service.TimelineExport, path string) (service.JobRef, error) {
	return a.svc.ExportTimeline(ctx, id, x, path)
}

// ExportSubtrace binds Service.ExportSubtrace.
func (a *App) ExportSubtrace(ctx context.Context, id string, x service.SubtraceExport, path string) (service.JobRef, error) {
	return a.svc.ExportSubtrace(ctx, id, x, path)
}

// ExportResult binds Service.ExportResult.
func (a *App) ExportResult(ctx context.Context, job string) (service.ExportResult, error) {
	return a.svc.ExportResult(ctx, job)
}

// SaveReport binds Service.SaveReport.
func (a *App) SaveReport(ctx context.Context, job, path string) error {
	return a.svc.SaveReport(ctx, job, path)
}

// Settings binds Service.Settings.
func (a *App) Settings(ctx context.Context) (service.Settings, error) { return a.svc.Settings(ctx) }

// SaveSettings binds Service.SaveSettings.
func (a *App) SaveSettings(ctx context.Context, s service.Settings) (service.Settings, error) {
	return a.svc.SaveSettings(ctx, s)
}

// CacheInfo binds Service.CacheInfo.
func (a *App) CacheInfo(ctx context.Context) (service.CacheInfo, error) { return a.svc.CacheInfo(ctx) }

// ClearCache binds Service.ClearCache.
func (a *App) ClearCache(ctx context.Context) (service.CacheInfo, error) {
	return a.svc.ClearCache(ctx)
}

// ClearRecent binds Service.ClearRecent.
func (a *App) ClearRecent(ctx context.Context) error {
	err := a.svc.ClearRecent(ctx)
	a.RefreshMenu()
	return err
}

// ChooseSave asks where to save a file with the native dialog, proposing
// name; it returns "" when the user cancels.
func (a *App) ChooseSave(name, kind string) (string, error) { return saveDialog(name, kind) }

// ChooseTraces lets the user pick trace files with the native dialog; it
// returns nothing when the user cancels. Several files are one trace.
func (a *App) ChooseTraces() ([]string, error) {
	paths, err := openDialog()
	return Usable(paths), err
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
