// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-modbus-trace/model"
)

// build writes n rows: device i%3, client i%2, FC03 except every 10th row
// a write (FC06), every 7th an exception, times decreasing every 5th row
// (determination order is not time order), latency n-i.
func build(t *testing.T, n int) (*Index, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ix")
	w, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		d, _ := w.Device([]string{"10.0.0.1:502", "10.0.0.2:502", "10.0.0.3:1502"}[i%3], uint8(1+i%3))
		c, _ := w.Client([]string{"10.0.0.9", "10.0.0.8"}[i%2])
		r := Row{Kind: KindTxn, TimeNS: int64(1000 * i), Conn: uint32(1 + i%3), Device: d, Client: c, TID: uint16(i), FC: 3,
			Sub: NoSub, Class: Code(ClassNames[:], "read"), Status: Code(StatusNames[:], "ok"), Space: Code(SpaceNames[:], "holding"),
			Start: uint16(100 * (i % 4)), Count: 10, LatUS: uint32(n - i), Summary: w.Summary("Read 10 holding registers")}
		if i%5 == 0 {
			r.TimeNS -= 2500
		}
		if i%10 == 0 {
			r.FC, r.Class, r.Summary = 6, Code(ClassNames[:], "write"), w.Summary("Write register")
		}
		if i%7 == 0 {
			r.Status, r.Exc = Code(StatusNames[:], "exception"), 2
		}
		if i%11 == 0 {
			r.Flags = Flags([]string{"retransmitted", "something_new"})
		}
		tm := time.Unix(0, r.TimeNS).UTC()
		req := []model.FrameRef{{Frame: uint64(2*i + 1), Offset: int64(100 * i), Iface: 1, Time: &tm, PayOff: 54, PayLen: 12}}
		resp := []model.FrameRef{{Part: 1, Frame: uint64(2*i + 2), Offset: int64(100*i + 50), PayOff: -1, PayLen: 29},
			{Frame: uint64(2*i + 3), Offset: int64(100*i + 80), PayOff: 54, PayLen: 1}}
		if err := w.Append(r, req, resp); err != nil {
			t.Fatal(err)
		}
	}
	gap, _ := w.Extra(map[string]any{"bytes": 1460})
	if err := w.Append(Row{Kind: KindGap, TimeNS: 5_000_000, Conn: 2, Device: 1, Extra: gap, Sub: NoSub, LatUS: NoLatency}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Finish(Meta{Library: "test", Fingerprint: "fp"}, map[string]any{"summary.json": map[string]int{"packets": 3}}); err != nil {
		t.Fatal(err)
	}
	ix, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return ix, dir
}

func TestRoundTrip(t *testing.T) {
	ix, dir := build(t, 1000)
	if ix.Rows() != 1001 || len(ix.Devices) != 3 || len(ix.Clients) != 2 || ix.Meta.Library != "test" || !ix.Meta.Complete {
		t.Fatalf("%+v", ix.Meta)
	}
	r := ix.Row(70)
	if r.FC != 6 || StatusNames[r.Status] != "exception" || r.Exc != 2 || r.TID != 70 || r.LatUS != 930 || ix.SummaryOf(r) != "Write register" {
		t.Errorf("row 70: %+v", r)
	}
	if got := FlagList(ix.Row(11).Flags); len(got) != 2 || got[0] != "retransmitted" || got[1] != "other" {
		t.Errorf("flags %v", got)
	}
	req, resp, err := ix.Refs(ix.Row(5))
	if err != nil || len(req) != 1 || len(resp) != 2 || req[0].Frame != 11 || req[0].Time == nil || resp[0].Part != 1 || resp[0].PayOff != -1 || resp[1].PayLen != 1 {
		t.Errorf("refs %+v %+v %v", req, resp, err)
	}
	if string(ix.Extra(ix.Row(1000))) != `{"bytes":1460}` || ix.Extra(ix.Row(1)) != nil {
		t.Error("extra")
	}
	for _, p := range []string{dir, filepath.Join(dir, "col"), filepath.Join(dir, "col", "t_ns.bin"), filepath.Join(dir, "meta.json")} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		want := FileMode
		if fi.IsDir() {
			want = DirMode
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s: %v", p, fi.Mode().Perm())
		}
	}
	if RowBytes() > 64 {
		t.Errorf("%d bytes per row", RowBytes())
	}
}

