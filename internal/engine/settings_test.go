// SPDX-License-Identifier: MIT

package engine

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/applog"
	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// R20: settings are checked, kept across restarts, and applied (the cache
// cap at once, the server ports in the Flows step).
func TestSettings(t *testing.T) {
	cache, config := filepath.Join(t.TempDir(), "cache"), filepath.Join(t.TempDir(), "config")
	e, err := New(Options{CacheDir: cache, ConfigDir: config})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, err := e.Settings(ctx)
	if err != nil || s.TimeMode != "local" || len(s.ServerPorts) != 0 || s.CacheCap != store.DefaultCap {
		t.Fatalf("defaults %+v %v", s, err)
	}
	for name, bad := range map[string]service.Settings{
		"time mode": {TimeMode: "solar", CacheCap: MinCacheCap},
		"port 0":    {TimeMode: "utc", ServerPorts: []uint16{0}, CacheCap: MinCacheCap},
		"too small": {TimeMode: "utc", CacheCap: 1},
		"too many":  {TimeMode: "utc", ServerPorts: ports(40), CacheCap: MinCacheCap},
	} {
		if _, err := e.SaveSettings(ctx, bad); !isCode(err, service.CodeInvalidArgument) {
			t.Errorf("%s: %v", name, err)
		}
	}
	got, err := e.SaveSettings(ctx, service.Settings{TimeMode: "utc", ServerPorts: []uint16{1502, 503, 1502}, OneBased: true, CacheCap: 512 << 20})
	if err != nil || !slices.Equal(got.ServerPorts, []uint16{503, 1502}) || !got.OneBased || got.CacheCap != 512<<20 {
		t.Fatalf("%+v %v", got, err)
	}
	if fi, err := os.Stat(filepath.Join(config, settingsFile)); err != nil || fi.Mode().Perm() != store.FileMode {
		t.Errorf("settings file: %v %v", fi, err)
	}
	path := writeTrace(t, "s.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	_, res := importTrace(t, e, nil, path)
	if !slices.Contains(res.ServerPorts, 503) || !slices.Contains(res.ServerPorts, 502) {
		t.Errorf("Flows step ports %v", res.ServerPorts)
	}
	_ = e.Shutdown()

	e2, err := New(Options{CacheDir: cache, ConfigDir: config})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e2.Shutdown() })
	if s, _ := e2.Settings(ctx); s.TimeMode != "utc" || s.CacheCap != 512<<20 {
		t.Errorf("after a restart %+v", s)
	}
	info, err := e2.CacheInfo(ctx)
	if err != nil || info.Entries == 0 || info.Bytes == 0 || info.Dir != cache || info.Cap != 512<<20 {
		t.Errorf("cache %+v %v", info, err)
	}
	if info, err = e2.ClearCache(ctx); err != nil || info.Entries != 0 {
		t.Errorf("cleared %+v %v", info, err)
	}
	if r, _ := e2.Recent(ctx); len(r) != 1 {
		t.Errorf("recent %v", r)
	}
	for range 2 {
		if err := e2.ClearRecent(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if r, _ := e2.Recent(ctx); len(r) != 0 {
		t.Errorf("recent after clear %v", r)
	}
}

func ports(n int) []uint16 {
	out := make([]uint16, n)
	for i := range out {
		out[i] = uint16(1000 + i)
	}
	return out
}

// logBuf collects log lines written from the job goroutines.
type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// R23: the warnings of a failed live snapshot and of a failed eviction
// say what failed, at info level without the paths of the cache.
func TestWarningsHaveNoPaths(t *testing.T) {
	ctx := context.Background()
	t.Run("live snapshot", func(t *testing.T) {
		paceLive(t)
		var log logBuf
		root := t.TempDir()
		e, err := New(Options{CacheDir: filepath.Join(root, "cache"), ConfigDir: filepath.Join(root, "config"), Logger: applog.New(&log, slog.LevelInfo)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = e.Shutdown() })
		info, _ := e.Open(ctx, []string{bigTrace(t, 10_000)})
		events, _ := e.Subscribe(t.Context())
		ref, _ := e.Scan(ctx, info.ID)
		waitJob(t, events, ref.ID)
		res, _ := e.ScanResult(ctx, info.ID)
		if err := os.WriteFile(e.cache.LiveDir(info.ID, 1), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		ref, _ = e.Analyze(ctx, info.ID, service.AnalyzeConfig{Flows: res.Selected, ServerPorts: res.ServerPorts})
		if j := waitJob(t, events, ref.ID); j.State != service.JobDone {
			t.Fatalf("analysis %+v", j)
		}
		if got := log.String(); !strings.Contains(got, "live snapshot failed") || !strings.Contains(got, "<path>") || strings.Contains(got, root) {
			t.Errorf("the log:\n%s", got)
		}
	})
	t.Run("eviction", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs a directory whose files cannot be removed: Unix permissions, not as root")
		}
		var log logBuf
		root := t.TempDir()
		cache := filepath.Join(root, "cache")
		e, err := New(Options{CacheDir: cache, ConfigDir: filepath.Join(root, "config"), Logger: applog.New(&log, slog.LevelInfo)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = e.Shutdown() })
		// An entry over the smallest cap that cannot be removed: a sparse file
		// in a directory without write permission.
		locked := filepath.Join(cache, "ix-0123456789abcdef0123456789abcdef", "locked")
		if err := os.MkdirAll(locked, 0o700); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(locked, "col.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(f.Truncate(MinCacheCap+1), f.Close(), os.Chmod(locked, 0o500)); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
		if _, err := e.SaveSettings(ctx, service.Settings{TimeMode: "local", ServerPorts: []uint16{}, CacheCap: MinCacheCap}); err != nil {
			t.Fatal(err)
		}
		if got := log.String(); !strings.Contains(got, "cache eviction failed") || !strings.Contains(got, "<path>") || strings.Contains(got, root) {
			t.Errorf("the log:\n%s", got)
		}
	})
}
