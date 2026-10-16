// SPDX-License-Identifier: MIT

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-modbus-capture/capture"
	"github.com/otfabric/go-modbus-capture/mbcap"

	"github.com/otfabric/go-modbus-trace/export/mbcapexport"
	"github.com/otfabric/go-modbus-trace/export/timeline"
	"github.com/otfabric/go-modbus-trace/trace"
	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// exportTrace imports the poller trace (reads, a write, an exception, two
// units) and returns the engine, the trace's ID and its path.
func exportTrace(t *testing.T) (*Engine, string, string) {
	t.Helper()
	e := newEngine(t)
	path := writeTrace(t, "poll.pcapng", tracetest.Options{Format: tracetest.PcapNG}, poller)
	id, _ := importTrace(t, e, nil, path)
	return e, id, path
}

// runExport waits for an export job and returns its result.
func runExport(t *testing.T, e *Engine, ref service.JobRef, err error) service.ExportResult {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	e.wait(ref.ID)
	res, err := e.ExportResult(context.Background(), ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// A9: the preview's counts equal the report's; the file is what the
// library writes for the same options (mbtracectl export mbcap differs only
// in the producer and step it names); it reads back through the capture
// library's checker with every sample.
func TestExportMBCAP(t *testing.T) {
	e, id, path := exportTrace(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		x    service.MBCAPExport
	}{
		{"everything", service.MBCAPExport{Exceptions: true, Format: "mbcap"}},
		{"no exceptions, unit 1, labels", service.MBCAPExport{Filter: service.Filter{Units: []uint8{1}}, Format: "mbcap",
			Labels: map[string]string{srv1.String() + "/1": "Meter"}}},
		{"window", service.MBCAPExport{Exceptions: true, Format: "jsonl", Filter: service.Filter{FromNS: ptr(traceStart.Add(3500 * time.Millisecond).UnixNano()),
			ToNS: ptr(traceStart.Add(8 * time.Second).UnixNano())}}},
		{"change-only csv", service.MBCAPExport{Exceptions: true, ChangeOnly: true, Format: "csv", TickNS: 1000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pv, err := e.PreviewMBCAP(ctx, id, tc.x)
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "out."+tc.x.Format)
			ref, err := e.ExportMBCAP(ctx, id, tc.x, out)
			res := runExport(t, e, ref, err)
			rep := res.Report
			if rep == nil || res.File != filepath.Base(out) || res.Kind != "mbcap" {
				t.Fatalf("result %+v", res)
			}
			if pv.Transactions != rep.TransactionsSeen || len(pv.Targets) != len(rep.Targets) {
				t.Errorf("preview %d transactions, %d targets; report %d, %d", pv.Transactions, len(pv.Targets), rep.TransactionsSeen, len(rep.Targets))
			}
			if tc.x.ChangeOnly {
				if !pv.UpTo || pv.Samples != rep.SamplesWritten+rep.ChangeOnlyDropped {
					t.Errorf("change-only preview %d (up to %v); report %d + %d dropped", pv.Samples, pv.UpTo, rep.SamplesWritten, rep.ChangeOnlyDropped)
				}
			} else if pv.Samples != rep.SamplesWritten {
				t.Errorf("preview %d samples, report %d", pv.Samples, rep.SamplesWritten)
			}
			for _, x := range pv.Excluded {
				if rep.Excluded[x.Reason] != x.Count || x.Meaning == "" {
					t.Errorf("excluded %s: preview %d, report %d", x.Reason, x.Count, rep.Excluded[x.Reason])
				}
			}
			if tc.x.Filter.FromNS != nil && rep.Excluded["outside_window"] == 0 {
				t.Error("the window excluded nothing")
			}
			var sum uint64
			for _, x := range pv.Excluded {
				sum += x.Count
			}
			if sum+pv.Samples != pv.Transactions {
				t.Errorf("preview does not add up: %d + %d != %d", sum, pv.Samples, pv.Transactions)
			}
			for k, tg := range pv.Targets {
				if tg.Label != rep.Targets[k].Label || tg.Samples != rep.Targets[k].Samples && !tc.x.ChangeOnly || len(tg.Clients) != len(rep.Targets[k].Streams) {
					t.Errorf("target %d: preview %+v, report %+v", k, tg, rep.Targets[k])
				}
			}

			// The same export straight through the library.
			sel, err := selectionOf(tc.x.Filter)
			if err != nil {
				t.Fatal(err)
			}
			f, err := trace.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			cfg := *mustInfo(t, e, id).Choices
			opts, _ := traceOptions(cfg)
			var want bytes.Buffer
			open, _ := mbcapOpen(tc.x.Format, &want)
			if _, err := mbcapexport.Export(ctx, f, opts, mbcapOptions(tc.x, sel), open); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want.Bytes()) {
				t.Errorf("the export differs from the library's (%d and %d bytes)", len(got), want.Len())
			}
			if tc.x.Format == "mbcap" {
				r, err := mbcap.NewReader(bytes.NewReader(got))
				if err != nil {
					t.Fatal(err)
				}
				c, err := capture.ReadAll(r)
				if err != nil {
					t.Fatal(err)
				}
				if uint64(len(c.Samples)) != rep.SamplesWritten || !strings.HasPrefix(c.Header.Producer, "modbus-trace-explorer ") {
					t.Errorf("read back %d samples (report %d), producer %q", len(c.Samples), rep.SamplesWritten, c.Header.Producer)
				}
			}
			if len(res.Replay) < 2 || !strings.HasPrefix(res.Replay[0], "mbcapctl verify ") || !strings.Contains(res.Replay[1], "--before-first absent --missing exception") ||
				!strings.HasPrefix(res.Doc, "https://") {
				t.Errorf("replay %q", res.Replay)
			}
		})
	}
	if pv, _ := e.PreviewMBCAP(ctx, id, service.MBCAPExport{Exceptions: false}); !slices.ContainsFunc(pv.Excluded, func(x service.Exclusion) bool {
		return x.Reason == "exceptions_disabled" && x.Count == 1
	}) || !slices.ContainsFunc(pv.Excluded, func(x service.Exclusion) bool { return x.Reason == "write_function" && x.Count == 1 }) {
		t.Errorf("exclusions %+v", pv.Excluded)
	}
}