func TestSelectAndPage(t *testing.T) {
	ix, _ := build(t, 1000)
	ctx := context.Background()
	all, _ := ix.Select(ctx, &Filter{}, ByTime)
	if all.Len() != 1001 {
		t.Fatal(all.Len())
	}
	for k := uint64(1); k < all.Len(); k++ {
		if ix.t[all.At(k, false)] < ix.t[all.At(k-1, false)] {
			t.Fatalf("time order broken at %d", k)
		}
	}
	if ix.t[all.At(0, true)] < ix.t[all.At(1, true)] {
		t.Error("descending")
	}
	writes := &Filter{Classes: []bool{false, false, true}, Key: "w"}
	sel, _ := ix.Select(ctx, writes, ByTime)
	if sel.Len() != 100 {
		t.Errorf("%d writes", sel.Len())
	}
	again, _ := ix.Select(ctx, writes, ByTime)
	if again != sel {
		t.Error("not cached")
	}
	dev := make([]bool, 3)
	dev[1] = true
	sel, _ = ix.Select(ctx, &Filter{Devices: dev}, ByTime)
	if sel.Len() != 334 { // 333 transactions and the gap
		t.Errorf("device 1: %d", sel.Len())
	}
	sel, _ = ix.Select(ctx, &Filter{Devices: dev, Statuses: []bool{false, false, true}}, ByTime)
	for k := range sel.Len() {
		if r := ix.Row(sel.At(k, false)); r.Kind != KindTxn || r.Status != 2 || r.Device != 1 {
			t.Fatalf("%+v", r)
		}
	}
	from, to := int64(100_000), int64(200_000)
	sel, _ = ix.Select(ctx, &Filter{FromNS: &from, ToNS: &to}, ByTime)
	if sel.Len() == 0 || ix.t[sel.At(0, false)] < from || ix.t[sel.At(sel.Len()-1, false)] >= to {
		t.Error("time window")
	}
	sel, _ = ix.Select(ctx, &Filter{Space: Code(SpaceNames[:], "holding"), Addr: true, AddrFrom: 205, AddrTo: 205}, ByTime)
	for k := range sel.Len() {
		if ix.Row(sel.At(k, false)).Start != 200 {
			t.Fatal("address range")
		}
	}
	sel, _ = ix.Select(ctx, &Filter{FlagsAll: 1, FlagsNone: 0}, ByTime)
	if sel.Len() != 91 {
		t.Errorf("flagged %d", sel.Len())
	}
	sel, _ = ix.Select(ctx, &Filter{FlagsNone: 1, Functions: func() []bool { f := make([]bool, 256); f[6] = true; return f }()}, ByTime)
	if sel.Len() != 100-10 { // writes without the flag: every 110th row has both
		t.Errorf("unflagged writes %d", sel.Len())
	}
	exc := make([]bool, 256)
	exc[2] = true
	sel, _ = ix.Select(ctx, &Filter{Exc: exc}, ByTime)
	if sel.Len() != 143 {
		t.Errorf("exceptions with code 2: %d", sel.Len())
	}
	sel, _ = ix.Select(ctx, &Filter{Addr: true, AddrFrom: 0, AddrTo: 9}, ByTime)
	if sel.Len() != 250 {
		t.Errorf("addresses 0-9 in any space: %d", sel.Len())
	}
	lat, _ := ix.Select(ctx, &Filter{Kinds: []bool{true}}, ByLatency)
	if ix.lat[lat.At(0, false)] != 1 || ix.lat[lat.At(0, true)] != 1000 {
		t.Error("latency order")
	}
	if r, ok := lat.Rank(lat.At(5, false), ix, ByLatency, false); !ok || r != 5 {
		t.Error("rank")
	}
	if k := ix.TimeRank(all, 500_000); ix.t[all.At(k, false)] < 500_000 || (k > 0 && ix.t[all.At(k-1, false)] >= 500_000) {
		t.Error("time rank")
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ix.Select(cctx, &Filter{Classes: []bool{true}}, ByTime); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
}

// cachedBytes returns the heap the selection cache holds.
func (ix *Index) cachedBytes() (n, entries int) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for _, c := range ix.cache {
		n += 4 * cap(c.sel.rows)
	}
	return n, len(ix.cache)
}

