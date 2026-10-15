// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

func TestParseFilter(t *testing.T) {
	first := time.Date(2026, 10, 14, 14, 2, 11, 0, time.Local).UnixNano()
	u16 := func(v uint16) *uint16 { return &v }
	i64 := func(v int64) *int64 { return &v }
	cases := map[string]service.Filter{
		"server:10.0.3.21:502 unit:1,2":       {Servers: []string{"10.0.3.21:502"}, Units: []uint8{1, 2}},
		"server:10.0.3.21 client:10.0.3.5":    {Servers: []string{"10.0.3.21"}, Clients: []string{"10.0.3.5"}},
		"conn:7 unit:3-5":                     {Conns: []uint32{7}, Units: []uint8{3, 4, 5}},
		"fc:3,4,0x2b":                         {Functions: []uint8{3, 4, 43}},
		"fc:read":                             {Classes: []string{"read"}},
		"fc:holding identification":           {Functions: []uint8{3, 43}},
		"class:write status:exception,orphan": {Classes: []string{"write"}, Statuses: []string{"exception", "orphan_response"}},
		"exc:2,3":                             {Exceptions: []uint8{2, 3}},
		"-flag:retransmitted flag:slow":       {NotFlags: []string{"retransmitted"}, Flags: []string{"slow"}},
		"addr:holding/4096-4160":              {Space: "holding", Start: u16(4096), End: u16(4160)},
		"addr:4100":                           {Start: u16(4100), End: u16(4100)},
		"addr:coils/0x10":                     {Space: "coil", Start: u16(16), End: u16(16)},
		"from:+15m":                           {FromNS: i64(first + int64(15*time.Minute))},
		"to:14:30":                            {ToNS: i64(time.Date(2026, 10, 14, 14, 30, 0, 0, time.Local).UnixNano())},
		"from:2026-10-14T15:00:00":            {FromNS: i64(time.Date(2026, 10, 14, 15, 0, 0, 0, time.Local).UnixNano())},
	}
	for text, want := range cases {
		got := parseFilter(text, first)
		if len(got.Errors) != 0 || !reflect.DeepEqual(got.Filter, want) {
			t.Errorf("%q: %+v %+v, want %+v", text, got.Filter, got.Errors, want)
		}
	}
	for _, bad := range []string{"bogus", "unit:300", "unit:x", "server:host", "client:10.0.0", "class:green", "status:fine",
		"flag:nope", "-unit:1", "addr:attic/1", "addr:9-1", "from:yesterday", "color:red", "fc:", "conn:5-99999"} {
		if got := parseFilter(bad, first); len(got.Errors) == 0 {
			t.Errorf("%q parsed: %+v", bad, got.Filter)
		}
	}
	e := newEngine(t)
	if got, _ := e.ParseFilter(context.Background(), "not-open", "unit:1"); len(got.Filter.Units) != 1 {
		t.Error("ParseFilter without a trace")
	}
}

