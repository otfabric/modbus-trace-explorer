// SPDX-License-Identifier: MIT

package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/otfabric/go-modbus-capture/capture"

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

// Targets and streams as the device and client columns name them.
func TestCaptureNames(t *testing.T) {
	for _, c := range []struct {
		t    capture.Target
		want string
	}{
		{capture.Target{Host: "10.0.0.2", Port: 502}, "10.0.0.2:502"},
		{capture.Target{Host: "fd00::2", Port: 502}, "[fd00::2]:502"},
		{capture.Target{Serial: capture.Serial{Device: "COM3"}}, "COM3"},
		{capture.Target{Label: "Meter"}, "Meter"},
		{capture.Target{ID: 4}, "target 4"},
	} {
		if got := targetName(c.t); got != c.want {
			t.Errorf("%+v: %q, want %q", c.t, got, c.want)
		}
	}
	for _, c := range []struct {
		s    capture.Stream
		want string
	}{
		{capture.Stream{Label: "client 10.0.0.9"}, "10.0.0.9"},
		{capture.Stream{Label: "client not-an-address"}, "client not-an-address"},
		{capture.Stream{Label: "poll 1 s"}, "poll 1 s"},
		{capture.Stream{ID: 2}, "stream 2"},
	} {
		if got := streamClient(c.s); got != c.want {
			t.Errorf("%+v: %q, want %q", c.s, got, c.want)
		}
	}
	if r, c, n, err := openCapture(filepath.Join(t.TempDir(), "missing.mbcap"), "mbcap"); err == nil || r != nil || c != nil || n != nil {
		t.Error("a missing capture opened")
	}
	p := filepath.Join(t.TempDir(), "x.txt")
	_ = os.WriteFile(p, []byte("x"), 0o600)
	if _, _, _, err := openCapture(p, "xml"); err == nil {
		t.Error("an unknown encoding opened")
	}
}

