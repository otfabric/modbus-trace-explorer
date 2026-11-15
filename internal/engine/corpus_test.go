// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

const corpusDir = "../../testdata/corpus"

// corpusEntry is a line of testdata/corpus/manifest.json (internal/corpus).
type corpusEntry struct {
	File            string            `json:"file"`
	Category        string            `json:"category"`
	Kind            string            `json:"kind"`
	Expect          string            `json:"expect"`
	MinTransactions uint64            `json:"min_transactions"`
	Error           string            `json:"error"`
	Warning         string            `json:"warning"`
	Quality         map[string]uint64 `json:"quality"`
}

func loadCorpus(t testing.TB) []corpusEntry {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(corpusDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m []corpusEntry
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// outcome is what the engine made of a corpus file.
type outcome struct {
	kind         string
	err          string // the first error, with the step that gave it
	warning      string // the import's warnings
	transactions uint64
	quality      map[string]uint64
}

// waitJob waits for a job's end on events.
func waitJob(t testing.TB, events <-chan service.Event, id string) service.JobState {
	t.Helper()
	timeout := time.After(2 * time.Minute)
	for {
		select {
		case ev := <-events:
			if ev.Job != nil && ev.Job.ID == id && ev.Job.State != service.JobRunning {
				return *ev.Job
			}
		case <-timeout:
			t.Fatalf("job %s did not end", id)
		}
	}
}

// runCorpusFile takes a file through everything the window does with it:
// open, scan and analyse (or import a capture), overview, timeline pages,
// the detail of the first rows, every view, address histories, quality.
// Every error is reported as the outcome; a panic fails the test.
func runCorpusFile(t testing.TB, e *Engine, paths ...string) outcome {
	t.Helper()
	ctx := context.Background()
	sub, cancel := context.WithCancel(ctx)
	defer cancel() // a subscriber nobody reads would slow every job's last event
	events, err := e.Subscribe(sub)
	if err != nil {
		t.Fatal(err)
	}
	fail := func(step string, err error) outcome { return outcome{err: step + ": " + err.Error()} }
	info, err := e.Open(ctx, paths)
	if err != nil {
		return fail("open", err)
	}
	out := outcome{kind: info.Kind}
	if !info.Analyzed {
		if info.Kind == "capture" {
			ref, err := e.ImportCapture(ctx, info.ID)
			if err != nil {
				return fail("import", err)
			}
			j := waitJob(t, events, ref.ID)
			if j.State != service.JobDone {
				return fail("import", fmt.Errorf("%s: %v", j.State, j.Error))
			}
			out.warning += j.Warning
		} else {
			ref, err := e.Scan(ctx, info.ID)
			if err != nil {
				return fail("scan", err)
			}
			j := waitJob(t, events, ref.ID)
			if j.State != service.JobDone {
				return fail("scan", fmt.Errorf("%s: %v", j.State, j.Error))
			}
			out.warning += j.Warning
			res, err := e.ScanResult(ctx, info.ID)
			if err != nil {
				return fail("scan result", err)
			}
			ref, err = e.Analyze(ctx, info.ID, service.AnalyzeConfig{Flows: res.Selected, ServerPorts: res.ServerPorts})
			if err != nil {
				return fail("analyze", err)
			}
			j = waitJob(t, events, ref.ID)
			if j.State != service.JobDone {
				return fail("analyze", fmt.Errorf("%s: %v", j.State, j.Error))
			}
			out.warning += j.Warning
		}
	}
	ov, err := e.Overview(ctx, info.ID)
	if err != nil {
		return fail("overview", err)
	}
	out.transactions = ov.Transactions
	for _, order := range []string{"time", "latency"} {
		for _, desc := range []bool{false, true} {
			p, err := e.Timeline(ctx, info.ID, service.TimelineQuery{Order: order, Desc: desc, Limit: 50})
			if err != nil {
				return fail("timeline", err)
			}
			for _, r := range p.Rows[:min(len(p.Rows), 20)] {
				if _, err := e.Detail(ctx, info.ID, r.Seq); err != nil {
					return fail(fmt.Sprintf("detail %d", r.Seq), err)
				}
			}
		}
	}
	devs, err := e.Devices(ctx, info.ID, service.Filter{})
	if err != nil {
		return fail("devices", err)
	}
	if _, err := e.Connections(ctx, info.ID, service.Filter{}); err != nil {
		return fail("connections", err)
	}
	if _, err := e.Patterns(ctx, info.ID, service.Filter{}); err != nil {
		return fail("patterns", err)
	}
	for _, d := range devs[:min(len(devs), 4)] {
		for _, space := range []string{"coil", "discrete", "holding", "input"} {
			cov, err := e.Coverage(ctx, info.ID, service.CoverageQuery{Server: d.Server, Unit: d.Unit, Space: space})
			if err != nil {
				return fail("coverage", err)
			}
			if len(cov.Read) > 0 {
				ref, err := e.AddressHistory(ctx, info.ID, service.HistoryQuery{Server: d.Server, Unit: d.Unit, Space: space, Address: cov.Read[0].Start})
				if err != nil {
					return fail("history", err)
				}
				if j := waitJob(t, events, ref.ID); j.State != service.JobDone {
					return fail("history", fmt.Errorf("%s: %v", j.State, j.Error))
				}
			}
		}
	}
	q, err := e.Quality(ctx, info.ID)
	if err != nil {
		return fail("quality", err)
	}
	out.quality = map[string]uint64{}
	for _, c := range q.Conditions {
		out.quality[c.Code] = c.Count
	}
	if _, err := e.Diagnostics(ctx, info.ID); err != nil {
		return fail("diagnostics", err)
	}
	if err := invariants(ctx, e, info.ID, ov); err != nil {
		t.Errorf("%s: %v", strings.Join(paths, ", "), err)
	}
	if err := e.Close(ctx, info.ID); err != nil {
		return fail("close", err)
	}
	return out
}

// invariants checks what holds for the index of any input: the overview,
// the index and the timeline agree on the row count in every order; each
// order is a permutation of the rows, sorted by its key and then by row;
// the timeline's totals of a status, in time and in latency order and from
// either end, are the numbers of its rows and of those with a latency; a
// time split parts the rows; and every frame reference of a trace decodes
// and names a frame of one of its files.
func invariants(ctx context.Context, e *Engine, id string, ov service.Overview) error {
	_, ix, release, err := e.index(id)
	if err != nil {
		return err
	}
	defer release()
	n := ix.Rows()
	if ov.Rows != n {
		return fmt.Errorf("the overview has %d rows, the index %d", ov.Rows, n)
	}
	key := [...]func(r store.Row) int64{
		store.ByTime:    func(r store.Row) int64 { return r.TimeNS },
		store.ByLatency: func(r store.Row) int64 { return int64(r.LatUS) },
	}
	for order, key := range key {
		sel, err := ix.Select(ctx, &store.Filter{}, store.Order(order))
		if err != nil {
			return err
		}
		if sel.Len() != n {
			return fmt.Errorf("order %d holds %d of %d rows", order, sel.Len(), n)
		}
		seen := make([]bool, n)
		var prev uint32
		for k := range n {
			i := sel.At(k, false)
			if uint64(i) >= n || seen[i] {
				return fmt.Errorf("order %d: rank %d is row %d, past the end or again", order, k, i)
			}
			seen[i] = true
			if k > 0 {
				a, b := key(ix.Row(prev)), key(ix.Row(i))
				if a > b || a == b && prev > i {
					return fmt.Errorf("order %d: row %d (%d) before row %d (%d)", order, prev, a, i, b)
				}
			}
			prev = i
		}
	}
	// The rows of each status, and of those the ones with a latency (the
	// latency order holds only transactions with one); index 0 counts
	// every row and every transaction with a latency.
	var rows, timed [len(store.StatusNames)]uint64
	for i := range n {
		r := ix.Row(uint32(i))
		rows[0]++
		if r.Kind != store.KindTxn || int(r.Status) >= len(rows) {
			continue
		}
		lat := uint64(0)
		if r.LatUS != store.NoLatency {
			lat = 1
		}
		timed[0] += lat
		if r.Status != 0 {
			rows[r.Status]++
			timed[r.Status] += lat
		}
	}
	// totals returns the timeline's totals of a filter in time and in
	// latency order, the same from either end.
	totals := func(f service.Filter) (byTime, byLatency uint64, err error) {
		var got [4]uint64
		for k, q := range []service.TimelineQuery{{Order: "time"}, {Order: "time", Desc: true}, {Order: "latency"}, {Order: "latency", Desc: true}} {
			q.Filter, q.Limit = f, 1
			p, err := e.Timeline(ctx, id, q)
			if err != nil {
				return 0, 0, err
			}
			got[k] = p.Total
		}
		if got[0] != got[1] || got[2] != got[3] {
			return 0, 0, fmt.Errorf("filter %+v: totals %v ascending and descending", f, got)
		}
		return got[0], got[2], nil
	}
	for s, name := range store.StatusNames {
		f := service.Filter{}
		if name != "" {
			f.Statuses = []string{name}
		}
		byTime, byLatency, err := totals(f)
		if err != nil || byTime != rows[s] || byLatency != timed[s] {
			return fmt.Errorf("status %q: timeline totals %d and %d, rows %d and %d: %v", name, byTime, byLatency, rows[s], timed[s], err)
		}
	}
	if n > 0 {
		mid := ix.Row(uint32(n / 2)).TimeNS
		before, _, err1 := totals(service.Filter{ToNS: &mid})
		after, _, err2 := totals(service.Filter{FromNS: &mid})
		if err := errors.Join(err1, err2); err != nil || before+after != n {
			return fmt.Errorf("a time split at %d parts %d rows into %d and %d: %v", mid, n, before, after, err)
		}
	}
	if ov.Summary == nil {
		return nil // a capture: no frame references
	}
	parts := max(1, len(ov.Summary.Inputs))
	for i := range n {
		req, resp, err := ix.Refs(ix.Row(uint32(i)))
		if err != nil {
			return fmt.Errorf("row %d: %v", i, err)
		}
		for _, f := range append(req, resp...) {
			if f.Frame < 1 || f.Frame > ov.Summary.Packets || int(f.Part) >= parts {
				return fmt.Errorf("row %d names frame %d of part %d; the trace has %d packets in %d files", i, f.Frame, f.Part, ov.Summary.Packets, parts)
			}
		}
	}
	return nil
}

// The corpus (testdata/corpus): every file is taken as its manifest says,
// realistic and hard ones shown, broken ones refused with a message or
// shown as far as they can be read, and nothing crashes.
func TestCorpus(t *testing.T) {
	e := newEngine(t)
	for _, c := range loadCorpus(t) {
		t.Run(c.File, func(t *testing.T) {
			got := runCorpusFile(t, e, filepath.Join(corpusDir, c.File))
			switch c.Expect {
			case "error":
				if got.err == "" {
					t.Fatalf("taken without an error (%d transactions)", got.transactions)
				}
				if !strings.Contains(strings.ToLower(got.err), strings.ToLower(c.Error)) {
					t.Errorf("error %q does not say %q", got.err, c.Error)
				}
				return
			case "empty":
				if got.err != "" || got.transactions != 0 {
					t.Fatalf("want an empty result: %+v", got)
				}
			case "ok":
				if got.err != "" {
					t.Fatalf("refused: %s", got.err)
				}
				if got.transactions < c.MinTransactions {
					t.Errorf("%d transactions, want at least %d", got.transactions, c.MinTransactions)
				}
			default:
				t.Fatalf("unknown expectation %q", c.Expect)
			}
			if !strings.Contains(strings.ToLower(got.warning), strings.ToLower(c.Warning)) {
				t.Errorf("warning %q does not say %q", got.warning, c.Warning)
			}
			if c.Warning == "" && got.warning != "" && c.Category != "broken" {
				t.Errorf("an unexpected warning: %q", got.warning)
			}
			if got.kind != c.Kind {
				t.Errorf("opened as %s, want %s", got.kind, c.Kind)
			}
			for code, n := range c.Quality {
				if got.quality[code] < n {
					t.Errorf("quality %s: %d, want at least %d", code, got.quality[code], n)
				}
			}
		})
	}
	// The ring buffer: three files, in any order, are one trace.
	ring := []string{"plant-ring_00003.pcapng", "plant-ring_00001.pcapng", "plant-ring_00002.pcapng"}
	var paths []string
	for _, f := range ring {
		paths = append(paths, filepath.Join(corpusDir, "realistic", f))
	}
	one := runCorpusFile(t, e, filepath.Join(corpusDir, "realistic/plant-scada.pcapng"))
	all := runCorpusFile(t, e, paths...)
	if all.err != "" || all.transactions != one.transactions {
		t.Errorf("the ring buffer: %+v; the whole trace has %d transactions", all, one.transactions)
	}
}