func mustInfo(t *testing.T, e *Engine, id string) service.FileInfo {
	t.Helper()
	tr, err := e.trace(id)
	if err != nil {
		t.Fatal(err)
	}
	return tr.info
}

// traceStart is the builder's default time of the first packet.
var traceStart = time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)

// The report saves as JSON; a second save replaces the first atomically.
func TestSaveReport(t *testing.T) {
	e, id, _ := exportTrace(t)
	ctx := context.Background()
	out := filepath.Join(t.TempDir(), "x.mbcap")
	ref, err := e.ExportMBCAP(ctx, id, service.MBCAPExport{Exceptions: true}, out)
	res := runExport(t, e, ref, err)
	rp := filepath.Join(t.TempDir(), "report.json")
	for range 2 {
		if err := e.SaveReport(ctx, res.Job, rp); err != nil {
			t.Fatal(err)
		}
	}
	var rep mbcapexport.Report
	b, _ := os.ReadFile(rp)
	if err := json.Unmarshal(b, &rep); err != nil || rep.SamplesWritten != res.Report.SamplesWritten {
		t.Fatalf("%v %+v", err, rep)
	}
	ents, _ := os.ReadDir(filepath.Dir(rp))
	if len(ents) != 1 {
		t.Errorf("left behind: %v", ents)
	}
	if err := e.SaveReport(ctx, "nope", rp); !isCode(err, service.CodeNotFound) {
		t.Errorf("unknown job: %v", err)
	}
	if err := e.SaveReport(ctx, res.Job, "relative.json"); !isCode(err, service.CodeInvalidArgument) {
		t.Errorf("relative path: %v", err)
	}
}

func isCode(err error, code string) bool {
	var se *service.Error
	return errors.As(err, &se) && se.Code == code
}

