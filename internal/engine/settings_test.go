// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/otfabric/go-modbus-trace/trace/tracetest"

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
