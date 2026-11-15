// SPDX-License-Identifier: MIT

package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// fuzzSuffixes are the names a fuzzed file gets: each takes another path
// through Open (a trace, gzip, each capture encoding).
var fuzzSuffixes = []string{".pcap", ".pcapng", ".pcapng.gz", ".mbcap", ".mbcap.jsonl", ".mbcap.csv"}

// FuzzOpen feeds hostile bytes, seeded with the corpus, through everything
// the window does with a file (runCorpusFile): whatever the input, the
// engine answers with a result or an error, never a panic or a hang.
// The seeds run with go test; make fuzz runs it for longer.
func FuzzOpen(f *testing.F) {
	for _, c := range loadCorpus(f) {
		data, err := os.ReadFile(filepath.Join(corpusDir, c.File))
		if err != nil || len(data) > 64<<10 {
			continue // the large realistic files take too long per input
		}
		for k, s := range fuzzSuffixes {
			if strings.HasSuffix(c.File, s) {
				f.Add(data, uint8(k))
			}
		}
	}
	e := newEngine(f)
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, data []byte, suffix uint8) {
		path := filepath.Join(dir, "input"+fuzzSuffixes[int(suffix)%len(fuzzSuffixes)])
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		_ = runCorpusFile(t, e, path)
		if d := time.Since(start); d > 2*time.Second && os.Getenv("MTE_FUZZ_SLOW") != "" {
			_ = os.WriteFile(filepath.Join(os.Getenv("MTE_FUZZ_SLOW"), fmt.Sprintf("slow-%d%s", d.Milliseconds(), filepath.Ext(path))), data, 0o600)
		}
	})
}

// FuzzParseFilter feeds any text to the filter box's parser: whatever it
// takes from the text, written back as filter text (printFilter) it parses
// again without an error to the same filter.
func FuzzParseFilter(f *testing.F) {
	for _, s := range []string{
		"server:10.0.3.21:502 unit:1,2", "server:10.0.3.21 client:10.0.3.5", "conn:7 unit:3-5", "fc:3,4,0x2b", "fc:read",
		"fc:holding identification", "class:write status:exception,orphan", "exc:2,3", "-flag:retransmitted flag:slow",
		"addr:holding/4096-4160", "addr:4100", "addr:coils/0x10", "addr:input/", "from:+15m", "to:14:30",
		"from:2026-10-14T15:00:00", "from:2026-10-14T15:00:00.123456789+02:00 to:+1h30m", "server:[fe80::1]:502 client:fe80::1",
		"bogus", "unit:300", "unit:1,x", "addr:holding/x", "-unit:1", "fc:", "conn:5-99999",
	} {
		f.Add(s)
	}
	first := time.Date(2026, 10, 14, 14, 2, 11, 0, time.UTC).UnixNano()
	f.Fuzz(func(t *testing.T, text string) {
		got := parseFilter(text, first)
		back := printFilter(got.Filter)
		again := parseFilter(back, first)
		if len(again.Errors) != 0 || !reflect.DeepEqual(again.Filter, got.Filter) {
			t.Fatalf("%q parsed to %+v; written back as %q it parses to %+v, %+v", text, got.Filter, back, again.Filter, again.Errors)
		}
	})
}

// printFilter writes a filter as filter text: one term a field, times as
// RFC 3339 in UTC.
func printFilter(f service.Filter) string {
	var terms []string
	add := func(key string, vals []string) {
		if len(vals) > 0 {
			terms = append(terms, key+":"+strings.Join(vals, ","))
		}
	}
	nums := func(n []uint64) []string {
		out := make([]string, len(n))
		for i, v := range n {
			out[i] = strconv.FormatUint(v, 10)
		}
		return out
	}
	wide := func(u []uint8) []uint64 {
		out := make([]uint64, len(u))
		for i, v := range u {
			out[i] = uint64(v)
		}
		return out
	}
	conns := make([]uint64, len(f.Conns))
	for i, v := range f.Conns {
		conns[i] = uint64(v)
	}
	add("server", f.Servers)
	add("client", f.Clients)
	add("unit", nums(wide(f.Units)))
	add("conn", nums(conns))
	add("fc", nums(wide(f.Functions)))
	add("class", f.Classes)
	add("status", f.Statuses)
	add("exc", nums(wide(f.Exceptions)))
	add("flag", f.Flags)
	add("-flag", f.NotFlags)
	switch {
	case f.Start != nil && f.End != nil && f.Space != "":
		terms = append(terms, fmt.Sprintf("addr:%s/%d-%d", f.Space, *f.Start, *f.End))
	case f.Start != nil && f.End != nil:
		terms = append(terms, fmt.Sprintf("addr:%d-%d", *f.Start, *f.End))
	case f.Space != "":
		terms = append(terms, "addr:"+f.Space+"/")
	}
	for key, ns := range map[string]*int64{"from": f.FromNS, "to": f.ToNS} {
		if ns != nil {
			terms = append(terms, key+":"+time.Unix(0, *ns).UTC().Format(time.RFC3339Nano))
		}
	}
	return strings.Join(terms, " ")
}
