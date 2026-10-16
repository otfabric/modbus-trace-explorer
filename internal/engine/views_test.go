// SPDX-License-Identifier: MIT

package engine

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net/netip"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/otfabric/go-modbus-trace/analyze"
	"github.com/otfabric/go-modbus-trace/trace"
	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// poller is a trace for the views: a register whose value follows a script,
// a write to it in the middle, two units, an exception, coils.
var script = []uint16{10, 10, 10, 11, 11, 12, 12, 12, 12, 13}

func poller(b *tracetest.Builder) {
	c := b.Conn(tracetest.ConnOptions{Client: cl1, Server: srv1})
	tid := uint16(0)
	for i, v := range script {
		b.Advance(time.Second)
		tid++
		c.Request(tid, 1, tracetest.ReadRequest(3, 100, 3))
		c.Response(tid, 1, tracetest.RegistersResponse(3, 7, v, 9))
		if i == 4 {
			tid++
			c.Request(tid, 1, []byte{6, 0, 101, 0, 99})
			c.Response(tid, 1, []byte{6, 0, 101, 0, 99})
		}
		tid++
		c.Request(tid, 2, tracetest.ReadRequest(1, 0, 8))
		c.Response(tid, 2, []byte{1, 1, byte(i % 2)})
	}
	tid++
	c.Request(tid, 1, tracetest.ReadRequest(4, 5, 1))
	c.Response(tid, 1, tracetest.Exception(4, 2))
	c.Close()
}

// library runs the go-modbus-trace analyzers over the same trace, as
// mbtracectl summary does.
func library(t *testing.T, path string) (*analyze.DevicesResult, *analyze.PatternsResult, []analyze.CoverageResult) {
	t.Helper()
	f, err := trace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	dev, pat, cov := analyze.NewDevices(), analyze.NewPatterns(), analyze.NewCoverage()
	if _, err := trace.Analyze(context.Background(), f, trace.Options{}, analyze.Tee(dev, pat, cov)); err != nil {
		t.Fatal(err)
	}
	return dev.Result(), pat.Result(), cov.Result()
}

