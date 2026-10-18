// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/otfabric/go-modbus-trace/model"
)

// appendRows appends rows from..to of a simple poll, out of time order now
// and then, with provenance.
func appendRows(t *testing.T, w *Writer, from, to int) {
	t.Helper()
	for i := from; i < to; i++ {
		d, _ := w.Device([]string{"10.0.0.1:502", "10.0.0.2:502"}[i%2], 1)
		c, _ := w.Client("10.0.0.9")
		r := Row{Kind: KindTxn, TimeNS: int64(1000*i) - int64(i%3)*1500, Conn: 1, Device: d, Client: c, TID: uint16(i), FC: 3,
			Sub: NoSub, Class: Code(ClassNames[:], "read"), Status: Code(StatusNames[:], "ok"), LatUS: uint32(i % 97),
			Summary: w.Summary("Read 1 holding register @ " + string(rune('A'+i%5)))}
		tm := time.Unix(0, r.TimeNS)
		if err := w.Append(r, []model.FrameRef{{Frame: uint64(2*i + 1), Offset: int64(64 * i), Time: &tm, PayOff: 54, PayLen: 12}}, nil); err != nil {
			t.Fatal(err)
		}
	}
}

// A snapshot of an index being written is an Index of the rows so far:
// it stays right while the writer appends, a later snapshot sees more, and
// the finished index all of them.
func TestSnapshot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ix.partial")
	w, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	appendRows(t, w, 0, 1000)
	snap1 := filepath.Join(root, "ix.live-1")
	if err := w.Snapshot(snap1, Meta{Fingerprint: "fp"}); err != nil {
		t.Fatal(err)
	}
	live, err := OpenLive(dir, snap1)
	if err != nil {
		t.Fatal(err)
	}
	if live.Rows() != 1000 || !live.Meta.Live {
		t.Fatalf("%d rows, live %v", live.Rows(), live.Meta.Live)
	}
	first := live.Row(999)
	// The writer goes on; the snapshot is unchanged and still readable.
	appendRows(t, w, 1000, 5000)
	if r := live.Row(999); r != first {
		t.Errorf("row 999 changed under the snapshot: %+v", r)
	}
	req, _, err := live.Refs(live.Row(500))
	if err != nil || len(req) != 1 || req[0].Frame != 1001 {
		t.Errorf("refs %+v %v", req, err)
	}
	sel, err := live.Select(context.Background(), &Filter{Key: "dev0", Devices: []bool{true, false}}, ByTime)
	if err != nil || sel.Len() != 500 {
		t.Errorf("a filter on the snapshot: %v %v", sel, err)
	}
	snap2 := filepath.Join(root, "ix.live-2")
	if err := w.Snapshot(snap2, Meta{Fingerprint: "fp"}); err != nil {
		t.Fatal(err)
	}
	live2, err := OpenLive(dir, snap2)
	if err != nil || live2.Rows() != 5000 {
		t.Fatalf("second snapshot: %v", err)
	}
	all, _ := live2.Select(context.Background(), &Filter{}, ByTime)
	prev := int64(-1 << 62)
	for k := range all.Len() {
		r := live2.Row(all.At(k, false))
		if r.TimeNS < prev {
			t.Fatalf("time order broken at %d", k)
		}
		prev = r.TimeNS
	}
	// A snapshot is not an index, and an index is not a snapshot.
	if _, err := Open(snap1); err == nil {
		t.Error("a snapshot opened as an index")
	}
	_ = live.Close()
	_ = live2.Close()
	if err := os.RemoveAll(snap1); err != nil {
		t.Fatal(err)
	}
	appendRows(t, w, 5000, 6000)
	if err := w.Finish(Meta{Fingerprint: "fp"}, nil); err != nil {
		t.Fatal(err)
	}
	ix, err := Open(dir)
	if err != nil || ix.Rows() != 6000 || ix.Meta.Live {
		t.Fatalf("finished: %v", err)
	}
	if _, err := OpenLive(dir, dir); err == nil {
		t.Error("an index opened as a snapshot")
	}
	_ = ix.Close()
	if err := w.Snapshot(filepath.Join(root, "late"), Meta{}); err == nil {
		t.Error("a snapshot after Finish")
	}
}

// A snapshot into a directory that exists is refused, and the writer
// goes on.
func TestSnapshotExists(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ix.partial")
	w, _ := Create(dir)
	appendRows(t, w, 0, 10)
	if err := w.Snapshot(root, Meta{}); err == nil {
		t.Error("a snapshot over an existing directory")
	}
	appendRows(t, w, 10, 20)
	if err := w.Finish(Meta{}, nil); err != nil {
		t.Fatal(err)
	}
	ix, err := Open(dir)
	if err != nil || ix.Rows() != 20 {
		t.Fatalf("after a refused snapshot: %v", err)
	}
	_ = ix.Close()
}

// A snapshot that claims more rows than the files hold is refused.
func TestSnapshotShortFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ix.partial")
	w, _ := Create(dir)
	appendRows(t, w, 0, 10)
	snap := filepath.Join(root, "s")
	if err := w.Snapshot(snap, Meta{}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(snap, "meta.json"), Meta{Format: FormatVersion, Rows: 1 << 20, Complete: true, Live: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLive(dir, snap); err == nil {
		t.Error("a snapshot longer than its files")
	}
	_ = w.Abort()
}

// A writer's errors reach Snapshot and Finish: an earlier write error, a
// buffer that cannot be written, permutations and documents that cannot
// be saved.
func TestWriterErrors(t *testing.T) {
	root := t.TempDir()
	w, _ := Create(filepath.Join(root, "a.partial"))
	appendRows(t, w, 0, 10)
	w.err = errors.New("disk full")
	if err := w.Snapshot(filepath.Join(root, "a.live"), Meta{}); err == nil || err.Error() != "disk full" {
		t.Errorf("snapshot after a write error: %v", err)
	}
	if err := w.Finish(Meta{}, nil); err == nil {
		t.Error("finish after a write error")
	}
	w.err = nil
	_ = w.Abort()

	w, _ = Create(filepath.Join(root, "b.partial"))
	appendRows(t, w, 0, 10)
	_ = w.files[0].Close()
	if err := w.Snapshot(filepath.Join(root, "b.live"), Meta{}); err == nil {
		t.Error("a snapshot into a closed column file")
	}
	_ = w.Abort()

	if runtime.GOOS == "windows" {
		return // permissions below are POSIX
	}
	for _, ro := range []string{"perm", ""} {
		dir := filepath.Join(root, "c"+ro+".partial")
		w, _ = Create(dir)
		appendRows(t, w, 0, 10)
		_ = os.Chmod(filepath.Join(dir, ro), 0o500)
		if err := w.Finish(Meta{}, nil); err == nil {
			t.Errorf("finish with %q read-only", ro)
		}
		_ = os.Chmod(filepath.Join(dir, ro), DirMode)
		_ = w.Abort()
	}
}