func TestJobsAndEvents(t *testing.T) {
	e := newEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, _ := e.Subscribe(ctx)
	path := writeTrace(t, "site.pcap", tracetestOptions(), site)
	info, _ := e.Open(context.Background(), []string{path})
	ref, err := e.Scan(context.Background(), info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Scan(context.Background(), info.ID); err == nil {
		t.Log("second scan started after the first finished (allowed)")
	}
	var final *service.JobState
	for final == nil {
		select {
		case ev := <-events:
			if ev.Job != nil && ev.Job.ID == ref.ID && ev.Job.State != service.JobRunning {
				final = ev.Job
			}
		case <-time.After(10 * time.Second):
			t.Fatal("no final event")
		}
	}
	if final.State != service.JobDone || final.Kind != "scan" {
		t.Errorf("%+v", final)
	}
	if err := e.CancelJob(context.Background(), "no-such-job"); err == nil {
		t.Error("cancelled a job that does not exist")
	}
	if _, err := e.ScanResult(context.Background(), "nope"); err == nil {
		t.Error("scan result of nothing")
	}
	if _, err := e.Overview(context.Background(), info.ID); err == nil {
		t.Error("overview before analysis")
	}
}

func TestTexts(t *testing.T) {
	code := func(names []string, n string) uint8 { return store.Code(names, n) }
	st := store.StatusNames[:]
	cl := store.ClassNames[:]
	cases := map[string]store.Row{
		"16 bits": {Status: code(st, "ok"), Class: code(cl, "read"), FC: 1, Count: 16},
		"3 words": {Status: code(st, "ok"), Class: code(cl, "read"), FC: 3, Count: 3},
		"OK":      {Status: code(st, "ok"), Class: code(cl, "write"), FC: 6, Count: 1},
		"Exception 0B Gateway target device failed to respond": {Status: code(st, "exception"), Exc: 0x0B},
		"Exception 09 Unknown exception":                       {Status: code(st, "exception"), Exc: 9},
		"No response observed":                                 {Status: code(st, "unanswered")},
		"Response without request":                             {Status: code(st, "orphan_response")},
		"Ambiguous":                                            {Status: code(st, "ambiguous")},
		"Malformed":                                            {Status: code(st, "malformed")},
		"Uncertain (gap)":                                      {Status: code(st, "uncertain")},
		"":                                                     {},
	}
	for want, r := range cases {
		if got := resultText(r); got != want {
			t.Errorf("%+v: %q, want %q", r, got, want)
		}
	}
	events := map[string]struct {
		kind  uint8
		extra string
	}{
		"Connection seen mid-stream":                                {store.KindConnOpen, `{"open":"midstream"}`},
		"Connection opened (SYN)":                                   {store.KindConnOpen, `{"open":"syn"}`},
		"Connection closed (RST) after 3 transactions":              {store.KindConnClose, `{"close":"rst","transactions":3}`},
		"Connection closed (odd) after 0 transactions":              {store.KindConnClose, `{"close":"odd"}`},
		"Gap: 10 bytes missing (client to server, loss)":            {store.KindGap, `{"dir":"c2s","bytes":10,"reason":"loss"}`},
		"Framing recovered after 4 bytes (confidence: next header)": {store.KindResync, `{"skipped":4,"confidence":"next_header"}`},
		"12 dropped": {store.KindNote, `{"text":"12 dropped"}`},
		"x":          {99, `{}`},
	}
	for want, ev := range events {
		got := eventText(ev.kind, []byte(ev.extra))
		if want == "x" {
			want = ""
		}
		if got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
	if meaningOfWarning("empty_pdu") == "" || meaningOfWarning("zzz") == "" || reasonText("new_reason") == "" {
		t.Error("meanings")
	}
	if DefaultCacheDir() == "" || DefaultConfigDir() == "" {
		t.Error("default dirs")
	}
}

// Cancelling an analysis leaves no index and lets the trace be analysed
// again.
func TestCancelAnalysis(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	path := writeTrace(t, "big.pcap", tracetestOptions(), func(b *tracetest.Builder) {
		c := b.Conn(tracetest.ConnOptions{})
		for i := range 20000 {
			c.Request(uint16(i), 1, tracetest.ReadRequest(3, 0, 10))
			c.Response(uint16(i), 1, tracetest.RegistersResponse(3, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10))
		}
	})
	info, _ := e.Open(ctx, []string{path})
	ref, err := e.Analyze(ctx, info.ID, service.AnalyzeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Scan(ctx, info.ID); err == nil {
		t.Error("a scan started during an analysis")
	}
	_ = e.CancelJob(ctx, ref.ID)
	e.wait(ref.ID)
	if _, err := e.Overview(ctx, info.ID); err == nil {
		t.Log("the analysis finished before the cancel arrived")
	} else if _, err := os.Stat(e.cache.Dir(info.ID) + ".partial"); !os.IsNotExist(err) {
		t.Error("a partial index was left behind")
	}
	ref, err = e.Analyze(ctx, info.ID, service.AnalyzeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	e.wait(ref.ID)
	if ov, err := e.Overview(ctx, info.ID); err != nil || ov.Transactions != 20000 {
		t.Errorf("%+v %v", ov, err)
	}
}