// A7: Devices, Conversations and Patterns agree with the library's
// analyzers (mbtracectl summary --json) on the same trace.
func TestViewsMatchTheLibrary(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	path := writeTrace(t, "poll.pcapng", tracetest.Options{Format: tracetest.PcapNG}, poller)
	id, _ := importTrace(t, e, nil, path)
	lib, libPat, libCov := library(t, path)

	devs, err := e.Devices(ctx, id, service.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != len(lib.Devices) {
		t.Fatalf("%d devices, library %d", len(devs), len(lib.Devices))
	}
	for k, d := range devs {
		l := lib.Devices[k]
		if d.Server != l.Server || d.Unit != l.Unit || d.Transactions != l.Transactions || fmt.Sprint(d.ByFunction) != fmt.Sprint(l.ByFunction) ||
			fmt.Sprint(d.ByStatus) != fmt.Sprint(l.ByStatus) || fmt.Sprint(d.Clients) != fmt.Sprint(l.Clients) ||
			d.Bytes != l.RequestBytes+l.ResponseBytes || d.FirstNS != l.First.UnixNano() || d.LastNS != l.Last.UnixNano() {
			t.Errorf("device %d:\n ours %+v\n lib  %+v", k, d, l)
		}
		// The library's latency is a 1% histogram; ours is exact (µs).
		if d.Latency == nil || l.Latency == nil || math.Abs(float64(d.Latency.P50NS-l.Latency.P50NS)) > 0.02*float64(l.Latency.P50NS)+1000 {
			t.Errorf("latency %+v, library %+v", d.Latency, l.Latency)
		}
	}
	if devs[0].Exceptions != 1 || devs[0].Writes != 1 || devs[0].Rate <= 0 {
		t.Errorf("%+v", devs[0])
	}

	pats, err := e.Patterns(ctx, id, service.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pats) != len(libPat.Patterns) {
		t.Fatalf("%d patterns, library %d", len(pats), len(libPat.Patterns))
	}
	byKey := map[string]service.Pattern{}
	for _, p := range pats {
		byKey[key(p.Client, p.Server, p.Unit, p.Function, p.Summary)] = p
	}
	for _, l := range libPat.Patterns {
		p, ok := byKey[key(l.Client, l.Server, l.Unit, l.Function, l.Summary)]
		if !ok || p.Requests != l.Count || p.Responses != l.Responses || p.Changed != l.Changed || math.Abs(p.Volatility-l.Volatility) > 1e-9 {
			t.Errorf("pattern %q: ours %+v, library %+v", l.Summary, p, l)
			continue
		}
		if l.Interval != nil && (p.Interval == nil || math.Abs(float64(p.Interval.P50NS-l.Interval.P50NS)) > 0.02*float64(l.Interval.P50NS)) {
			t.Errorf("pattern %q interval %+v, library %+v", l.Summary, p.Interval, l.Interval)
		}
	}
	regs := byKey[key(cl1.Addr().String(), srv1.String(), 1, 3, "Read 3 holding registers @ 100")]
	if regs.VolatilityClass != "noisy" || regs.Changed != 3 || regs.Interval == nil || len(regs.Interval.Histogram) == 0 {
		t.Errorf("register pattern %+v", regs)
	}

	cov, err := e.Coverage(ctx, id, service.CoverageQuery{Server: srv1.String(), Unit: 1, Space: "holding"})
	if err != nil {
		t.Fatal(err)
	}
	var lc analyze.CoverageResult
	for _, c := range libCov {
		if c.Server == srv1.String() && c.Unit == 1 && c.Space == "holding" {
			lc = c
		}
	}
	if fmt.Sprint(len(cov.Read), cov.Read[0].Start, cov.Read[0].End, cov.Read[0].Count) != fmt.Sprint(len(lc.Read), lc.Read[0].Start, lc.Read[0].End, lc.Read[0].Count) ||
		len(cov.Written) != len(lc.Written) || cov.Written[0].Start != 101 || !slices.Contains(cov.Spaces, "holding") {
		t.Errorf("coverage %+v, library %+v", cov, lc)
	}

	conns, err := e.Connections(ctx, id, service.Filter{})
	if err != nil || len(conns) != 1 || conns[0].Transactions != 22 || conns[0].Open != "syn" || conns[0].Close != "fin" {
		t.Errorf("connections %+v %v", conns, err)
	}

	// Views follow the filter.
	u2 := service.Filter{Units: []uint8{2}}
	devs, _ = e.Devices(ctx, id, u2)
	pats, _ = e.Patterns(ctx, id, u2)
	if len(devs) != 1 || devs[0].Unit != 2 || len(pats) != 1 || pats[0].Function != 1 {
		t.Errorf("unit 2: %+v %+v", devs, pats)
	}
	// "Show in timeline" of a pattern.
	p := page(t, e, id, service.Filter{Summary: "Read 3 holding registers @ 100"})
	if len(txns(p)) != len(script) {
		t.Errorf("pattern rows %d", len(txns(p)))
	}
	if p := page(t, e, id, service.Filter{Summary: "never seen"}); p.Total != 0 {
		t.Error("an unknown summary matched")
	}
	if _, err := e.Coverage(ctx, id, service.CoverageQuery{Server: srv1.String(), Unit: 1, Space: "attic"}); err == nil {
		t.Error("an unknown space")
	}
}

func key(client, server string, unit, fc uint8, summary string) string {
	return fmt.Sprintf("%s|%s|%d|%d|%s", client, server, unit, fc, summary)
}

// history runs an address history to the end and returns its points.
func history(t *testing.T, e *Engine, id string, q service.HistoryQuery) ([]service.HistoryPoint, *service.HistoryChunk) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, _ := e.Subscribe(ctx)
	ref, err := e.AddressHistory(context.Background(), id, q)
	if err != nil {
		t.Fatal(err)
	}
	var pts []service.HistoryPoint
	for {
		select {
		case ev := <-events:
			if ev.History == nil || ev.History.Job != ref.ID {
				continue
			}
			pts = append(pts, ev.History.Points...)
			if ev.History.Done {
				return pts, ev.History
			}
		case <-time.After(10 * time.Second):
			t.Fatal("no end of the history")
		}
	}
}

