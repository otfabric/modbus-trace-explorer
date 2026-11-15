// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// bigTrace is n transactions on 8 connections.
func bigTrace(t *testing.T, n int) string {
	t.Helper()
	return writeTrace(t, "big.pcap", tracetest.Options{CountOnly: true}, func(b *tracetest.Builder) {
		var conns []*tracetest.Conn
		for i := range 8 {
			conns = append(conns, b.Conn(tracetest.ConnOptions{
				Client: netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, 0, 0, 9}), uint16(40000+i)),
				Server: netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, 1, 0, byte(i)}), 502),
			}))
		}
		for i := range n {
			c := conns[i%len(conns)]
			c.Request(uint16(i), 1, tracetest.ReadRequest(3, uint16(i%50), 2))
			c.Response(uint16(i), 1, tracetest.RegistersResponse(3, uint16(i), 7))
		}
	})
}

// paceLive makes snapshots due at once and often, for a test.
func paceLive(t *testing.T) {
	every, rows := liveEvery, liveMinRows
	liveEvery, liveMinRows = 0, 1024
	t.Cleanup(func() { liveEvery, liveMinRows = every, rows })
}

// R21: during the analysis the rows so far can be paged, filtered and
// opened; the finished index has them all, and no snapshot is left.
func TestLiveImport(t *testing.T) {
	paceLive(t)
	e := newEngine(t)
	ctx := context.Background()
	path := bigTrace(t, 60_000)
	info, _ := e.Open(ctx, []string{path})
	events, _ := e.Subscribe(t.Context())
	ref, _ := e.Scan(ctx, info.ID)
	waitJob(t, events, ref.ID)
	res, _ := e.ScanResult(ctx, info.ID)
	ref, err := e.Analyze(ctx, info.ID, service.AnalyzeConfig{Flows: res.Selected, ServerPorts: res.ServerPorts})
	if err != nil {
		t.Fatal(err)
	}
	var liveTotal uint64
	for ev := range events {
		j := ev.Job
		if j == nil || j.ID != ref.ID {
			continue
		}
		if j.State == service.JobRunning && j.Ready && liveTotal == 0 {
			p, err := e.Timeline(ctx, info.ID, service.TimelineQuery{Order: "time", Limit: 20})
			if err != nil || p.Total == 0 {
				t.Fatalf("the live timeline: %+v %v", p, err)
			}
			liveTotal = p.Total
			for _, r := range p.Rows {
				if r.Kind != "txn" {
					continue
				}
				if d, err := e.Detail(ctx, info.ID, r.Seq); err != nil || d.Request == nil || d.Request.Hex == "" {
					t.Errorf("a live detail: %v", err)
				}
				break
			}
			f, err := e.Timeline(ctx, info.ID, service.TimelineQuery{Filter: service.Filter{Servers: []string{"10.1.0.3"}}, Order: "latency", Limit: 5})
			if err != nil || f.Total == 0 || f.Total >= p.Total {
				t.Errorf("a live filter: %+v %v", f, err)
			}
			if _, err := e.Devices(ctx, info.ID, service.Filter{}); err != nil {
				t.Errorf("a live view: %v", err)
			}
		}
		if j.State != service.JobRunning {
			if j.State != service.JobDone {
				t.Fatalf("analysis %+v", j)
			}
			break
		}
	}
	if liveTotal == 0 {
		t.Fatal("no live timeline during the analysis")
	}
	final, _ := e.Timeline(ctx, info.ID, service.TimelineQuery{Order: "time", Limit: 1})
	if final.Total < 60_000 || final.Total <= liveTotal {
		t.Errorf("final %d rows, live %d", final.Total, liveTotal)
	}
	ents, _ := os.ReadDir(e.cache.Root())
	for _, en := range ents {
		if strings.Contains(en.Name(), ".live-") || strings.HasSuffix(en.Name(), ".partial") {
			t.Errorf("left behind: %s", en.Name())
		}
	}
}

