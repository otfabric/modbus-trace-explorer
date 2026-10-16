// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// R5 and UX.md section 9: go to time, go to frame, next and previous row
// of the same connection.
func TestLocate(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	path := writeTrace(t, "site.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	id, _ := importTrace(t, e, nil, path)
	all, err := e.Timeline(ctx, id, service.TimelineQuery{Order: "time", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	rows := all.Rows
	loc := func(q service.LocateQuery) service.Location {
		t.Helper()
		l, err := e.Locate(ctx, id, q)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}

	// Time: exact, between two rows, after the end, and descending.
	r := rows[3]
	if l := loc(service.LocateQuery{TimeNS: &r.TimeNS}); !l.Found || !l.Exact || l.Seq != r.Seq || l.Rank != 3 {
		t.Errorf("exact time: %+v", l)
	}
	between := r.TimeNS - 1
	if l := loc(service.LocateQuery{TimeNS: &between}); l.Exact || l.Rank > 3 {
		t.Errorf("between: %+v", l)
	}
	late := rows[len(rows)-1].TimeNS + int64(time.Hour)
	if l := loc(service.LocateQuery{TimeNS: &late}); !l.Found || l.Rank != uint64(len(rows)-1) || l.Note == "" {
		t.Errorf("after the end: %+v", l)
	}
	if l := loc(service.LocateQuery{TimeNS: &r.TimeNS, Desc: true}); l.Rank != uint64(len(rows)-1-3) || l.Seq != r.Seq {
		t.Errorf("descending: %+v", l)
	}

	// Frame: the first transaction's request frame, then a frame no row carries.
	var txn service.Row
	for _, x := range rows {
		if x.Kind == "txn" {
			txn = x
			break
		}
	}
	d, err := e.Detail(ctx, id, txn.Seq)
	if err != nil || d.Request == nil || len(d.Request.Packets) == 0 {
		t.Fatalf("detail %v", err)
	}
	fr := d.Request.Packets[0].Frame
	if l := loc(service.LocateQuery{Frame: &fr}); !l.Exact || l.Seq != txn.Seq {
		t.Errorf("frame %d: %+v", fr, l)
	}
	missing := uint64(1) // the SYN: no transaction carries it
	if l := loc(service.LocateQuery{Frame: &missing}); l.Exact || !l.Found || l.Note == "" {
		t.Errorf("frame 1: %+v", l)
	}
	huge := uint64(1 << 40)
	if l := loc(service.LocateQuery{Frame: &huge}); l.Found {
		t.Errorf("frame past the end: %+v", l)
	}

	// Next and previous of the same connection skip the other connection.
	first := rows[0]
	seq := first.Seq
	l := loc(service.LocateQuery{Seq: &seq, Step: 1})
	if !l.Found || l.Seq == seq {
		t.Fatalf("next: %+v", l)
	}
	for _, x := range rows {
		if x.Seq == l.Seq && x.Conn != first.Conn {
			t.Errorf("next row is on connection %d, not %d", x.Conn, first.Conn)
		}
	}
	back := l.Seq
	if p := loc(service.LocateQuery{Seq: &back, Step: -1}); p.Seq != seq {
		t.Errorf("previous: %+v", p)
	}
	if p := loc(service.LocateQuery{Seq: &seq, Step: -1}); p.Found {
		t.Errorf("before the first: %+v", p)
	}
	if p := loc(service.LocateQuery{Seq: &seq, Step: 1, Filter: service.Filter{Units: []uint8{2}}}); p.Found {
		t.Errorf("a row outside the filter: %+v", p)
	}

	for name, q := range map[string]service.LocateQuery{
		"nothing": {},
		"two":     {TimeNS: &r.TimeNS, Frame: &fr},
	} {
		if _, err := e.Locate(ctx, id, q); !isCode(err, service.CodeInvalidArgument) {
			t.Errorf("%s: %v", name, err)
		}
	}
	zero := uint64(0)
	if _, err := e.Locate(ctx, id, service.LocateQuery{Seq: &zero}); !isCode(err, service.CodeNotFound) {
		t.Errorf("row 0: %v", err)
	}
	if l := loc(service.LocateQuery{TimeNS: &r.TimeNS, Filter: service.Filter{Units: []uint8{99}}}); l.Found {
		t.Errorf("empty filter: %+v", l)
	}
}

// R23: diagnostics carry versions and facts, never paths or addresses.
func TestDiagnostics(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	path := writeTrace(t, "site.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	id, _ := importTrace(t, e, nil, path)
	for _, fid := range []string{"", id} {
		d, err := e.Diagnostics(ctx, fid)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(d, "go-modbus-trace") || strings.Contains(d, path) || strings.Contains(d, "site.pcapng") ||
			strings.Contains(d, srv1.Addr().String()) || strings.Contains(d, cl1.Addr().String()) {
			t.Errorf("diagnostics:\n%s", d)
		}
		if fid != "" && !strings.Contains(d, "index:") {
			t.Errorf("no trace facts:\n%s", d)
		}
	}
	if _, err := e.Diagnostics(ctx, "nope"); !isCode(err, service.CodeNotFound) {
		t.Errorf("unknown trace: %v", err)
	}
}

// R1: several files are one trace in the order of their first packets.
func TestFilesInTimeOrder(t *testing.T) {
	e := newEngine(t)
	early := writeTrace(t, "a.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	late := writeTrace(t, "b.pcapng", tracetest.Options{Format: tracetest.PcapNG, Start: traceStart.Add(time.Hour)}, site)
	info, err := e.Open(context.Background(), []string{late, early})
	if err != nil {
		t.Fatal(err)
	}
	if info.Paths[0] != early || info.Paths[1] != late || !strings.HasPrefix(info.Name, "a.pcapng") {
		t.Errorf("order %v, name %q", info.Paths, info.Name)
	}
	if got := byFirstPacket(context.Background(), []string{late, "/nonexistent", early}); got[0] != early || got[1] != late || got[2] != "/nonexistent" {
		t.Errorf("with an unreadable file: %v", got)
	}
}