// A8: the history of a scripted register is the script, with the write
// marked; repeated values collapse unless asked; the cap continues.
func TestAddressHistory(t *testing.T) {
	e := newEngine(t)
	path := writeTrace(t, "poll.pcap", tracetest.Options{}, poller)
	id, _ := importTrace(t, e, nil, path)
	q := service.HistoryQuery{Server: srv1.String(), Unit: 1, Space: "holding", Address: 101}
	pts, last := history(t, e, id, service.HistoryQuery{Server: q.Server, Unit: 1, Space: "holding", Address: 101, Every: true})
	var reads []uint16
	writes := 0
	for _, p := range pts {
		if p.Write {
			writes++
			if p.Value != 99 || p.Client != cl1.Addr().String() {
				t.Errorf("write %+v", p)
			}
			continue
		}
		reads = append(reads, p.Value)
	}
	if fmt.Sprint(reads) != fmt.Sprint(script) || writes != 1 || last.Candidates != 11 || last.Next != nil {
		t.Errorf("reads %v, writes %d, last %+v", reads, writes, last)
	}
	changes, _ := history(t, e, id, q)
	if len(changes) != 4+1 { // 10, 11, 12, 13 and the write
		t.Errorf("changes only: %+v", changes)
	}
	capped, end := history(t, e, id, service.HistoryQuery{Server: q.Server, Unit: 1, Space: "holding", Address: 101, Every: true, Limit: 3})
	if len(capped) != 3 || end.Next == nil {
		t.Fatalf("capped %d %+v", len(capped), end)
	}
	more, _ := history(t, e, id, service.HistoryQuery{Server: q.Server, Unit: 1, Space: "holding", Address: 101, Every: true, After: *end.Next})
	if len(capped)+len(more) != len(pts) {
		t.Errorf("continued %d + %d of %d", len(capped), len(more), len(pts))
	}
	bits, _ := history(t, e, id, service.HistoryQuery{Server: q.Server, Unit: 2, Space: "coil", Address: 0, Every: true})
	if len(bits) != 10 || bits[1].Value != 1 {
		t.Errorf("coil history %+v", bits)
	}
	if _, err := e.AddressHistory(context.Background(), id, service.HistoryQuery{Server: q.Server, Unit: 1, Space: "attic"}); err == nil {
		t.Error("an unknown space")
	}
	_ = os.Rename(path, path+".gone")
	if _, err := e.AddressHistory(context.Background(), id, q); err == nil {
		t.Error("a history without the trace")
	}
}

func TestWriteValueAt(t *testing.T) {
	cases := []struct {
		pdu  []byte
		addr uint16
		v    uint16
		ok   bool
	}{
		{[]byte{5, 0, 7, 0xFF, 0}, 7, 1, true},
		{[]byte{5, 0, 7, 0, 0}, 7, 0, true},
		{[]byte{6, 0, 7, 1, 2}, 7, 0x0102, true},
		{[]byte{6, 0, 7, 1, 2}, 8, 0, false},
		{[]byte{15, 0, 10, 0, 9, 2, 0b00000100, 1}, 12, 1, true},
		{[]byte{15, 0, 10, 0, 9, 2, 0b00000100, 1}, 18, 1, true},
		{[]byte{16, 0, 10, 0, 2, 4, 0, 1, 0, 2}, 11, 2, true},
		{[]byte{16, 0, 10, 0, 2, 4, 0, 1, 0, 2}, 12, 0, false},
		{[]byte{23, 0, 0, 0, 1, 0, 20, 0, 1, 2, 0xAB, 0xCD}, 20, 0xABCD, true},
		{[]byte{3, 0, 0, 0, 1}, 0, 0, false},
		{[]byte{6}, 0, 0, false},
	}
	for _, c := range cases {
		if v, ok := writeValueAt(c.pdu, c.addr); v != c.v || ok != c.ok {
			t.Errorf("% x @ %d: %d %v", c.pdu, c.addr, v, ok)
		}
	}
}

func TestQualityView(t *testing.T) {
	e := newEngine(t)
	path := writeTrace(t, "lossy.pcap", tracetest.Options{}, func(b *tracetest.Builder) {
		c := b.Conn(tracetest.ConnOptions{Server: srv1, Midstream: true})
		c.Request(1, 1, tracetest.ReadRequest(3, 0, 1))
		c.Write(tracetest.ToClient, tracetest.WriteOpts{Lose: []int{0}}, tracetest.Frame{TID: 1, Unit: 1, PDU: tracetest.RegistersResponse(3, 1)})
		c.Request(2, 1, tracetest.ReadRequest(3, 0, 1))
		c.Response(2, 1, tracetest.RegistersResponse(3, 1))
	})
	id, _ := importTrace(t, e, &service.AnalyzeConfig{ServerPorts: []uint16{502}}, path)
	q, err := e.Quality(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]service.Condition{}
	for _, c := range q.Conditions {
		got[c.Code] = c
	}
	if len(q.Conditions) < 15 || got["gaps"].Count == 0 || got["gaps"].Severity != "problem" || got["capture_drops"].Count != 0 ||
		got["retransmitted"].Meaning == "" || got["unanswered"].Filter == nil || len(q.Facts) < 4 {
		t.Errorf("%+v", q)
	}
	// "Show in timeline" filters exist and select rows.
	if p := page(t, e, id, *got["gaps"].Filter); len(txns(p)) == 0 {
		t.Error("gap filter selects nothing")
	}
	_ = bytes.MinRead
	_ = netip.Addr{}
}
