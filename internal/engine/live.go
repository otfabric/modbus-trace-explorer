// SPDX-License-Identifier: MIT

package engine

import (
	"os"
	"time"

	"github.com/otfabric/go-modbus-trace/model"

	"github.com/otfabric/modbus-trace-explorer/internal/applog"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// The timeline during the import (R21): while the analysis writes the
// index, snapshots of the rows written so far (store.Writer.Snapshot) are
// published as the trace's index, so the window can page, filter and open
// details before the end. Snapshots are taken at growing intervals: a
// second apart at least, and only once the rows have grown by half since
// the last, so sorting them stays a small share of the import. Each
// replaces the previous one without waiting for the readers of the
// previous one: an address history can read one for as long as it runs.
// A snapshot replaced is retired (store.Index.Retire): it is unmapped, and
// its directory removed, when its last reader releases it.

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
	last     time.Time
	lastRows uint64
	off      bool           // a snapshot failed: the import goes on without them
	retired  []*store.Index // without store.MovesMapped: the indexes this import replaced, for end
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
	// Numbered across the engine's imports: a snapshot of an import that
	// ended can outlive it while a reader holds it.
	dir := l.e.cache.LiveDir(l.t.info.ID, int(l.e.liveSeq.Add(1)))
	meta := store.Meta{Library: l.e.lib, Semantics: model.EventSemanticsVersion, Fingerprint: l.t.info.ID}
	var ix *store.Index
	err := l.w.Snapshot(dir, meta)
	if err == nil {
		ix, err = store.OpenLive(l.w.Dir(), dir)
	}
	if err != nil {
		// No path at info level (R23): the error's paths are in the debug line.
		l.e.log.Warn("live snapshot failed; the timeline appears at the end", applog.Err(err))
		l.e.log.Debug("live snapshot failed", "err", err)
		_ = os.RemoveAll(dir)
		l.off = true
		return
	}
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
		l.retire(old)
	}
	l.last, l.lastRows = l.e.now(), ix.Rows()
	l.j.setReady()
}

// retire lets go of an index this import replaced: it closes when its
// last reader releases it, and a snapshot's directory is removed then.
func (l *live) retire(ix *store.Index) {
	var after func()
	if ix.Meta.Live {
		dir := ix.Dir()
		after = func() { _ = os.RemoveAll(dir) }
	}
	ix.Retire(after)
	if !store.MovesMapped {
		l.retired = append(l.retired, ix) // end waits for it
	}
}

// end retires the published snapshot before the index is committed or
// abandoned. Where a directory with mapped files in it cannot be renamed
// or removed (Windows, store.MovesMapped), it then waits for the readers
// of every index the import replaced; elsewhere they finish on their own.
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
		l.retire(ix)
	}
	if !store.MovesMapped {
		for _, r := range l.retired {
			<-r.Closed()
		}
		l.retired = nil
	}
}