// SECURITY.md "Limits": the cached selections hold at most the cache's
// bytes together, and at most six; the newest is kept whatever its size,
// so asking again for it does not scan.
func TestSelectionCacheBytes(t *testing.T) {
	ix, _ := build(t, 50_000)
	all := func(k int) *Filter { return &Filter{Kinds: []bool{true}, Key: "k" + strconv.Itoa(k)} }
	if _, err := ix.Select(context.Background(), all(-1), ByTime); err != nil {
		t.Fatal(err)
	}
	one, _ := ix.cachedBytes() // every selection below holds the same
	for _, c := range []struct {
		name   string
		budget int
		want   int // entries kept
	}{
		{"no room", 0, 1},
		{"room for one", one, 1},
		{"room for one and a byte less than two", 2*one - 1, 1},
		{"room for two", 2 * one, 2},
		{"room for three", 3 * one, 3},
		{"room for many", 1 << 30, maxCached},
	} {
		t.Run(c.name, func(t *testing.T) {
			was := selectionCacheBytes
			selectionCacheBytes = c.budget
			t.Cleanup(func() { selectionCacheBytes = was })
			ix.mu.Lock()
			ix.cache = nil
			ix.mu.Unlock()
			for k := range 8 {
				sel, err := ix.Select(context.Background(), all(k), ByTime)
				if err != nil || sel.Len() != 50_000 {
					t.Fatalf("select %d: %v", k, err)
				}
				n, entries := ix.cachedBytes()
				if entries != min(k+1, c.want) || (entries > 1 && n > c.budget) {
					t.Fatalf("after %d selections: %d cached, %d bytes, budget %d", k+1, entries, n, c.budget)
				}
				if again, _ := ix.Select(context.Background(), all(k), ByTime); again != sel {
					t.Fatalf("selection %d is not the cached one", k)
				}
			}
		})
	}
}

func TestOpenRefusesIncomplete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ix")
	w, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Append(Row{Kind: KindTxn}, nil, nil)
	if _, err := Open(dir); !errors.Is(err, ErrFormat) {
		t.Errorf("incomplete: %v", err)
	}
	if err := w.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("abort left the directory")
	}
	if _, err := Create(filepath.Join(t.TempDir())); err == nil {
		t.Error("created over an existing directory")
	}
	// A damaged column (wrong size) is refused.
	_, dir2 := build(t, 10)
	if err := os.WriteFile(filepath.Join(dir2, "col", "fc.bin"), []byte{1}, FileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir2); !errors.Is(err, ErrFormat) {
		t.Errorf("damaged: %v", err)
	}
}

func TestEmptyIndex(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ix")
	w, _ := Create(dir)
	if err := w.Finish(Meta{}, nil); err != nil {
		t.Fatal(err)
	}
	ix, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ix.Close() }()
	sel, _ := ix.Select(context.Background(), &Filter{Classes: []bool{true}, Key: "x"}, ByTime)
	if ix.Rows() != 0 || sel.Len() != 0 {
		t.Error("not empty")
	}
}

