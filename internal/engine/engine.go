// SPDX-License-Identifier: MIT

// Package engine implements the Service over go-modbus-trace and the
// analysis cache (internal/store): it opens traces, runs the scan and the
// analysis as cancellable jobs, writes the index, and answers the user
// interface's queries from it, going back to the trace only for the bytes
// of a row (docs/ARCHITECTURE.md sections 4 to 7).
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/otfabric/go-modbus-trace/model"
	"github.com/otfabric/go-modbus-trace/trace"

	"github.com/otfabric/modbus-trace-explorer/internal/buildinfo"
	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// Options configure an Engine.
type Options struct {
	Logger *slog.Logger
	// CacheDir is the analysis cache (default os.UserCacheDir()/modbus-trace-explorer).
	CacheDir string
	// CacheCap is the cache's size cap (default store.DefaultCap).
	CacheCap int64
	// ConfigDir keeps the recent files (default os.UserConfigDir()/modbus-trace-explorer).
	ConfigDir string
	// Now is the clock for the cache's last-use times and job rates.
	Now func() time.Time
}

// Engine is the application's Service. It is safe for concurrent use.
type Engine struct {
	log    *slog.Logger
	cache  *store.Cache
	config string
	now    func() time.Time
	lib    string

	mu     sync.Mutex
	files  map[string]*openTrace
	jobs   map[string]*job
	nextID int
	subs   map[chan service.Event]struct{}
}

var _ service.Service = (*Engine)(nil)

// openTrace is a trace the user has open.
type openTrace struct {
	info    service.FileInfo
	data    []string // what the library reads: the paths, or decompressed copies
	release func()   // lets the cache evict this trace's entries again

	mu    sync.Mutex
	lib   *trace.File // random access for Detail, opened on first use
	scan  *service.ScanResult
	ix    *store.Index
	conns map[uint32]model.Connection
	sum   *trace.FileSummary
	busy  bool // a scan or an analysis runs
}

// DefaultCacheDir returns the per-user cache location.
func DefaultCacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "modbus-trace-explorer")
}

// DefaultConfigDir returns the per-user settings location.
func DefaultConfigDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "modbus-trace-explorer")
}

// New creates an Engine; it fails when the cache directory cannot be made.
func New(o Options) (*Engine, error) {
	log := o.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	if o.CacheDir == "" {
		o.CacheDir = DefaultCacheDir()
	}
	if o.ConfigDir == "" {
		o.ConfigDir = DefaultConfigDir()
	}
	cache, err := store.NewCache(o.CacheDir, o.CacheCap, now)
	if err != nil {
		return nil, err
	}
	return &Engine{log: log, cache: cache, config: o.ConfigDir, now: now, lib: buildinfo.ModuleVersion(buildinfo.GoModbusTracePath),
		files: map[string]*openTrace{}, jobs: map[string]*job{}, subs: map[chan service.Event]struct{}{}}, nil
}

// Shutdown cancels every job and releases every open trace.
func (e *Engine) Shutdown() error {
	e.mu.Lock()
	jobs := make([]*job, 0, len(e.jobs))
	for _, j := range e.jobs {
		jobs = append(jobs, j)
	}
	files := e.files
	e.files = map[string]*openTrace{}
	e.mu.Unlock()
	for _, j := range jobs {
		j.cancel()
		<-j.done
	}
	var err error
	for _, t := range files {
		err = errors.Join(err, t.close())
	}
	return err
}

func (t *openTrace) close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var err error
	if t.ix != nil {
		err = t.ix.Close()
		t.ix = nil
	}
	if t.release != nil {
		t.release()
	}
	return err
}

// About returns the versions in this build.
func (e *Engine) About(context.Context) (service.About, error) {
	return service.About{
		Version:       buildinfo.Version,
		GoModbusTrace: e.lib,
		Wails:         buildinfo.ModuleVersion(buildinfo.WailsPath),
		Go:            runtime.Version(),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
	}, nil
}

