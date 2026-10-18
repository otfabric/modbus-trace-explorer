// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// importCapture opens a capture and imports it, as the window does.
func importCapture(t *testing.T, e *Engine, path string) (string, service.JobState) {
	t.Helper()
	ctx := context.Background()
	info, err := e.Open(ctx, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != "capture" || info.Analyzed {
		t.Fatalf("opened %+v", info)
	}
	events, _ := e.Subscribe(ctx)
	ref, err := e.ImportCapture(ctx, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	for ev := range events {
		if ev.Job != nil && ev.Job.ID == ref.ID && ev.Job.State != service.JobRunning {
			return info.ID, *ev.Job
		}
	}
	t.Fatal("no end of the import")
	return "", service.JobState{}
}

// R24: a capture exported from the poller trace opens read-only in the same
// timeline, its samples decoded, the views computed over them, the value
// history read from the kept PDUs, in each of the three encodings.
func TestOpenCapture(t *testing.T) {
	e, id, _ := exportTrace(t)
	ctx := context.Background()
	for _, format := range []string{"mbcap", "jsonl", "csv"} {
		t.Run(format, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "poll.mbcap")
			if format != "mbcap" {
				out += "." + format
			}
			ref, err := e.ExportMBCAP(ctx, id, service.MBCAPExport{Exceptions: true, Format: format}, out)
			rep := runExport(t, e, ref, err).Report

			cid, job := importCapture(t, e, out)
			if job.State != service.JobDone {
				t.Fatalf("import %+v", job)
			}
			ov, err := e.Overview(ctx, cid)
			if err != nil || ov.Capture == nil || ov.Capture.Encoding != format || ov.Transactions != rep.SamplesWritten ||
				ov.Capture.Targets != len(rep.Targets) || !ov.Capture.EndKnown || len(ov.Capture.Provenance) == 0 {
				t.Fatalf("overview %+v %+v %v", ov, ov.Capture, err)
			}
			page, err := e.Timeline(ctx, cid, service.TimelineQuery{Order: "time", Limit: 100})
			if err != nil || page.Total != rep.SamplesWritten {
				t.Fatalf("%d rows, report %d samples (%v)", page.Total, rep.SamplesWritten, err)
			}
			first := page.Rows[0]
			if first.Kind != "txn" || first.Server != srv1.String() || first.Client != cl1.Addr().String() || !strings.Contains(first.Summary, "holding") {
				t.Errorf("first row %+v", first)
			}
			d, err := e.Detail(ctx, cid, first.Seq)
			if err != nil || d.Request == nil || d.Request.Hex == "" || d.Response == nil || len(d.Request.Fields) < 6 ||
				!slices.ContainsFunc(d.Notes, func(n service.Note) bool { return n.Code == "capture_sample" }) {
				t.Errorf("detail %+v %v", d, err)
			}
			exc, _ := e.Timeline(ctx, cid, service.TimelineQuery{Filter: service.Filter{Statuses: []string{"exception"}}, Order: "time", Limit: 10})
			if exc.Total != 1 || exc.Rows[0].Exception != 2 {
				t.Errorf("the exception sample: %+v", exc)
			}

			devs, _ := e.Devices(ctx, cid, service.Filter{})
			if len(devs) != 2 || devs[0].Server != srv1.String() {
				t.Errorf("devices %+v", devs)
			}
			conns, _ := e.Connections(ctx, cid, service.Filter{})
			if len(conns) != 2 || conns[0].Client == "" || conns[0].Open != "capture" {
				t.Errorf("streams %+v", conns)
			}
			pats, _ := e.Patterns(ctx, cid, service.Filter{})
			if len(pats) == 0 || pats[0].Requests == 0 {
				t.Errorf("patterns %+v", pats)
			}
			cov, _ := e.Coverage(ctx, cid, service.CoverageQuery{Server: srv1.String(), Unit: 1, Space: "holding"})
			if len(cov.Read) == 0 || cov.Read[0].Start != 100 {
				t.Errorf("coverage %+v", cov)
			}
			// The scripted register: its distinct values, in order; the
			// write of the trace is not in a capture.
			pts, _ := history(t, e, cid, service.HistoryQuery{Server: srv1.String(), Unit: 1, Space: "holding", Address: 101})
			var got []uint16
			for _, p := range pts {
				if p.Write {
					t.Error("a write in a capture's history")
				}
				got = append(got, p.Value)
			}
			if want := slices.Compact(slices.Clone(script)); !slices.Equal(got, want) {
				t.Errorf("history %v, want %v", got, want)
			}

			q, _ := e.Quality(ctx, cid)
			if !slices.ContainsFunc(q.Facts, func(f service.Fact) bool {
				return f.Label == "Producer" && strings.HasPrefix(f.Value, "modbus-trace-explorer")
			}) ||
				!slices.ContainsFunc(q.Conditions, func(c service.Condition) bool { return c.Code == "exceptions" && c.Count == 1 }) {
				t.Errorf("quality %+v", q)
			}
			fr := uint64(5)
			if l, _ := e.Locate(ctx, cid, service.LocateQuery{Frame: &fr}); l.Found || l.Note == "" {
				t.Errorf("go to frame in a capture: %+v", l)
			}
			for name, err := range map[string]error{
				"scan":    func() error { _, err := e.Scan(ctx, cid); return err }(),
				"analyze": func() error { _, err := e.Analyze(ctx, cid, service.AnalyzeConfig{}); return err }(),
				"export": func() error {
					_, err := e.ExportTimeline(ctx, cid, service.TimelineExport{Format: "csv"}, out+".csv")
					return err
				}(),
				"preview": func() error { _, err := e.PreviewMBCAP(ctx, cid, service.MBCAPExport{}); return err }(),
			} {
				if !isCode(err, service.CodeInvalidArgument) {
					t.Errorf("%s of a capture: %v", name, err)
				}
			}

			// Reopened, the import is reused.
			if err := e.Close(ctx, cid); err != nil {
				t.Fatal(err)
			}
			again, err := e.Open(ctx, []string{out})
			if err != nil || !again.Analyzed || again.Kind != "capture" {
				t.Fatalf("reopened %+v %v", again, err)
			}
			if d, err := e.Detail(ctx, again.ID, first.Seq); err != nil || d.Request == nil || d.Request.Hex == "" {
				t.Errorf("detail after reopening: %v", err)
			}
		})
	}
}

// Captures that cannot be read, or opened with other files, are refused.
func TestCaptureRefusals(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.mbcap")
	if err := os.WriteFile(broken, []byte("\x89MBCAP\r\nnot a capture"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, job := importCapture(t, e, broken)
	if job.State != service.JobFailed {
		t.Errorf("a broken capture: %+v", job)
	}
	trace := writeTrace(t, "site.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	if _, err := e.Open(ctx, []string{trace, broken}); !isCode(err, service.CodeInvalidArgument) {
		t.Errorf("a capture with a trace: %v", err)
	}
	info, _ := e.Open(ctx, []string{trace})
	if _, err := e.ImportCapture(ctx, info.ID); !isCode(err, service.CodeInvalidArgument) || info.Kind != "trace" {
		t.Errorf("importing a trace as a capture: %v", err)
	}
	for name, want := range map[string]string{"a.mbcap": "mbcap", "a.mbcap.jsonl": "jsonl", "b.jsonl": "jsonl", "a.mbcap.csv": "csv", "a.pcapng": ""} {
		if got := captureEncoding(filepath.Join(dir, name)); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}