// A snapshot that cannot be written stops the snapshots, not the
// analysis: the timeline appears at the end, as without them.
func TestLiveSnapshotFails(t *testing.T) {
	paceLive(t)
	e := newEngine(t)
	ctx := context.Background()
	info, _ := e.Open(ctx, []string{bigTrace(t, 20_000)})
	events, _ := e.Subscribe(t.Context())
	ref, _ := e.Scan(ctx, info.ID)
	waitJob(t, events, ref.ID)
	res, _ := e.ScanResult(ctx, info.ID)
	// The first snapshot's directory is taken by a file.
	if err := os.WriteFile(e.cache.LiveDir(info.ID, 1), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ref, err := e.Analyze(ctx, info.ID, service.AnalyzeConfig{Flows: res.Selected, ServerPorts: res.ServerPorts})
	if err != nil {
		t.Fatal(err)
	}
	for ev := range events {
		j := ev.Job
		if j == nil || j.ID != ref.ID {
			continue
		}
		if j.Ready {
			t.Fatalf("ready without a snapshot: %+v", j)
		}
		if j.State != service.JobRunning {
			if j.State != service.JobDone {
				t.Fatalf("analysis %+v", j)
			}
			break
		}
	}
	p, err := e.Timeline(ctx, info.ID, service.TimelineQuery{Order: "time", Limit: 1})
	if err != nil || p.Total < 20_000 {
		t.Fatalf("after the analysis: %+v %v", p, err)
	}
	if _, err := os.Stat(e.cache.LiveDir(info.ID, 1)); !os.IsNotExist(err) {
		t.Error("the failed snapshot's directory is left")
	}
}

// Queries while snapshots replace each other, and while the analysis is
// cancelled under them: no crash, no deadlock.
func TestQueriesDuringLiveImport(t *testing.T) {
	paceLive(t)
	e := newEngine(t)
	ctx := context.Background()
	path := bigTrace(t, 40_000)
	for _, cancel := range []bool{false, true} {
		g := newGate(1)
		if cancel {
			holdAnalyses(t, g) // the analysis waits at 4096 rows for its cancel
		}
		info, _ := e.Open(ctx, []string{path})
		events, _ := e.Subscribe(t.Context())
		ref, _ := e.Scan(ctx, info.ID)
		waitJob(t, events, ref.ID)
		res, _ := e.ScanResult(ctx, info.ID)
		stop := make(chan struct{})
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				for {
					select {
					case <-stop:
						return
					default:
					}
					if p, err := e.Timeline(ctx, info.ID, service.TimelineQuery{Order: "time", Offset: 1000, Limit: 50}); err == nil && len(p.Rows) > 0 {
						_, _ = e.Detail(ctx, info.ID, p.Rows[0].Seq)
					}
					_, _ = e.Patterns(ctx, info.ID, service.Filter{})
				}
			})
		}
		ref, _ = e.Analyze(ctx, info.ID, service.AnalyzeConfig{Flows: res.Selected, ServerPorts: res.ServerPorts})
		if cancel {
			within(t, time.Minute, g.held, "the analysis at 4096 rows")
			_ = e.CancelJob(ctx, ref.ID)
		}
		j := waitJob(t, events, ref.ID)
		close(stop)
		wg.Wait()
		if want := map[bool]string{false: service.JobDone, true: service.JobCanceled}[cancel]; j.State != want {
			t.Errorf("cancel %v: the analysis ended %s, want %s", cancel, j.State, want)
		}
		if cancel {
			if _, err := e.Timeline(ctx, info.ID, service.TimelineQuery{Order: "time", Limit: 1}); err == nil {
				t.Error("a cancelled analysis left an index")
			}
		}
		_ = e.Close(ctx, info.ID)
		// A new cache entry next time: the fingerprint is the same, so
		// clear it.
		_, _ = e.ClearCache(ctx)
	}
}

// gate holds a job at chosen passes of a test hook (analyzeGate,
// historyGate): at each, it sends on held, then waits for release or for
// the job's cancellation.
type gate struct {
	mu      sync.Mutex
	passes  int
	stopAt  map[int]bool
	held    chan struct{}
	release chan struct{}
}

func newGate(stopAt ...int) *gate {
	g := &gate{stopAt: map[int]bool{}, held: make(chan struct{}), release: make(chan struct{})}
	for _, n := range stopAt {
		g.stopAt[n] = true
	}
	return g
}

