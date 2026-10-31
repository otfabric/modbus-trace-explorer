// SPDX-License-Identifier: MIT

// Package e2e drives the application the way the window does, through the
// service.Service interface and its events and nothing else: open a trace
// file, scan, analyse, read the views, export, close, and fail on a bad file.
package e2e

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/otfabric/go-modbus-trace/trace"
	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/engine"
	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

const polls = 8

type app struct {
	service.Service
	events <-chan service.Event
}

func start(t *testing.T) *app {
	t.Helper()
	e, err := engine.New(engine.Options{CacheDir: filepath.Join(t.TempDir(), "cache"), ConfigDir: filepath.Join(t.TempDir(), "config")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Shutdown(); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	events, err := e.Subscribe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return &app{e, events}
}

// finish waits for the job's last event, as the window does.
func (a *app) finish(t *testing.T, ref service.JobRef, err error) *service.JobState {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	timeout := time.After(time.Minute)
	for {
		select {
		case ev := <-a.events:
			if ev.Job != nil && ev.Job.ID == ref.ID && ev.Job.State != service.JobRunning {
				return ev.Job
			}
		case <-timeout:
			t.Fatalf("job %s did not finish", ref.ID)
		}
	}
}

func done(t *testing.T, st *service.JobState) {
	t.Helper()
	if st.State != service.JobDone {
		t.Fatalf("job %s: %s, %+v", st.ID, st.State, st.Error)
	}
}

// writeTrace is a site with one poller, an exception and a write.
func writeTrace(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	b, err := tracetest.New(&buf, tracetest.Options{Format: tracetest.PcapNG})
	if err != nil {
		t.Fatal(err)
	}
	c := b.Conn(tracetest.ConnOptions{Server: tracetest.MustEndpoint("10.0.0.2:502")})
	for i := range polls {
		c.Request(uint16(i), 1, tracetest.ReadRequest(3, 100, 2))
		c.Response(uint16(i), 1, tracetest.RegistersResponse(3, uint16(i), 7))
		b.Advance(time.Second)
	}
	c.Request(100, 1, tracetest.ReadRequest(4, 0, 1))
	c.Response(100, 1, tracetest.Exception(4, 2))
	c.Request(101, 1, []byte{6, 0, 5, 0, 80})
	c.Response(101, 1, []byte{6, 0, 5, 0, 80})
	c.Close()
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "site.pcapng")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// load opens, scans and analyses a trace with what the scan suggests.
func (a *app) load(t *testing.T, path string) string {
	t.Helper()
	ctx := context.Background()
	info, err := a.Open(ctx, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := a.Scan(ctx, info.ID)
	done(t, a.finish(t, ref, err))
	res, err := a.ScanResult(ctx, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	ref, err = a.Analyze(ctx, info.ID, service.AnalyzeConfig{Flows: res.Selected, ServerPorts: res.ServerPorts})
	done(t, a.finish(t, ref, err))
	return info.ID
}

func TestImportViewsAndClose(t *testing.T) {
	a := start(t)
	ctx := context.Background()
	id := a.load(t, writeTrace(t))

	p, err := a.Timeline(ctx, id, service.TimelineQuery{Order: "time", Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	var txns int
	for _, r := range p.Rows {
		if r.Kind == "txn" {
			txns++
		}
	}
	if txns != polls+2 {
		t.Fatalf("%d transactions on the timeline, want %d", txns, polls+2)
	}
	d, err := a.Detail(ctx, id, p.Rows[0].Seq)
	if err != nil {
		t.Fatal(err)
	}
	if d.Row.Seq != p.Rows[0].Seq {
		t.Errorf("detail of row %d: %+v", p.Rows[0].Seq, d.Row)
	}
	devs, err := a.Devices(ctx, id, service.Filter{})
	if err != nil || len(devs) != 1 {
		t.Fatalf("devices %+v, %v", devs, err)
	}
	pats, err := a.Patterns(ctx, id, service.Filter{})
	if err != nil || len(pats) == 0 {
		t.Fatalf("patterns %+v, %v", pats, err)
	}
	if err := a.Close(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Overview(ctx, id); err == nil {
		t.Error("a closed trace still answers")
	}
}

func TestCacheAndRecent(t *testing.T) {
	a := start(t)
	ctx := context.Background()
	path := writeTrace(t)
	id := a.load(t, path)
	before, err := a.CacheInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(ctx, id); err != nil {
		t.Fatal(err)
	}
	if before.Bytes == 0 {
		t.Errorf("nothing in the cache after an analysis: %+v", before)
	}
	recent, err := a.Recent(ctx)
	if err != nil || len(recent) == 0 {
		t.Errorf("recent files %+v, %v", recent, err)
	}
}

func TestExports(t *testing.T) {
	a := start(t)
	ctx := context.Background()
	id := a.load(t, writeTrace(t))
	dir := t.TempDir()

	out := filepath.Join(dir, "site.mbcap")
	ref, err := a.ExportMBCAP(ctx, id, service.MBCAPExport{Exceptions: true, Format: "mbcap"}, out)
	done(t, a.finish(t, ref, err))
	res, err := a.ExportResult(ctx, ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Report == nil || res.Report.SamplesWritten == 0 {
		t.Fatalf("MBCAP export: %+v", res)
	}
	if err := a.SaveReport(ctx, ref.ID, filepath.Join(dir, "report.json")); err != nil {
		t.Error(err)
	}

	lines := filepath.Join(dir, "timeline.jsonl")
	ref, err = a.ExportTimeline(ctx, id, service.TimelineExport{Format: "jsonl"}, lines)
	done(t, a.finish(t, ref, err))
	b, err := os.ReadFile(lines)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(b, []byte("\n")); n < polls+2 {
		t.Errorf("timeline export has %d lines", n)
	}

	sub := filepath.Join(dir, "sub.pcapng")
	ref, err = a.ExportSubtrace(ctx, id, service.SubtraceExport{}, sub)
	done(t, a.finish(t, ref, err))
	f, err := trace.Open(sub)
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := trace.Scan(ctx, f, trace.ScanOptions{})
	if err != nil || s.Packets == 0 {
		t.Fatalf("the sub-trace does not read: %+v, %v", s, err)
	}
}

// TestBadInput is what the window shows as an error: nothing may hang or
// take the engine down, and it must still work afterwards.
func TestBadInput(t *testing.T) {
	a := start(t)
	ctx := context.Background()
	if _, err := a.Open(ctx, []string{filepath.Join(t.TempDir(), "missing.pcap")}); err == nil {
		t.Error("a missing file opened")
	}
	bad := filepath.Join(t.TempDir(), "notes.pcap")
	if err := os.WriteFile(bad, []byte("this is no trace"), 0o600); err != nil {
		t.Fatal(err)
	}
	if info, err := a.Open(ctx, []string{bad}); err == nil {
		ref, err := a.Scan(ctx, info.ID)
		if err == nil {
			if st := a.finish(t, ref, nil); st.State != service.JobFailed {
				t.Errorf("scan of a text file: %s", st.State)
			}
		}
		_ = a.Close(ctx, info.ID)
	}
	a.load(t, writeTrace(t))
}
