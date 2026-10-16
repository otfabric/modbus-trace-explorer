// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// TestScale imports a generated trace of MTE_SCALE transactions and times
// the import and the queries the user interface makes (R21, A4). Skipped
// unless MTE_SCALE is set: make scale.
func TestScale(t *testing.T) {
	n, err := strconv.Atoi(os.Getenv("MTE_SCALE"))
	if err != nil || n <= 0 {
		t.Skip("set MTE_SCALE=<transactions> (make scale)")
	}
	t0 := time.Now()
	path := writeTrace(t, "scale.pcap", tracetest.Options{CountOnly: true}, func(b *tracetest.Builder) {
		var conns []*tracetest.Conn
		for i := range 40 {
			conns = append(conns, b.Conn(tracetest.ConnOptions{
				Client: netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, 0, 0, byte(1 + i%6)}), uint16(40000+i)),
				Server: netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, 1, 0, byte(i)}), 502),
			}))
		}
		vals := make([]uint16, 62)
		for i := range n {
			c := conns[i%len(conns)]
			c.Request(uint16(i), 1, tracetest.ReadRequest(3, 4096, 62))
			vals[0] = uint16(i)
			c.Response(uint16(i), 1, tracetest.RegistersResponse(3, vals...))
		}
	})
	fi, _ := os.Stat(path)
	t.Logf("trace: %d transactions, %.0f MiB, generated in %v", n, float64(fi.Size())/(1<<20), time.Since(t0).Round(time.Millisecond))
	e := newEngine(t)
	ctx := context.Background()
	t0 = time.Now()
	id, _ := importTrace(t, e, &service.AnalyzeConfig{ServerPorts: []uint16{502}}, path)
	el := time.Since(t0)
	ov, _ := e.Overview(ctx, id)
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	ixSize := dirSize(e.cache.Dir(id))
	t.Logf("open+scan+analyze+index: %v = %.0f MB/s; %d rows; index %.0f MiB = %.1f%% of the trace; heap %.0f MiB",
		el.Round(time.Millisecond), float64(fi.Size())/1e6/el.Seconds(), ov.Rows, float64(ixSize)/(1<<20),
		100*float64(ixSize)/float64(fi.Size()), float64(ms.HeapAlloc)/(1<<20))
	timeQ := func(name string, q service.TimelineQuery) {
		var worst time.Duration
		for range 5 {
			t0 := time.Now()
			if _, err := e.Timeline(ctx, id, q); err != nil {
				t.Fatal(err)
			}
			worst = max(worst, time.Since(t0))
		}
		t.Logf("  %-40s worst of 5: %v", name, worst.Round(10*time.Microsecond))
	}
	timeQ("first page", service.TimelineQuery{Order: "time", Limit: 100})
	timeQ("page at the end", service.TimelineQuery{Order: "time", Offset: uint64(n), Limit: 100})
	timeQ("filter unit+fc (first time, then cached)", service.TimelineQuery{Filter: service.Filter{Units: []uint8{1}, Functions: []uint8{3}}, Order: "time", Offset: uint64(n / 2), Limit: 100})
	timeQ("latency order, middle", service.TimelineQuery{Order: "latency", Offset: uint64(n / 2), Limit: 100})
	t0 = time.Now()
	_, _ = e.Timeline(ctx, id, service.TimelineQuery{Filter: service.Filter{Servers: []string{"10.1.0.7"}}, Order: "time", Limit: 100})
	t.Logf("  %-40s %v", "a new filter (scan of the columns)", time.Since(t0).Round(10*time.Microsecond))
	view := func(name string, fn func() error) {
		t0 := time.Now()
		if err := fn(); err != nil {
			t.Fatal(err)
		}
		t.Logf("  %-40s %v", name, time.Since(t0).Round(10*time.Microsecond))
	}
	view("devices", func() error { _, err := e.Devices(ctx, id, service.Filter{}); return err })
	view("connections", func() error { _, err := e.Connections(ctx, id, service.Filter{}); return err })
	view("patterns", func() error { _, err := e.Patterns(ctx, id, service.Filter{}); return err })
	view("coverage", func() error {
		_, err := e.Coverage(ctx, id, service.CoverageQuery{Server: "10.1.0.7:502", Unit: 1, Space: "holding"})
		return err
	})
	view("quality", func() error { _, err := e.Quality(ctx, id); return err })
	t0 = time.Now()
	pts, last := history(t, e, id, service.HistoryQuery{Server: "10.1.0.7:502", Unit: 1, Space: "holding", Address: 4096})
	t.Logf("  %-40s %v: %d points of %d candidates, capped: %v", "address history (a changing register)",
		time.Since(t0).Round(time.Millisecond), len(pts), last.Candidates, last.Next != nil)
}

func dirSize(dir string) int64 {
	var n int64
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if e.IsDir() {
			n += dirSize(dir + "/" + e.Name())
			continue
		}
		if fi, err := e.Info(); err == nil {
			n += fi.Size()
		}
	}
	return n
}

// TestImportFile imports the trace at MTE_TRACE (a real capture kept
// outside the repository) and reports what TestScale does.
func TestImportFile(t *testing.T) {
	path := os.Getenv("MTE_TRACE")
	if path == "" {
		t.Skip("set MTE_TRACE=<path> to import a trace")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	e := newEngine(t)
	t0 := time.Now()
	id, res := importTrace(t, e, nil, path)
	el := time.Since(t0)
	ov, err := e.Overview(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	ix := dirSize(e.cache.Dir(id))
	t.Logf("%d bytes, %d flows (%d selected): %d rows, %d transactions in %v; index %d bytes = %.1f%% of the trace, %.0f bytes/row",
		fi.Size(), len(res.Flows), len(res.Selected), ov.Rows, ov.Transactions, el.Round(time.Millisecond), ix,
		100*float64(ix)/float64(fi.Size()), float64(ix)/float64(max(ov.Rows, 1)))
}
