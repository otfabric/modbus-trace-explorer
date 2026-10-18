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
	events, _ := e.Subscribe(ctx)
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

// Queries while snapshots replace each other, and while the analysis is
// cancelled under them: no crash, no deadlock.
func TestQueriesDuringLiveImport(t *testing.T) {
	paceLive(t)
	e := newEngine(t)
	ctx := context.Background()
	path := bigTrace(t, 40_000)
	for _, cancel := range []bool{false, true} {
		info, _ := e.Open(ctx, []string{path})
		events, _ := e.Subscribe(ctx)
		ref, _ := e.Scan(ctx, info.ID)
		waitJob(t, events, ref.ID)
		res, _ := e.ScanResult(ctx, info.ID)
		stop := make(chan struct{})
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
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
			}()
		}
		ref, _ = e.Analyze(ctx, info.ID, service.AnalyzeConfig{Flows: res.Selected, ServerPorts: res.ServerPorts})
		if cancel {
			time.Sleep(50 * time.Millisecond)
			_ = e.CancelJob(ctx, ref.ID)
		}
		j := waitJob(t, events, ref.ID)
		close(stop)
		wg.Wait()
		if cancel && j.State == service.JobCanceled {
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