func (g *gate) pass(ctx context.Context) {
	g.mu.Lock()
	g.passes++
	stop := g.stopAt[g.passes]
	g.mu.Unlock()
	if !stop {
		return
	}
	select {
	case g.held <- struct{}{}:
	case <-ctx.Done():
		return
	}
	select {
	case <-g.release:
	case <-ctx.Done():
	}
}

// holdAnalyses sets analyzeGate for the test: the analyses pass g at every
// 4096 rows.
func holdAnalyses(t *testing.T, g *gate) {
	t.Helper()
	analyzeGate = g.pass
	t.Cleanup(func() { analyzeGate = nil })
}

// within waits for a channel, failing the test after d.
func within(t *testing.T, d time.Duration, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(d):
		t.Fatalf("%s: not within %v", what, d)
	}
}

// leftovers lists the snapshots and partial indexes in the cache.
func leftovers(e *Engine) []string {
	var out []string
	ents, _ := os.ReadDir(e.cache.Root())
	for _, en := range ents {
		if strings.Contains(en.Name(), ".live-") || strings.HasSuffix(en.Name(), ".partial") {
			out = append(out, en.Name())
		}
	}
	return out
}

// L4/L6: an analysis cancelled at a known point ends cancelled, leaves no
// partial index or snapshot, and the trace can be analysed again.
func TestCancelAnalysis(t *testing.T) {
	paceLive(t)
	g := newGate(1)
	holdAnalyses(t, g)
	e := newEngine(t)
	ctx := context.Background()
	info, _ := e.Open(ctx, []string{bigTrace(t, 20_000)})
	events, _ := e.Subscribe(t.Context())
	ref, _ := e.Scan(ctx, info.ID)
	waitJob(t, events, ref.ID)
	res, _ := e.ScanResult(ctx, info.ID)
	cfg := service.AnalyzeConfig{Flows: res.Selected, ServerPorts: res.ServerPorts}
	ref, err := e.Analyze(ctx, info.ID, cfg)
	if err != nil {
		t.Fatal(err)
	}
	within(t, time.Minute, g.held, "the analysis at 4096 rows")
	if _, err := e.Scan(ctx, info.ID); err == nil {
		t.Error("a scan started during an analysis")
	}
	if p, err := e.Timeline(ctx, info.ID, service.TimelineQuery{Order: "time", Limit: 1}); err != nil || p.Total != 4096 {
		t.Errorf("the snapshot at the gate: %+v %v", p, err)
	}
	if err := e.CancelJob(ctx, ref.ID); err != nil {
		t.Fatal(err)
	}
	if j := waitJob(t, events, ref.ID); j.State != service.JobCanceled {
		t.Fatalf("cancelled at 4096 rows, the analysis ended %s %+v", j.State, j.Error)
	}
	if _, err := e.Overview(ctx, info.ID); err == nil {
		t.Error("a cancelled analysis left an index")
	}
	if l := leftovers(e); len(l) > 0 {
		t.Errorf("left behind: %v", l)
	}
	ref, _ = e.Analyze(ctx, info.ID, cfg)
	if j := waitJob(t, events, ref.ID); j.State != service.JobDone {
		t.Fatalf("again: %+v", j)
	}
	if ov, err := e.Overview(ctx, info.ID); err != nil || ov.Transactions != 20_000 {
		t.Errorf("%+v %v", ov, err)
	}
}