// Open prepares a trace: its fingerprint, and the cached analysis if there
// is one.
func (e *Engine) Open(_ context.Context, paths []string) (service.FileInfo, error) {
	if len(paths) == 0 {
		return service.FileInfo{}, service.Errorf(service.CodeInvalidArgument, "no trace file")
	}
	var size int64
	gz := false
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return service.FileInfo{}, service.Wrap(service.CodeNotFound, err, "cannot open %s", filepath.Base(p))
		}
		if !fi.Mode().IsRegular() {
			return service.FileInfo{}, service.Errorf(service.CodeInvalidArgument, "%s is not a file", filepath.Base(p))
		}
		size += fi.Size()
		if g, err := store.IsGzip(p); err != nil {
			return service.FileInfo{}, service.Wrap(service.CodeUnavailable, err, "cannot read %s", filepath.Base(p))
		} else if g {
			gz = true
		}
	}
	fp, err := store.Fingerprint(paths, e.lib, model.EventSemanticsVersion)
	if err != nil {
		return service.FileInfo{}, service.Wrap(service.CodeUnavailable, err, "cannot read the trace")
	}
	name := filepath.Base(paths[0])
	if len(paths) > 1 {
		name += " + " + strconv.Itoa(len(paths)-1) + " more"
	}
	info := service.FileInfo{ID: fp, Paths: slices.Clone(paths), Name: name, Size: size, Gzip: gz}
	t := &openTrace{info: info, data: slices.Clone(paths), release: e.cache.Hold(fp)}
	if ix, err := e.cache.Lookup(fp); err == nil {
		if err := t.attach(ix); err == nil {
			info.Analyzed = true
			var cfg service.AnalyzeConfig
			if json.Unmarshal(ix.Meta.Choices, &cfg) == nil {
				info.Choices = &cfg
			}
		} else {
			_ = ix.Close()
		}
	}
	t.info = info
	e.mu.Lock()
	old := e.files[fp]
	e.files[fp] = t
	e.mu.Unlock()
	if old != nil {
		_ = old.close()
	}
	e.remember(paths)
	e.log.Info("trace opened", "files", len(paths), "analyzed", info.Analyzed) // no paths at info level (R23)
	return info, nil
}

// attach makes ix the trace's index, with the documents written beside it.
func (t *openTrace) attach(ix *store.Index) error {
	var sum trace.FileSummary
	if err := readJSON(filepath.Join(ix.Dir(), "summary.json"), &sum); err != nil {
		return err
	}
	var conns []model.Connection
	if err := readJSON(filepath.Join(ix.Dir(), "conns.json"), &conns); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ix != nil {
		_ = t.ix.Close()
	}
	t.ix, t.sum = ix, &sum
	t.conns = map[uint32]model.Connection{}
	for _, c := range conns {
		t.conns[c.ID] = c
	}
	return nil
}

// Close releases an open trace and cancels its jobs.
func (e *Engine) Close(_ context.Context, id string) error {
	e.mu.Lock()
	t := e.files[id]
	delete(e.files, id)
	var mine []*job
	for _, j := range e.jobs {
		if j.state.File == id {
			mine = append(mine, j)
		}
	}
	e.mu.Unlock()
	if t == nil {
		return service.Errorf(service.CodeNotFound, "no open trace %s", id)
	}
	for _, j := range mine {
		j.cancel()
		<-j.done
	}
	return t.close()
}

func (e *Engine) trace(id string) (*openTrace, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.files[id]
	if t == nil {
		return nil, service.Errorf(service.CodeNotFound, "the trace is not open")
	}
	return t, nil
}

// index returns the open trace's index, or an error when it has none yet.
func (e *Engine) index(id string) (*openTrace, *store.Index, error) {
	t, err := e.trace(id)
	if err != nil {
		return nil, nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ix == nil {
		return nil, nil, service.Errorf(service.CodeNotFound, "the trace is not analysed yet")
	}
	return t, t.ix, nil
}

const maxRecent = 10

type recentFile struct {
	Paths []string `json:"paths"`
}

// remember puts paths first in the recent files list.
func (e *Engine) remember(paths []string) {
	list := e.readRecent()
	list = slices.DeleteFunc(list, func(r recentFile) bool { return slices.Equal(r.Paths, paths) })
	list = append([]recentFile{{Paths: slices.Clone(paths)}}, list...)
	if len(list) > maxRecent {
		list = list[:maxRecent]
	}
	if err := os.MkdirAll(e.config, store.DirMode); err != nil {
		return
	}
	if b, err := json.Marshal(list); err == nil {
		_ = os.WriteFile(filepath.Join(e.config, "recent.json"), b, store.FileMode)
	}
}

func (e *Engine) readRecent() []recentFile {
	var list []recentFile
	_ = readJSON(filepath.Join(e.config, "recent.json"), &list)
	return list
}

// Recent lists the traces opened recently, most recent first.
func (e *Engine) Recent(context.Context) ([]service.RecentFile, error) {
	out := []service.RecentFile{}
	for _, r := range e.readRecent() {
		if len(r.Paths) == 0 {
			continue
		}
		exists := true
		for _, p := range r.Paths {
			if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
				exists = false
			}
		}
		name := filepath.Base(r.Paths[0])
		if len(r.Paths) > 1 {
			name += " + " + strconv.Itoa(len(r.Paths)-1) + " more"
		}
		out = append(out, service.RecentFile{Paths: r.Paths, Name: name, Exists: exists})
	}
	return out, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
