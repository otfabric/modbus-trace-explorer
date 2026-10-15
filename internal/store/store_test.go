// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