// SECURITY.md "Limits": the device and client dictionaries hold 65,536
// entries each (index 0 to 65,535), an index MaxRows rows; one more is
// refused with a LimitError that names the limit, and an entry already in
// a full dictionary is still found.
func TestLimits(t *testing.T) {
	for _, c := range []struct {
		what string
		max  int
		add  func(w *Writer, i int) (uint16, error)
	}{
		{"devices", MaxDevices, func(w *Writer, i int) (uint16, error) {
			return w.Device("10.0.0.1:"+strconv.Itoa(i>>8), uint8(i))
		}},
		{"clients", MaxClients, func(w *Writer, i int) (uint16, error) {
			return w.Client("10.1." + strconv.Itoa(i>>8) + "." + strconv.Itoa(i&0xFF) + "/" + strconv.Itoa(i>>16))
		}},
	} {
		t.Run(c.what, func(t *testing.T) {
			w, err := Create(filepath.Join(t.TempDir(), "ix"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = w.Abort() }()
			for i := range c.max {
				if k, err := c.add(w, i); err != nil || int(k) != i {
					t.Fatalf("entry %d: %d, %v", i, k, err)
				}
			}
			_, err = c.add(w, c.max)
			var le *LimitError
			if !errors.As(err, &le) || le.What != c.what || le.Max != uint64(c.max) || !errors.Is(err, ErrTooMany) {
				t.Fatalf("entry %d: %v", c.max, err)
			}
			if !strings.Contains(err.Error(), strconv.Itoa(c.max)) {
				t.Errorf("message %q", err)
			}
			if k, err := c.add(w, c.max-1); err != nil || int(k) != c.max-1 {
				t.Errorf("the last entry again: %d, %v", k, err)
			}
		})
	}
	t.Run("rows", func(t *testing.T) {
		w, err := Create(filepath.Join(t.TempDir(), "ix"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = w.Abort() }()
		r := Row{Kind: KindTxn, Sub: NoSub, LatUS: NoLatency}
		w.n = MaxRows - 1
		if err := w.Append(r, nil, nil); err != nil || w.Rows() != MaxRows {
			t.Fatalf("row %d: %v", uint64(MaxRows), err)
		}
		err = w.Append(r, nil, nil)
		var le *LimitError
		if !errors.As(err, &le) || le.What != "rows" || le.Max != MaxRows || w.Rows() != MaxRows {
			t.Fatalf("row %d: %v", uint64(MaxRows)+1, err)
		}
	})
}

// A frame's time reads back as written, however far it is from its row's
// time: the difference takes all 64 bits.
func TestFrameTimeRoundTrip(t *testing.T) {
	const lim = math.MaxInt64
	cases := []struct{ row, frame int64 }{
		{0, 0}, {0, 1}, {1, 0}, {0, -1},
		{0, 1<<62 - 1}, {0, 1 << 62}, {0, 1<<62 + 1}, {0, -(1 << 62)}, {0, -(1<<62 + 1)},
		{1<<62 - 1, 0}, {1 << 62, 0}, {-(1 << 62), 0},
		{0, lim}, {0, -lim}, {0, math.MinInt64}, {lim, 0}, {math.MinInt64, 0},
		{math.MinInt64, lim}, {lim, math.MinInt64}, {-lim, lim}, {lim, -lim}, {lim - 1, lim}, {math.MinInt64 + 1, math.MinInt64},
	}
	dir := filepath.Join(t.TempDir(), "ix")
	w, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		tm := time.Unix(0, c.frame)
		refs := []model.FrameRef{{Frame: uint64(i + 1), Iface: 7, Time: &tm, PayOff: 54, PayLen: 12}, {Frame: uint64(i + 2), Iface: 1 << 31}}
		if err := w.Append(Row{Kind: KindTxn, TimeNS: c.row, Sub: NoSub, LatUS: NoLatency}, refs, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Finish(Meta{Fingerprint: "fp"}, nil); err != nil {
		t.Fatal(err)
	}
	ix, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ix.Close() }()
	for i, c := range cases {
		req, _, err := ix.Refs(ix.Row(uint32(i)))
		if err != nil || len(req) != 2 {
			t.Fatalf("row %d: %v %+v", i, err, req)
		}
		if req[0].Time == nil || req[0].Time.UnixNano() != c.frame || req[0].Iface != 7 || req[0].PayLen != 12 {
			t.Errorf("row time %d, frame time %d: read back %v (interface %d)", c.row, c.frame, req[0].Time, req[0].Iface)
		}
		if req[1].Time != nil || req[1].Iface != 1<<31 || req[1].Frame != uint64(i+2) {
			t.Errorf("row %d: the reference without a time reads back as %+v", i, req[1])
		}
	}
}

// L1, L2, L11: Retire refuses new readers at once without waiting for
// those in flight; the last one's Release unmaps the index and runs the
// callback. Close waits for them; closing again, from two goroutines or
// after Retire, is harmless.
func TestRetire(t *testing.T) {
	ix, _ := build(t, 100)
	if !ix.Acquire() {
		t.Fatal("a new index refused a reader")
	}
	if !ix.Acquire() {
		t.Fatal("a second reader was refused")
	}
	var ran int
	ix.Retire(func() { ran++ })
	if ix.Acquire() {
		t.Error("a retired index took a reader")
	}
	select {
	case <-ix.Closed():
		t.Fatal("unmapped with two readers")
	default:
	}
	if r := ix.Row(99); r.TID != 99 || ran != 0 {
		t.Errorf("a reader of a retired index: %+v, callback ran %d times", r, ran)
	}
	ix.Release()
	if ran != 0 {
		t.Error("the callback ran with a reader left")
	}
	closed := make(chan error, 2)
	for range 2 {
		go func() { closed <- ix.Close() }()
	}
	ix.Release()
	for range 2 {
		if err := <-closed; err != nil {
			t.Error(err)
		}
	}
	<-ix.Closed()
	if ran != 1 {
		t.Errorf("the callback ran %d times", ran)
	}
	ix.Retire(func() { ran++ })
	if err := ix.Close(); err != nil || ran != 2 {
		t.Errorf("after closing: %v, callback ran %d times", err, ran)
	}

	// Without readers, Retire unmaps at once.
	other, _ := build(t, 10)
	other.Retire(nil)
	<-other.Closed()
	if other.Acquire() {
		t.Error("a retired index took a reader")
	}
}