// L13: opening a trace that is open and being analysed cancels that
// analysis before Open returns; the new one analyses it alone.
func TestReopenDuringAnalysis(t *testing.T) {
	g := newGate(1)
	holdAnalyses(t, g)
	e := newEngine(t)
	ctx := context.Background()
	path := bigTrace(t, 20_000)
	info, _ := e.Open(ctx, []string{path})
	events, _ := e.Subscribe(t.Context())
	ref, _ := e.Scan(ctx, info.ID)
	waitJob(t, events, ref.ID)
	res, _ := e.ScanResult(ctx, info.ID)
	cfg := service.AnalyzeConfig{Flows: res.Selected, ServerPorts: res.ServerPorts}
	first, err := e.Analyze(ctx, info.ID, cfg)
	if err != nil {
		t.Fatal(err)
	}
	within(t, time.Minute, g.held, "the first analysis at 4096 rows")
	again, err := e.Open(ctx, []string{path})
	if err != nil || again.ID != info.ID {
		t.Fatalf("open again: %+v %v", again, err)
	}
	e.mu.Lock()
	_, running := e.jobs[first.ID]
	e.mu.Unlock()
	if running {
		t.Error("the first analysis still runs after the trace was opened again")
	}
	if j := waitJob(t, events, first.ID); j.State != service.JobCanceled {
		t.Errorf("the first analysis ended %s %+v", j.State, j.Error)
	}
	second, err := e.Analyze(ctx, again.ID, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if j := waitJob(t, events, second.ID); j.State != service.JobDone {
		t.Fatalf("the second analysis: %+v %+v", j, j.Error)
	}
	if p, err := e.Timeline(ctx, again.ID, service.TimelineQuery{Order: "time", Limit: 1}); err != nil || p.Total < 20_000 {
		t.Errorf("timeline: %+v %v", p, err)
	}
	if l := leftovers(e); len(l) > 0 {
		t.Errorf("left behind: %v", l)
	}
}

// L11: an address history that holds a live snapshot neither stops the
// import from publishing the next ones nor its Cancel; queries go on, and
// the snapshot is closed, its directory removed, when the history ends.
func TestHistoryDuringLiveImport(t *testing.T) {
	if !store.MovesMapped {
		t.Skip("a directory with mapped files cannot be renamed or removed here: the end of an import waits for its snapshots' readers")
	}
	paceLive(t)
	g := newGate(1, 3)
	holdAnalyses(t, g)
	hg := newGate(1)
	historyGate = hg.pass
	t.Cleanup(func() { historyGate = nil })
	e := newEngine(t)
	ctx := context.Background()
	info, _ := e.Open(ctx, []string{bigTrace(t, 40_000)})
	events, _ := e.Subscribe(t.Context())
	ref, _ := e.Scan(ctx, info.ID)
	waitJob(t, events, ref.ID)
	res, _ := e.ScanResult(ctx, info.ID)
	a, err := e.Analyze(ctx, info.ID, service.AnalyzeConfig{Flows: res.Selected, ServerPorts: res.ServerPorts})
	if err != nil {
		t.Fatal(err)
	}
	within(t, time.Minute, g.held, "the analysis at 4096 rows")
	h, err := e.AddressHistory(ctx, info.ID, service.HistoryQuery{Server: "10.1.0.0:502", Unit: 1, Space: "holding", Address: 0, Every: true})
	if err != nil {
		t.Fatal(err)
	}
	within(t, time.Minute, hg.held, "the history on the first snapshot")
	g.release <- struct{}{}
	// The analysis publishes the next snapshots while the history holds
	// the first, and reaches 12,288 rows.
	within(t, time.Minute, g.held, "the analysis at 12,288 rows while a history holds its first snapshot")
	p, err := e.Timeline(ctx, info.ID, service.TimelineQuery{Order: "time", Limit: 10})
	if err != nil || p.Total != 3*4096 {
		t.Errorf("a query meanwhile: %+v %v", p.Total, err)
	}
	if _, err := os.Stat(e.cache.LiveDir(info.ID, 1)); err != nil {
		t.Errorf("the first snapshot went while the history holds it: %v", err)
	}
	if err := e.CancelJob(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if j := waitJob(t, events, a.ID); j.State != service.JobCanceled {
		t.Errorf("the analysis ended %s", j.State)
	}
	if _, err := e.Timeline(ctx, info.ID, service.TimelineQuery{Order: "time", Limit: 1}); err == nil {
		t.Error("a cancelled analysis left an index")
	}
	hg.release <- struct{}{}
	if j := waitJob(t, events, h.ID); j.State != service.JobDone {
		t.Errorf("the history ended %s %+v", j.State, j.Error)
	}
	if l := leftovers(e); len(l) > 0 {
		t.Errorf("left behind once the history ended: %v", l)
	}
}