// jsonlCapture writes a capture in JSON Lines from its lines.
func jsonlCapture(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.mbcap.jsonl")
	head := `{"t":"header","version":"0.1","tick_ns":1000,"wall_start":"2026-10-14T06:58:00Z","producer":"test","flags":{"exceptions":true}}`
	if err := os.WriteFile(p, []byte(strings.Join(append([]string{head}, lines...), "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// An exception and a read become rows with their status and range; a
// sample of a stream never defined is a damaged capture.
func TestCaptureSamples(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	p := jsonlCapture(t,
		`{"t":"target","id":1,"transport":"tcp","host":"10.0.0.2","port":502,"unit":1}`,
		`{"t":"stream","id":1,"target":1,"role":"poll","label":"client 10.0.0.9"}`,
		`{"t":"s","s":1,"at":0,"req":"0300000001","resp":"8302"}`,
		`{"t":"s","s":1,"at":1000,"req":"0400320002","resp":"040400010002"}`,
		`{"t":"end","at":5000}`)
	id, job := importCapture(t, e, p)
	if job.State != service.JobDone {
		t.Fatalf("import %+v", job)
	}
	page, _ := e.Timeline(ctx, id, service.TimelineQuery{Order: "time", Limit: 10})
	if page.Total != 2 || page.Rows[0].Status != "exception" || page.Rows[0].Exception != 2 || page.Rows[1].FC == nil || *page.Rows[1].FC != 4 {
		t.Fatalf("rows %+v", page.Rows)
	}
	q, _ := e.Quality(ctx, id)
	if !slices.ContainsFunc(q.Conditions, func(c service.Condition) bool { return c.Code == "exceptions" && c.Count == 1 }) {
		t.Errorf("quality %+v", q.Conditions)
	}
	cov, _ := e.Coverage(ctx, id, service.CoverageQuery{Server: "10.0.0.2:502", Unit: 1, Space: "input"})
	if len(cov.Read) != 1 || cov.Read[0].Start != 50 || cov.Read[0].End != 51 {
		t.Errorf("the read range: %+v", cov)
	}

	// The readers refuse a sample of a stream never defined.
	orphan := jsonlCapture(t,
		`{"t":"target","id":1,"transport":"tcp","host":"10.0.0.2","port":502,"unit":1}`,
		`{"t":"s","s":7,"at":0,"req":"0300000001","resp":"03020001"}`)
	if _, job := importCapture(t, e, orphan); job.State != service.JobFailed {
		t.Errorf("a sample of an unknown stream: %+v", job)
	}
}

// Import refusals, a second import of a capture in use, and a damaged
// cache entry under the detail and the address history.
func TestCaptureImportAndCache(t *testing.T) {
	e, tid, _ := exportTrace(t)
	ctx := context.Background()
	if _, err := e.ImportCapture(ctx, "nope"); !isCode(err, service.CodeNotFound) {
		t.Errorf("an unknown file: %v", err)
	}
	out := filepath.Join(t.TempDir(), "x.mbcap")
	ref, err := e.ExportMBCAP(ctx, tid, service.MBCAPExport{Exceptions: true}, out)
	runExport(t, e, ref, err)
	cid, _ := importCapture(t, e, out)
	tr, _ := e.trace(cid)
	if !tr.claim() {
		t.Fatal("cannot claim")
	}
	if _, err := e.ImportCapture(ctx, cid); !isCode(err, service.CodeInvalidArgument) {
		t.Errorf("an import while one runs: %v", err)
	}
	tr.unclaim()

	// The detail opens the PDUs; importing again replaces them.
	if d, err := e.Detail(ctx, cid, 1); err != nil || d.Request == nil {
		t.Fatalf("detail: %v", err)
	}
	events, _ := e.Subscribe(ctx)
	ref, err = e.ImportCapture(ctx, cid)
	if err != nil {
		t.Fatal(err)
	}
	if j := waitJob(t, events, ref.ID); j.State != service.JobDone {
		t.Fatalf("second import %+v", j)
	}
	if d, err := e.Detail(ctx, cid, 1); err != nil || d.Request == nil || d.Request.Hex == "" {
		t.Fatalf("detail after the second import: %v", err)
	}

	// A damaged entry: the PDUs' index cut, then gone.
	_, ix, _ := e.index(cid)
	idx := filepath.Join(ix.Dir(), pduIndex)
	if err := os.Truncate(idx, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Detail(ctx, cid, 2); !isCode(err, service.CodeInternal) {
		t.Errorf("detail with a cut index: %v", err)
	}
	ref, err = e.AddressHistory(ctx, cid, service.HistoryQuery{Server: srv1.String(), Unit: 1, Space: "holding", Address: 100})
	if err != nil {
		t.Fatal(err)
	}
	if j := waitJob(t, events, ref.ID); j.State != service.JobFailed {
		t.Errorf("history on a damaged entry: %+v", j)
	}
	_ = e.Close(ctx, cid)
	again, _ := e.Open(ctx, []string{out})
	if err := os.Remove(filepath.Join(ix.Dir(), pduData)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Detail(ctx, again.ID, 1); !isCode(err, service.CodeInternal) {
		t.Errorf("detail without the PDUs: %v", err)
	}
	if err := os.Remove(idx); err != nil {
		t.Fatal(err)
	}
	if _, err := openPDUs(ix.Dir()); err == nil {
		t.Error("PDUs opened without their files")
	}
	if err := os.WriteFile(filepath.Join(ix.Dir(), pduData), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openPDUs(ix.Dir()); err == nil {
		t.Error("PDUs opened without their index")
	}
	if _, err := newPDUWriter(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("PDU writer in a missing folder")
	}
	clash := t.TempDir()
	if err := os.Mkdir(filepath.Join(clash, pduIndex), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := newPDUWriter(clash); err == nil {
		t.Error("PDU writer over a folder")
	}
}

// The address history of a trace that changed on disk after its analysis:
// rows whose bytes cannot be read any more are left out; the job ends.
func TestHistoryOfAChangedTrace(t *testing.T) {
	ctx := context.Background()
	for _, change := range []string{"garbage", "cut"} {
		t.Run(change, func(t *testing.T) {
			e, id, path := exportTrace(t)
			raw, _ := os.ReadFile(path)
			switch change {
			case "garbage":
				_ = os.WriteFile(path, bytes.Repeat([]byte{0xAB}, len(raw)), 0o600)
			case "cut":
				_ = os.WriteFile(path, raw[:len(raw)/3], 0o600)
			}
			events, _ := e.Subscribe(ctx)
			ref, err := e.AddressHistory(ctx, id, service.HistoryQuery{Server: srv1.String(), Unit: 1, Space: "holding", Address: 101})
			if err != nil {
				t.Fatal(err)
			}
			var points int
			for ev := range events {
				if ev.History != nil && ev.History.Job == ref.ID {
					points += len(ev.History.Points)
				}
				if ev.Job != nil && ev.Job.ID == ref.ID && ev.Job.State != service.JobRunning {
					if ev.Job.State != service.JobDone {
						t.Errorf("history of a %s file: %+v", change, ev.Job)
					}
					break
				}
			}
			if change == "garbage" && points != 0 {
				t.Errorf("%d points from a garbage file", points)
			}
		})
	}
}

// An import into a cache that cannot be written fails with a message; a
// cache entry whose capture document is damaged is imported again.
func TestCaptureCacheFailures(t *testing.T) {
	ctx := context.Background()
	e, tid, _ := exportTrace(t)
	out := filepath.Join(t.TempDir(), "x.mbcap")
	ref, err := e.ExportMBCAP(ctx, tid, service.MBCAPExport{Exceptions: true}, out)
	runExport(t, e, ref, err)

	cid, _ := importCapture(t, e, out)
	_, ix, _ := e.index(cid)
	_ = e.Close(ctx, cid)
	if err := os.WriteFile(filepath.Join(ix.Dir(), captureDoc), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if again, err := e.Open(ctx, []string{out}); err != nil || again.Analyzed {
		t.Errorf("a damaged capture document: %+v %v", again, err)
	}
	_ = e.Close(ctx, cid)

	root := t.TempDir()
	ro, err := New(Options{CacheDir: filepath.Join(root, "cache"), ConfigDir: filepath.Join(root, "config")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ro.Shutdown() })
	if err := os.Chmod(filepath.Join(root, "cache"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "cache"), 0o700) })
	if _, job := importCapture(t, ro, out); job.State != service.JobFailed || job.Error == nil || job.Error.Code != service.CodeUnavailable {
		t.Errorf("an import into a read-only cache: %+v", job)
	}
}
