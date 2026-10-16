// SPDX-License-Identifier: MIT

package engine

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

func newEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(Options{CacheDir: filepath.Join(t.TempDir(), "cache"), ConfigDir: filepath.Join(t.TempDir(), "config")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Shutdown() })
	return e
}

// writeTrace builds a trace with the library's builder and writes it.
func writeTrace(t *testing.T, name string, o tracetest.Options, script func(b *tracetest.Builder)) string {
	t.Helper()
	var buf bytes.Buffer
	b, err := tracetest.New(&buf, o)
	if err != nil {
		t.Fatal(err)
	}
	script(b)
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

var (
	srv1 = netip.MustParseAddrPort("10.0.0.2:502")
	srv2 = netip.MustParseAddrPort("10.0.0.3:1502")
	cl1  = netip.MustParseAddrPort("10.0.0.10:40000")
	cl2  = netip.MustParseAddrPort("10.0.0.11:40001")
)

// site is A1's trace: two clients, two servers, two units, several
// functions, an exception and a write.
func site(b *tracetest.Builder) {
	c := b.Conn(tracetest.ConnOptions{Client: cl1, Server: srv1})
	for i := range 5 {
		c.Request(uint16(i), 1, tracetest.ReadRequest(3, 100, 2))
		c.Response(uint16(i), 1, tracetest.RegistersResponse(3, uint16(i), 7))
	}
	c.Request(9, 2, tracetest.ReadRequest(4, 0, 1))
	c.Response(9, 2, tracetest.Exception(4, 2))
	c.Request(10, 1, []byte{6, 0, 5, 0, 80})
	c.Response(10, 1, []byte{6, 0, 5, 0, 80})
	c.Close()
	d := b.Conn(tracetest.ConnOptions{Client: cl2, Server: srv2})
	for i := range 3 {
		d.Request(uint16(i), 1, tracetest.ReadRequest(1, 0, 16))
		d.Response(uint16(i), 1, []byte{1, 2, 0xAA, 0x55})
	}
	d.Close()
}

// importTrace opens, scans and analyses paths with cfg (nil: the scan's
// suggestion), and returns the file ID.
func importTrace(t *testing.T, e *Engine, cfg *service.AnalyzeConfig, paths ...string) (string, service.ScanResult) {
	t.Helper()
	ctx := context.Background()
	info, err := e.Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := e.Scan(ctx, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.wait(ref.ID)
	res, err := e.ScanResult(ctx, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		cfg = &service.AnalyzeConfig{Flows: res.Selected, ServerPorts: res.ServerPorts}
	}
	ref, err = e.Analyze(ctx, info.ID, *cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.wait(ref.ID)
	return info.ID, res
}

func page(t *testing.T, e *Engine, id string, f service.Filter) service.TimelinePage {
	t.Helper()
	p, err := e.Timeline(context.Background(), id, service.TimelineQuery{Filter: f, Order: "time", Limit: MaxPage})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func txns(p service.TimelinePage) []service.Row {
	var out []service.Row
	for _, r := range p.Rows {
		if r.Kind == "txn" {
			out = append(out, r)
		}
	}
	return out
}

// A1: the timeline's transactions equal the script's, in order, for pcap
// and pcapng.
func TestImportTimeline(t *testing.T) {
	for _, format := range []tracetest.Format{tracetest.Pcap, tracetest.PcapNG} {
		e := newEngine(t)
		path := writeTrace(t, "site.trace", tracetest.Options{Format: format}, site)
		id, res := importTrace(t, e, nil, path)
		if len(res.Selected) != 2 || !res.OneClick && len(res.ServerPorts) < 2 {
			t.Errorf("scan: %+v", res)
		}
		p := page(t, e, id, service.Filter{})
		rows := txns(p)
		if len(rows) != 10 {
			t.Fatalf("%d transactions: %+v", len(rows), rows)
		}
		for k := 1; k < len(p.Rows); k++ {
			if p.Rows[k].TimeNS < p.Rows[k-1].TimeNS {
				t.Fatal("not in time order")
			}
		}
		first := rows[0]
		if first.Server != srv1.String() || first.Client != cl1.Addr().String() || *first.Unit != 1 || *first.FC != 3 ||
			first.Status != "ok" || first.Summary != "Read 2 holding registers @ 100" || first.Result != "2 words" || first.LatencyNS == nil {
			t.Errorf("first: %+v", first)
		}
		var exc, write, coils int
		for _, r := range rows {
			switch {
			case r.Status == "exception":
				exc++
				if r.Result != "Exception 02 Illegal data address" || *r.Unit != 2 {
					t.Errorf("exception: %+v", r)
				}
			case r.Class == "write":
				write++
			case *r.FC == 1:
				coils++
				if r.Result != "16 bits" || r.Server != srv2.String() {
					t.Errorf("coils: %+v", r)
				}
			}
		}
		if exc != 1 || write != 1 || coils != 3 {
			t.Errorf("exceptions %d, writes %d, coils %d", exc, write, coils)
		}
		ov, err := e.Overview(context.Background(), id)
		if err != nil || ov.Transactions != 10 || ov.Devices != 3 || ov.Connections != 2 || ov.TraceMissing || ov.Summary == nil {
			t.Errorf("overview %+v %v", ov, err)
		}
		// Descending and by latency.
		down, _ := e.Timeline(context.Background(), id, service.TimelineQuery{Order: "time", Desc: true, Limit: 1})
		if down.Rows[0].Seq != p.Rows[len(p.Rows)-1].Seq {
			t.Error("descending")
		}
		lat, _ := e.Timeline(context.Background(), id, service.TimelineQuery{Order: "latency", Limit: MaxPage})
		if lat.Total != 10 {
			t.Errorf("latency order holds %d rows", lat.Total)
		}
	}
}

// A2: Modbus on non-standard ports next to other TCP; a TLS flow is
// classed TLS; a flow the scan does not class can be forced by naming its
// server.
func TestFlowsStep(t *testing.T) {
	e := newEngine(t)
	odd := netip.MustParseAddrPort("10.0.0.5:5021")
	web := netip.MustParseAddrPort("10.0.0.6:80")
	tls := netip.MustParseAddrPort("10.0.0.7:802")
	path := writeTrace(t, "mixed.pcapng", tracetest.Options{Format: tracetest.PcapNG}, func(b *tracetest.Builder) {
		c := b.Conn(tracetest.ConnOptions{Server: odd})
		for i := range 4 {
			c.Request(uint16(i), 1, tracetest.ReadRequest(3, 0, 1))
			c.Response(uint16(i), 1, tracetest.RegistersResponse(3, 1))
		}
		h := b.Conn(tracetest.ConnOptions{Client: cl2, Server: web})
		h.Write(tracetest.ToServer, tracetest.WriteOpts{}, tracetest.Frame{Raw: []byte("GET / HTTP/1.1\r\n\r\n")})
		s := b.Conn(tracetest.ConnOptions{Client: netip.MustParseAddrPort("10.0.0.12:40100"), Server: tls})
		s.Write(tracetest.ToServer, tracetest.WriteOpts{}, tracetest.Frame{Raw: []byte{0x16, 3, 3, 0, 5, 1, 2, 3, 4, 5}})
	})
	ctx := context.Background()
	info, _ := e.Open(ctx, []string{path})
	ref, _ := e.Scan(ctx, info.ID)
	e.wait(ref.ID)
	res, err := e.ScanResult(ctx, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	classes := map[string]string{}
	for _, f := range res.Flows {
		classes[f.B.String()] = f.Class
	}
	if classes[odd.String()] != "modbus_tcp" || classes[web.String()] == "modbus_tcp" || classes[tls.String()] != "tls" {
		t.Errorf("classes %v", classes)
	}
	if len(res.Selected) != 1 || !slices.Contains(res.ServerPorts, 5021) || !slices.Contains(res.ServerPorts, 502) {
		t.Errorf("selection %v, ports %v", res.Selected, res.ServerPorts)
	}
	if _, err := e.Analyze(ctx, info.ID, service.AnalyzeConfig{Servers: []string{"not an endpoint"}}); err == nil {
		t.Error("a bad server override was accepted")
	}
}

// A3: a lost segment and a resync give gap and resync rows, flags on the
// transactions next to them, and a quality grade.
func TestQualityRows(t *testing.T) {
	e := newEngine(t)
	path := writeTrace(t, "lossy.pcap", tracetest.Options{}, func(b *tracetest.Builder) {
		c := b.Conn(tracetest.ConnOptions{Server: srv1, Midstream: true})
		c.Request(1, 1, tracetest.ReadRequest(3, 0, 1))
		c.Write(tracetest.ToClient, tracetest.WriteOpts{Lose: []int{0}}, tracetest.Frame{TID: 1, Unit: 1, PDU: tracetest.RegistersResponse(3, 1)})
		for i := 2; i < 6; i++ {
			c.Request(uint16(i), 1, tracetest.ReadRequest(3, 0, 1))
			c.Response(uint16(i), 1, tracetest.RegistersResponse(3, 1))
		}
		c.Write(tracetest.ToServer, tracetest.WriteOpts{}, tracetest.Frame{Raw: []byte{9, 9, 9}})
		c.Request(7, 1, tracetest.ReadRequest(3, 0, 1))
		c.Response(7, 1, tracetest.RegistersResponse(3, 1))
	})
	id, _ := importTrace(t, e, &service.AnalyzeConfig{ServerPorts: []uint16{502}}, path)
	p := page(t, e, id, service.Filter{})
	kinds := map[string]int{}
	for _, r := range p.Rows {
		kinds[r.Kind]++
	}
	if kinds["gap"] == 0 || kinds["resync"] == 0 || kinds["txn"] == 0 {
		t.Errorf("kinds %v", kinds)
	}
	flagged := page(t, e, id, service.Filter{Flags: []string{"midstream"}})
	if len(txns(flagged)) == 0 {
		t.Error("no midstream transactions")
	}
	ov, _ := e.Overview(context.Background(), id)
	if ov.Quality != "problems" || ov.QualityCount == 0 {
		t.Errorf("quality %s %d", ov.Quality, ov.QualityCount)
	}
	for _, r := range p.Rows {
		if r.Kind == "gap" && !strings.HasPrefix(r.Summary, "Gap: ") {
			t.Errorf("gap text %q", r.Summary)
		}
	}
}

// A5: the detail of a transaction decodes both ADUs, with fields whose
// byte ranges lie in the ADU, the packets that carried it, and the MBCAP
// classification.
func TestDetail(t *testing.T) {
	e := newEngine(t)
	path := writeTrace(t, "site.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	id, _ := importTrace(t, e, nil, path)
	rows := txns(page(t, e, id, service.Filter{}))
	d, err := e.Detail(context.Background(), id, rows[0].Seq)
	if err != nil {
		t.Fatal(err)
	}
	if d.Request == nil || d.Response == nil {
		t.Fatalf("%+v", d)
	}
	want := hex.EncodeToString(tracetest.MBAP(0, 1, tracetest.ReadRequest(3, 100, 2)))
	if d.Request.Hex != want {
		t.Errorf("request %s, want %s", d.Request.Hex, want)
	}
	for _, a := range []*service.ADUDetail{d.Request, d.Response} {
		if len(a.Packets) == 0 || a.Packets[0].Frame == 0 || !strings.HasPrefix(a.Packets[0].Display, "frame.number == ") {
			t.Errorf("packets %+v", a.Packets)
		}
		for _, f := range a.Fields {
			if f.Offset < 0 || f.Offset+f.Length > len(a.Hex)/2 {
				t.Errorf("field %+v outside the ADU", f)
			}
		}
	}
	if d.MBCAP != "Will be exported to MBCAP." || !strings.Contains(d.Text, "asked 10.0.0.2:502 unit 1") || d.Conn == nil {
		t.Errorf("mbcap %q, text %q", d.MBCAP, d.Text)
	}
	var write service.Row
	for _, r := range rows {
		if r.Class == "write" {
			write = r
		}
	}
	dw, _ := e.Detail(context.Background(), id, write.Seq)
	if !strings.Contains(dw.MBCAP, "writes are not part of replay data") {
		t.Errorf("write: %q", dw.MBCAP)
	}
	// A connection row: its own text and data.
	all := page(t, e, id, service.Filter{})
	dc, err := e.Detail(context.Background(), id, all.Rows[0].Seq)
	if err != nil || all.Rows[0].Kind != "conn_open" || dc.Extra == nil {
		t.Errorf("connection row %+v %v", dc, err)
	}
	if _, err := e.Detail(context.Background(), id, 99999); err == nil {
		t.Error("detail of a row that does not exist")
	}
}

// A6: each filter narrows consistently.
func TestFilters(t *testing.T) {
	e := newEngine(t)
	path := writeTrace(t, "site.pcap", tracetest.Options{}, site)
	id, _ := importTrace(t, e, nil, path)
	u2 := uint8(2)
	cases := map[string]struct {
		f    service.Filter
		want int // transactions
	}{
		"server ip":   {service.Filter{Servers: []string{"10.0.0.3"}}, 3},
		"server port": {service.Filter{Servers: []string{srv1.String()}}, 7},
		"unit":        {service.Filter{Units: []uint8{u2}}, 1},
		"client":      {service.Filter{Clients: []string{cl2.Addr().String()}}, 3},
		"function":    {service.Filter{Functions: []uint8{3}}, 5},
		"status":      {service.Filter{Statuses: []string{"exception"}}, 1},
		"exception":   {service.Filter{Exceptions: []uint8{2}}, 1},
		"class":       {service.Filter{Classes: []string{"write"}}, 1},
		"space":       {service.Filter{Space: "coil"}, 3},
		"address":     {service.Filter{Space: "holding", Start: ptr[uint16](101), End: ptr[uint16](101)}, 5},
		"any space":   {service.Filter{Start: ptr[uint16](5), End: ptr[uint16](5)}, 4}, // the write @ 5 and the coil reads 0-15
		"two terms":   {service.Filter{Servers: []string{"10.0.0.2"}, Functions: []uint8{3}}, 5},
		"nothing":     {service.Filter{Servers: []string{"10.9.9.9"}}, 0},
		"no flag":     {service.Filter{NotFlags: []string{"retransmitted"}}, 10},
		"connection":  {service.Filter{Conns: []uint32{2}}, 3},
	}
	for name, c := range cases {
		p := page(t, e, id, c.f)
		if got := len(txns(p)); got != c.want {
			t.Errorf("%s: %d transactions, want %d", name, got, c.want)
		}
		if uint64(len(p.Rows)) != p.Total {
			t.Errorf("%s: total %d for %d rows", name, p.Total, len(p.Rows))
		}
	}
	// A server filter keeps that server's connection rows.
	p := page(t, e, id, service.Filter{Servers: []string{"10.0.0.3"}})
	if p.Total != 3+2 {
		t.Errorf("server rows %d", p.Total)
	}
	for _, bad := range []service.Filter{{Statuses: []string{"fine"}}, {Classes: []string{"x"}}, {Flags: []string{"x"}},
		{Space: "attic"}, {Start: ptr[uint16](9), End: ptr[uint16](1)}} {
		if _, err := e.Timeline(context.Background(), id, service.TimelineQuery{Filter: bad, Limit: 10}); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	// Times.
	all := page(t, e, id, service.Filter{})
	from := all.Rows[5].TimeNS
	p = page(t, e, id, service.Filter{FromNS: &from})
	if p.Rows[0].TimeNS < from {
		t.Error("from")
	}
}

func ptr[T any](v T) *T { return &v }

// A11: an unchanged trace reopens from the cache; a changed one is new; a
// moved one keeps its overview and timeline but not its bytes; clearing
// works; permissions hold.
func TestCacheReuse(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	mk := func() *Engine {
		e, err := New(Options{CacheDir: cacheDir, ConfigDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = e.Shutdown() })
		return e
	}
	path := writeTrace(t, "site.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	id, _ := importTrace(t, mk(), nil, path)
	e := mk()
	info, err := e.Open(context.Background(), []string{path})
	if err != nil || !info.Analyzed || info.ID != id || info.Choices == nil {
		t.Fatalf("reopen %+v %v", info, err)
	}
	if p := page(t, e, id, service.Filter{}); len(txns(p)) != 10 {
		t.Error("cached timeline")
	}
	// Moved: the overview and the timeline stay, the bytes do not.
	moved := path + ".moved"
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	ov, _ := e.Overview(context.Background(), id)
	if !ov.TraceMissing {
		t.Error("a moved trace is not reported missing")
	}
	rows := txns(page(t, e, id, service.Filter{}))
	d, err := e.Detail(context.Background(), id, rows[0].Seq)
	if err != nil || d.Request == nil || d.Request.Hex != "" || len(d.Request.Packets) == 0 {
		t.Errorf("detail of a moved trace %+v %v", d.Request, err)
	}
	_ = os.Rename(moved, path)
	// Changed: another fingerprint, not analysed.
	b, _ := os.ReadFile(path)
	_ = os.WriteFile(path, append(b, 0), 0o600)
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(path, later, later)
	info2, _ := e.Open(context.Background(), []string{path})
	if info2.ID == id || info2.Analyzed {
		t.Errorf("a changed trace reused the cache: %+v", info2)
	}
	if fi, _ := os.Stat(cacheDir); fi.Mode().Perm() != 0o700 {
		t.Errorf("cache dir %v", fi.Mode().Perm())
	}
	recent, _ := e.Recent(context.Background())
	if len(recent) != 1 || recent[0].Paths[0] != path || !recent[0].Exists {
		t.Errorf("recent %+v", recent)
	}
	if err := e.Close(context.Background(), info2.ID); err != nil {
		t.Error(err)
	}
	if err := e.Close(context.Background(), "nope"); err == nil {
		t.Error("closed a trace that is not open")
	}
}

// R1/A16: a gzip trace is decompressed into the cache and analysed.
func TestGzip(t *testing.T) {
	e := newEngine(t)
	path := writeTrace(t, "site.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	raw, _ := os.ReadFile(path)
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	gz := filepath.Join(t.TempDir(), "site.pcapng.gz")
	_ = os.WriteFile(gz, z.Bytes(), 0o600)
	id, _ := importTrace(t, e, nil, gz)
	info, _ := e.trace(id)
	if !info.info.Gzip || info.data[0] == gz {
		t.Errorf("not decompressed: %+v", info.data)
	}
	rows := txns(page(t, e, id, service.Filter{}))
	d, _ := e.Detail(context.Background(), id, rows[0].Seq)
	if len(rows) != 10 || d.Request == nil || d.Request.Hex == "" {
		t.Errorf("%d rows, detail %+v", len(rows), d.Request)
	}

	// A16: the decompressed copy goes when the trace closes; reopened,
	// the analysis is reused and Prepare makes the copy again.
	ctx := context.Background()
	cp := info.data[0]
	if err := e.Close(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cp); !os.IsNotExist(err) {
		t.Errorf("the decompressed copy is still there: %v", err)
	}
	again, err := e.Open(ctx, []string{gz})
	if err != nil || !again.Analyzed || !again.Prepare {
		t.Fatalf("reopened %+v %v", again, err)
	}
	if d, err := e.Detail(ctx, id, rows[0].Seq); err != nil || d.Request == nil || d.Request.Hex != "" {
		t.Errorf("detail before Prepare: %v %+v", err, d.Request)
	}
	if _, err := e.ExportTimeline(ctx, id, service.TimelineExport{Format: "csv"}, filepath.Join(t.TempDir(), "t.csv")); !isCode(err, service.CodeUnavailable) {
		t.Errorf("export before Prepare: %v", err)
	}
	ref, err := e.Prepare(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	e.wait(ref.ID)
	if d, err := e.Detail(ctx, id, rows[0].Seq); err != nil || d.Request == nil || d.Request.Hex == "" {
		t.Errorf("detail after Prepare: %v", err)
	}
	ref, err = e.ExportTimeline(ctx, id, service.TimelineExport{Format: "csv"}, filepath.Join(t.TempDir(), "t.csv"))
	if res := runExport(t, e, ref, err); res.Rows == 0 {
		t.Errorf("export from a gzip trace: %+v", res)
	}
	if fi, _ := e.trace(id); fi.info.Prepare {
		t.Error("still preparing")
	}

	// Without the copy (cleared), the trace is imported again.
	_ = e.Close(ctx, id)
	if _, err := e.ClearCache(ctx); err != nil {
		t.Fatal(err)
	}
	if again, _ := e.Open(ctx, []string{gz}); again.Analyzed {
		t.Error("analysed without its decompressed copy")
	}
}

// A13: hostile files give a clear error or partial results, never a crash.
func TestHostileFiles(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	dir := t.TempDir()
	good := writeTrace(t, "good.pcap", tracetest.Options{}, site)
	raw, _ := os.ReadFile(good)
	files := map[string][]byte{
		"empty.pcap":   {},
		"garbage.pcap": []byte("PK\x03\x04 this is a zip file"),
		"cut.pcap":     raw[:len(raw)-7],
		"huge.pcap":    append(append([]byte{}, raw[:24]...), 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0x7f, 0xff, 0xff, 0xff, 0x7f),
	}
	for name, b := range files {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, b, 0o600)
		info, err := e.Open(ctx, []string{p})
		if err != nil {
			t.Fatalf("%s: open: %v", name, err)
		}
		ref, err := e.Scan(ctx, info.ID)
		if err != nil {
			t.Fatal(err)
		}
		events, _ := e.Subscribe(ctx)
		_ = events
		e.wait(ref.ID)
		res, err := e.ScanResult(ctx, info.ID)
		switch name {
		case "cut.pcap":
			if err != nil || len(res.Flows) == 0 {
				t.Errorf("%s: a cut trace must give its flows: %v", name, err)
			}
		case "empty.pcap", "garbage.pcap":
			if err == nil {
				t.Errorf("%s: scanned", name)
			}
		}
	}
	if _, err := e.Open(ctx, []string{filepath.Join(dir, "missing.pcap")}); err == nil {
		t.Error("opened a missing file")
	}
	if _, err := e.Open(ctx, nil); err == nil {
		t.Error("opened nothing")
	}
}

func tracetestOptions() tracetest.Options { return tracetest.Options{} }
