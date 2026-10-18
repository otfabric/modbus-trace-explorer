// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
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
	if err := e.Close(ctx, info.ID); err != nil {
		return fail("close", err)
	}
	return out
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
