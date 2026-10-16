// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// Bounds of the settings.
const (
	MinCacheCap    = 256 << 20
	MaxCacheCap    = 1 << 40
	maxServerPorts = 32
)

const settingsFile = "settings.json"

// readSettings returns the stored settings and whether there were any.
func (e *Engine) readSettings() (service.Settings, bool) {
	var s service.Settings
	if err := readJSON(filepath.Join(e.config, settingsFile), &s); err != nil {
		return s, false
	}
	return s, true
}

// defaultPorts are the server ports the user added in Settings.
func (e *Engine) defaultPorts() []uint16 {
	s, _ := e.readSettings()
	return s.ServerPorts
}

// Settings returns the user's settings, with defaults for what was never set.
func (e *Engine) Settings(context.Context) (service.Settings, error) {
	s, _ := e.readSettings()
	if s.TimeMode == "" {
		s.TimeMode = "local"
	}
	if s.ServerPorts == nil {
		s.ServerPorts = []uint16{}
	}
	s.CacheCap = e.cache.Cap()
	return s, nil
}

// SaveSettings checks and stores the settings; a new cache cap applies at
// once (evicting what no longer fits and is not in use).
func (e *Engine) SaveSettings(ctx context.Context, s service.Settings) (service.Settings, error) {
	if !slices.Contains([]string{"local", "utc", "relative"}, s.TimeMode) {
		return service.Settings{}, service.Errorf(service.CodeInvalidArgument, "time display %q: local, utc or relative", s.TimeMode)
	}
	ports := []uint16{}
	for _, p := range s.ServerPorts {
		if p == 0 {
			return service.Settings{}, service.Errorf(service.CodeInvalidArgument, "server port 0: 1 to 65535")
		}
		if !slices.Contains(ports, p) {
			ports = append(ports, p)
		}
	}
	if len(ports) > maxServerPorts {
		return service.Settings{}, service.Errorf(service.CodeInvalidArgument, "at most %d server ports", maxServerPorts)
	}
	slices.Sort(ports)
	s.ServerPorts = ports
	if s.CacheCap < MinCacheCap || s.CacheCap > MaxCacheCap {
		return service.Settings{}, service.Errorf(service.CodeInvalidArgument, "cache size: 256 MiB to 1 TiB")
	}
	if err := os.MkdirAll(e.config, store.DirMode); err != nil {
		return service.Settings{}, service.Wrap(service.CodeUnavailable, err, "cannot save the settings")
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return service.Settings{}, service.Wrap(service.CodeInternal, err, "cannot save the settings")
	}
	if err := writeAtomic(filepath.Join(e.config, settingsFile), store.FileMode, func(w io.Writer) error { _, err := w.Write(b); return err }); err != nil {
		return service.Settings{}, err
	}
	if s.CacheCap != e.cache.Cap() {
		e.cache.SetCap(s.CacheCap)
		if err := e.cache.Evict(); err != nil {
			e.log.Warn("cache eviction failed", "err", err)
		}
	}
	return e.Settings(ctx)
}

// CacheInfo returns the analysis cache's location and size.
func (e *Engine) CacheInfo(context.Context) (service.CacheInfo, error) {
	i := e.cache.Info()
	return service.CacheInfo{Dir: i.Root, Bytes: i.Bytes, Cap: i.Cap, Entries: i.Entries}, nil
}

// ClearCache removes every entry not held by an open trace.
func (e *Engine) ClearCache(ctx context.Context) (service.CacheInfo, error) {
	if err := e.cache.Clear(); err != nil {
		return service.CacheInfo{}, service.Wrap(service.CodeUnavailable, err, "cannot clear the cache")
	}
	e.log.Info("cache cleared")
	return e.CacheInfo(ctx)
}

// ClearRecent empties the recent files list.
func (e *Engine) ClearRecent(context.Context) error {
	err := os.Remove(filepath.Join(e.config, "recent.json"))
	if err != nil && !os.IsNotExist(err) {
		return service.Wrap(service.CodeUnavailable, err, "cannot clear the recent files")
	}
	return nil
}
