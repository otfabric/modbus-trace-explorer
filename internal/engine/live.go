// SPDX-License-Identifier: MIT

package engine

import (
	"os"
	"time"

	"github.com/otfabric/go-modbus-trace/model"

	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// The timeline during the import (R21): while the analysis writes the
// index, snapshots of the rows written so far (store.Writer.Snapshot) are
// published as the trace's index, so the window can page, filter and open
// details before the end. Snapshots are taken at growing intervals: a
// second apart at least, and only once the rows have grown by half since
// the last, so sorting them stays a small share of the import. Each
// replaces the previous one; queries still reading the previous one
// finish first (store.Index.Close waits for them).

// liveEvery and liveMinRows pace the snapshots (variables for the tests).
var (
	liveEvery          = time.Second
	liveMinRows uint64 = 2048
)

// live publishes snapshots of an import.
type live struct {
	e        *Engine
	t        *openTrace
	j        *job
	w        *store.Writer
	conns    func() []model.Connection
	k        int
	last     time.Time
	lastRows uint64
	dirs     []string
	off      bool // a snapshot failed: the import goes on without them
}

func newLive(e *Engine, t *openTrace, j *job, w *store.Writer, conns func() []model.Connection) *live {
	return &live{e: e, t: t, j: j, w: w, conns: conns, last: e.now()}
}

// maybe publishes a snapshot when one is due.
func (l *live) maybe() {
	if l.off || l.e.now().Sub(l.last) < liveEvery {
		return
	}
	rows := l.w.Rows()
	if rows < max(liveMinRows, l.lastRows+l.lastRows/2) {
		return
	}
	l.publish()
}

func (l *live) publish() {
	l.k++
	dir := l.e.cache.LiveDir(l.t.info.ID, l.k)
	meta := store.Meta{Library: l.e.lib, Semantics: model.EventSemanticsVersion, Fingerprint: l.t.info.ID}
	if err := l.w.Snapshot(dir, meta); err != nil {
		l.e.log.Warn("live snapshot failed; the timeline appears at the end", "err", err)
		_ = os.RemoveAll(dir)
		l.off = true
		return
	}
	ix, err := store.OpenLive(l.w.Dir(), dir)
	if err != nil {
		l.e.log.Warn("live snapshot unreadable; the timeline appears at the end", "err", err)
		_ = os.RemoveAll(dir)
		l.off = true
		return
	}
	l.dirs = append(l.dirs, dir)
	conns := map[uint32]model.Connection{}
	for _, c := range l.conns() {
		conns[c.ID] = c
	}
	t := l.t
	t.mu.Lock()
	old := t.ix
	t.ix, t.sum, t.capture, t.conns = ix, nil, nil, conns
	t.mu.Unlock()
	if old != nil {
		_ = old.Close() // waits for the queries still reading it
	}
	l.removeOld()
	l.last, l.lastRows = l.e.now(), ix.Rows()
	l.j.setReady()
}

// removeOld removes the snapshots before the current one.
func (l *live) removeOld() {
	if len(l.dirs) < 2 {
		return
	}
	for _, d := range l.dirs[:len(l.dirs)-1] {
		_ = os.RemoveAll(d)
	}
	l.dirs = l.dirs[len(l.dirs)-1:]
}

// end closes the published snapshot and removes the snapshots, before
// the index is committed or abandoned (a directory with mapped files in it
// cannot be renamed on Windows).
func (l *live) end() {
	t := l.t
	t.mu.Lock()
	ix := t.ix
	if ix != nil && ix.Meta.Live {
		t.ix = nil
	} else {
		ix = nil
	}
	t.mu.Unlock()
	if ix != nil {
		_ = ix.Close()
	}
	for _, d := range l.dirs {
		_ = os.RemoveAll(d)
	}
	l.dirs = nil
}