// A10: the timeline export is the library's for the same selection; the
// sub-trace holds the selected connection, and reopened in the engine it
// shows the same transactions.
func TestExportTimelineAndSubtrace(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	path := writeTrace(t, "two.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	id, _ := importTrace(t, e, nil, path)

	for _, format := range []string{"csv", "jsonl"} {
		out := filepath.Join(t.TempDir(), "t."+format)
		f := service.Filter{Units: []uint8{1}}
		ref, err := e.ExportTimeline(ctx, id, service.TimelineExport{Filter: f, Format: format}, out)
		res := runExport(t, e, ref, err)
		sel, _ := selectionOf(f)
		tf, _ := trace.Open(path)
		var want bytes.Buffer
		tfmt := map[string]timeline.Format{"csv": timeline.CSV, "jsonl": timeline.JSONL}[format]
		opts, _ := traceOptions(*mustInfo(t, e, id).Choices)
		if _, _, err := timeline.Write(ctx, tf, opts, &want,
			timeline.Options{Format: tfmt, Selection: sel}); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(out)
		if !bytes.Equal(got, want.Bytes()) || res.Rows == 0 {
			t.Errorf("%s: %d rows; differs from the library: %v", format, res.Rows, !bytes.Equal(got, want.Bytes()))
		}
	}

	conns, _ := e.Connections(ctx, id, service.Filter{})
	if len(conns) != 2 {
		t.Fatalf("%d connections", len(conns))
	}
	sub := filepath.Join(t.TempDir(), "sub.pcapng")
	ref, err := e.ExportSubtrace(ctx, id, service.SubtraceExport{Filter: service.Filter{Conns: []uint32{conns[0].ID}}}, sub)
	res := runExport(t, e, ref, err)
	if res.Connections != 1 || res.Packets == 0 || res.Format != "pcapng" {
		t.Fatalf("%+v", res)
	}
	id2, _ := importTrace(t, e, nil, sub)
	want, _ := e.Timeline(ctx, id, service.TimelineQuery{Filter: service.Filter{Conns: []uint32{conns[0].ID}}, Order: "time", Limit: 100})
	got, _ := e.Timeline(ctx, id2, service.TimelineQuery{Order: "time", Limit: 100})
	summaries := func(p service.TimelinePage) []string {
		var s []string
		for _, r := range p.Rows {
			if r.Kind == "txn" {
				s = append(s, r.Summary+"|"+r.Result)
			}
		}
		return s
	}
	if !slices.Equal(summaries(got), summaries(want)) || len(summaries(got)) == 0 {
		t.Errorf("sub-trace transactions\n got  %q\n want %q", summaries(got), summaries(want))
	}
	if _, err := e.ExportSubtrace(ctx, id, service.SubtraceExport{Filter: service.Filter{Units: []uint8{1}}}, sub); !isCode(err, service.CodeInvalidArgument) {
		t.Errorf("a transaction term in a sub-trace: %v", err)
	}
}

// Exports refuse what they cannot do, and never leave a partial file.
func TestExportRefusals(t *testing.T) {
	e, id, path := exportTrace(t)
	ctx := context.Background()
	dir := t.TempDir()
	out := filepath.Join(dir, "x.mbcap")
	for name, err := range map[string]error{
		"exception term": func() error {
			_, err := e.ExportMBCAP(ctx, id, service.MBCAPExport{Filter: service.Filter{Exceptions: []uint8{2}}}, out)
			return err
		}(),
		"request term": func() error {
			_, err := e.PreviewMBCAP(ctx, id, service.MBCAPExport{Filter: service.Filter{Summary: "x"}})
			return err
		}(),
		"range without space": func() error {
			_, err := e.ExportTimeline(ctx, id, service.TimelineExport{Filter: service.Filter{Start: ptr[uint16](1)}, Format: "csv"}, out)
			return err
		}(),
		"bad server": func() error {
			_, err := e.ExportTimeline(ctx, id, service.TimelineExport{Filter: service.Filter{Servers: []string{"x"}}, Format: "csv"}, out)
			return err
		}(),
		"bad client": func() error {
			_, err := e.ExportTimeline(ctx, id, service.TimelineExport{Filter: service.Filter{Clients: []string{"x"}}, Format: "csv"}, out)
			return err
		}(),
		"bad format": func() error {
			_, err := e.ExportTimeline(ctx, id, service.TimelineExport{Format: "xml"}, out)
			return err
		}(),
		"bad encoding": func() error { _, err := e.ExportMBCAP(ctx, id, service.MBCAPExport{Format: "xml"}, out); return err }(),
		"tick":         func() error { _, err := e.ExportMBCAP(ctx, id, service.MBCAPExport{TickNS: 2e9}, out); return err }(),
		"relative":     func() error { _, err := e.ExportMBCAP(ctx, id, service.MBCAPExport{}, "x.mbcap"); return err }(),
		"no folder": func() error {
			_, err := e.ExportMBCAP(ctx, id, service.MBCAPExport{}, filepath.Join(dir, "missing", "x.mbcap"))
			return err
		}(),
		"the trace itself": func() error { _, err := e.ExportMBCAP(ctx, id, service.MBCAPExport{}, path); return err }(),
	} {
		if !isCode(err, service.CodeInvalidArgument) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.ExportResult(ctx, "nope"); !isCode(err, service.CodeNotFound) {
		t.Errorf("unknown result: %v", err)
	}

	// A cancelled export leaves nothing behind.
	big := writeTrace(t, "big.pcap", tracetest.Options{CountOnly: true}, func(b *tracetest.Builder) {
		c := b.Conn(tracetest.ConnOptions{Client: cl1, Server: srv1})
		for i := range 200_000 {
			c.Request(uint16(i), 1, tracetest.ReadRequest(3, 0, 10))
			c.Response(uint16(i), 1, tracetest.RegistersResponse(3, make([]uint16, 10)...))
		}
	})
	bid, _ := importTrace(t, e, nil, big)
	ref, err := e.ExportTimeline(ctx, bid, service.TimelineExport{Format: "jsonl"}, out)
	if err != nil {
		t.Fatal(err)
	}
	_ = e.CancelJob(ctx, ref.ID)
	e.wait(ref.ID)
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("left behind after cancel: %v", ents)
	}

	// A trace that is gone cannot be exported.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ExportMBCAP(ctx, id, service.MBCAPExport{}, out); !isCode(err, service.CodeUnavailable) {
		t.Errorf("missing trace: %v", err)
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/tmp/a.mbcap":       "/tmp/a.mbcap",
		"/tmp/my file.mbcap": "'/tmp/my file.mbcap'",
		"/tmp/it's":          `'/tmp/it'\''s'`,
		"":                   "''",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
