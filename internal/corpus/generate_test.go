// SPDX-License-Identifier: MIT

package corpus

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-modbus-capture/capture"
	"github.com/otfabric/go-modbus-capture/jsonl"
	"github.com/otfabric/go-modbus-capture/mbcap"
	"github.com/otfabric/go-modbus-capture/mbcsv"

	"github.com/otfabric/go-modbus-trace/export/mbcapexport"
	"github.com/otfabric/go-modbus-trace/trace"
	"github.com/otfabric/go-modbus-trace/trace/tracetest"
)

// Dir is the corpus, relative to this package.
const Dir = "../../testdata/corpus"

// Entry describes one corpus file (manifest.json).
type Entry struct {
	File     string `json:"file"`     // path below testdata/corpus
	Category string `json:"category"` // realistic, edge or broken
	About    string `json:"about"`
	Kind     string `json:"kind"` // trace or capture
	// Expect: "ok" (imports and shows transactions), "empty" (imports,
	// nothing to show), "error" (refused with a message).
	Expect          string            `json:"expect"`
	MinTransactions uint64            `json:"min_transactions,omitempty"`
	Error           string            `json:"error,omitempty"`   // part of the message
	Warning         string            `json:"warning,omitempty"` // part of the import's warning
	Quality         map[string]uint64 `json:"quality,omitempty"` // conditions that must be counted, at least this many
	Static          bool              `json:"static,omitempty"`  // copied from go-modbus-capture's test vectors, not generated
}

// file is a corpus file to write.
type file struct {
	Entry
	data []byte
}

var start = time.Date(2026, 10, 14, 6, 58, 0, 0, time.UTC)

// TestCorpus writes the corpus with MTE_UPDATE=1 (make golden) and
// otherwise checks that every generated file is what the generator makes.
func TestCorpus(t *testing.T) {
	files := generate(t)
	update := os.Getenv("MTE_UPDATE") != ""
	var manifest []Entry
	for _, f := range files {
		manifest = append(manifest, f.Entry)
		if f.Static {
			if _, err := os.Stat(filepath.Join(Dir, f.File)); err != nil {
				t.Errorf("%s: %v", f.File, err)
			}
			continue
		}
		path := filepath.Join(Dir, f.File)
		if update {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, f.data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, f.data) {
			t.Errorf("%s is not up to date: run make golden (%v)", f.File, err)
		}
	}
	sort.Slice(manifest, func(i, j int) bool { return manifest[i].File < manifest[j].File })
	b, _ := json.MarshalIndent(manifest, "", "  ")
	b = append(b, '\n')
	readme := []byte(readmeText(manifest))
	for name, want := range map[string][]byte{"manifest.json": b, "README.md": readme} {
		path := filepath.Join(Dir, name)
		if update {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is not up to date: run make golden (%v)", name, err)
		}
	}
	// Nothing in the corpus that the manifest does not describe.
	known := map[string]bool{"manifest.json": true, "README.md": true}
	for _, e := range manifest {
		known[e.File] = true
	}
	_ = filepath.WalkDir(Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(Dir, p)
		if !known[filepath.ToSlash(rel)] {
			t.Errorf("%s is not in the manifest", rel)
		}
		return nil
	})
}

func generate(t *testing.T) []file {
	t.Helper()
	var out []file
	add := func(e Entry, data []byte) { out = append(out, file{Entry: e, data: data}) }
	tr := func(o tracetest.Options, script func(b *tracetest.Builder)) []byte { return build(t, o, script) }

	// --- realistic -------------------------------------------------------
	plant := tr(tracetest.Options{Format: tracetest.PcapNG, Start: start, Step: 40 * time.Microsecond}, plantScript(15*time.Minute))
	add(Entry{File: "realistic/plant-scada.pcapng", Category: "realistic", Kind: "trace", Expect: "ok", MinTransactions: 2000,
		About:   "15 minutes of a small plant: SCADA polls a PV inverter, an energy meter and a battery gateway (units 1–3); an HMI writes setpoints; device identification at the start; the meter rejects one block (illegal data address); battery unit 3 stops answering for a minute and its gateway then reports it unreachable; the meter resets its connection once; a few retransmissions.",
		Quality: map[string]uint64{"unanswered": 5, "retransmitted": 1}}, plant)
	add(Entry{File: "realistic/plant-scada.pcapng.gz", Category: "realistic", Kind: "trace", Expect: "ok", MinTransactions: 2000,
		About: "The plant trace compressed with gzip (decompressed into the cache while open)."}, gz(plant, "plant-scada.pcapng"))
	for k, part := range ring(t, 3, 15*time.Minute) {
		add(Entry{File: fmt.Sprintf("realistic/plant-ring_%05d.pcapng", k+1), Category: "realistic", Kind: "trace", Expect: "ok", MinTransactions: 300,
			About: fmt.Sprintf("Part %d of 3 of the plant as a ring buffer of five-minute files (as dumpcap -b writes it): open the three together, in any order, as one trace.", k+1)}, part)
	}
	add(Entry{File: "realistic/rtu-gateway.pcap", Category: "realistic", Kind: "trace", Expect: "ok", MinTransactions: 400,
		About:   "Classic pcap: a Modbus/TCP-to-RTU gateway with eight units, polled round-robin for coils, discrete inputs and registers; units 6 and 8 are not connected (gateway exceptions 0x0A and 0x0B).",
		Quality: map[string]uint64{}}, tr(tracetest.Options{Format: tracetest.Pcap, Start: start}, gatewayScript))
	add(Entry{File: "realistic/function-tour.pcapng", Category: "realistic", Kind: "trace", Expect: "ok", MinTransactions: 20,
		About: "Every kind of function the decoder knows, answered and as exceptions: coils, discrete inputs, registers, writes, FC23, FC24 FIFO, FC20/21 file records, FC08 diagnostics, FC11/FC17, FC43/14 device identification over two pages, and a vendor function."},
		tr(tracetest.Options{Format: tracetest.PcapNG, Start: start}, tourScript))

	plantPath := filepath.Join(t.TempDir(), "plant.pcapng")
	if err := os.WriteFile(plantPath, plant, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, enc := range []string{"mbcap", "jsonl", "csv"} {
		name := "realistic/plant-scada.mbcap"
		if enc != "mbcap" {
			name += "." + enc
		}
		add(Entry{File: name, Category: "realistic", Kind: "capture", Expect: "ok", MinTransactions: 1500,
			About: "The plant's reads exported as an MBCAP capture (" + enc + "), as modbusctl server replay would serve them."},
			export(t, plantPath, enc, false))
	}
	add(Entry{File: "realistic/plant-scada-change-only.mbcap", Category: "realistic", Kind: "capture", Expect: "ok", MinTransactions: 100,
		About: "The plant exported change-only: repeated identical responses left out.", Quality: map[string]uint64{"change_only": 1}},
		export(t, plantPath, "mbcap", true))

	// --- edge: valid but hard --------------------------------------------
	simple := func(name, about string, o tracetest.Options, script func(b *tracetest.Builder), min uint64, q map[string]uint64) {
		if o.Start.IsZero() {
			o.Start = start
		}
		add(Entry{File: "edge/" + name, Category: "edge", Kind: "trace", Expect: "ok", MinTransactions: min, About: about, Quality: q}, tr(o, script))
	}
	simple("midstream.pcapng", "The capture starts while the connections are open: no handshakes, the server side is inferred from the traffic (guessed roles).",
		tracetest.Options{Format: tracetest.PcapNG}, midstreamScript, 40, nil)
	simple("nonstandard-port.pcapng", "Modbus/TCP on port 5020 next to SSH on 22: the scan proposes 5020.",
		tracetest.Options{Format: tracetest.PcapNG}, otherPortScript, 30, nil)
	simple("lossy-link.pcapng", "A bad link: lost segments (gaps and resyncs), retransmissions, a retransmission with different bytes, segments out of order, ADUs split byte by byte, keepalives.",
		tracetest.Options{Format: tracetest.PcapNG}, lossyScript, 20, map[string]uint64{"gaps": 1, "retransmitted": 1, "out_of_order": 1, "overlap_conflict": 1})
	simple("ip-fragments.pcap", "Responses that do not fit one packet arrive as IP fragments.",
		tracetest.Options{Format: tracetest.Pcap}, fragmentScript, 10, nil)
	simple("snaplen-64.pcap", "Captured with a snap length of 64 bytes (tcpdump -s 64): long ADUs are cut.",
		tracetest.Options{Format: tracetest.Pcap, SnapLen: 64}, cutScript, 5, map[string]uint64{"snaplen_truncated": 1})
	simple("tid-and-pairing.pcapng", "Pairing trouble: two requests with the same transaction ID outstanding, a response without a request, a request never answered, malformed PDUs (wrong byte counts, an exception code 0), a non-zero protocol ID, broadcast unit 0 and unit 255.",
		tracetest.Options{Format: tracetest.PcapNG}, pairingScript, 10, map[string]uint64{"unanswered": 1})
	simple("clock-backwards.pcapng", "The capture host's clock steps back by two seconds in the middle (time disorder).",
		tracetest.Options{Format: tracetest.PcapNG}, clockScript, 20, nil)
	simple("many-connections.pcapng", "300 short connections, one poll each (a client that reconnects for every read), from 12 clients to 25 devices: long lists in the Conversations and Devices views.",
		tracetest.Options{Format: tracetest.PcapNG, Step: 20 * time.Microsecond, CountOnly: true}, manyScript, 300, nil)
	simple("ipv6.pcapng", "Modbus/TCP over IPv6.",
		tracetest.Options{Format: tracetest.PcapNG}, ipv6Script, 10, nil)
	simple("tls-and-modbus.pcapng", "Modbus/TCP Security on port 802 (encrypted, not decodable) next to plain Modbus/TCP on 502.",
		tracetest.Options{Format: tracetest.PcapNG}, tlsScript, 10, nil)
	simple("two-interfaces-drops.pcapng", "Captured on two interfaces at once, some packets seen on both, and the capture host reporting dropped packets.",
		tracetest.Options{Format: tracetest.PcapNG, Interfaces: []tracetest.Iface{{Link: tracetest.Ethernet, Name: "eth0"}, {Link: tracetest.EthernetVLAN, Name: "eth1.10"}}},
		twoIfaceScript, 10, map[string]uint64{"capture_drops": 1})
	simple("noise-around-modbus.pcap", "Modbus/TCP between ARP, UDP and ICMP packets that are not TCP (counted as unsupported).",
		tracetest.Options{Format: tracetest.Pcap}, noiseScript, 10, map[string]uint64{"unsupported": 1})
	for _, v := range []struct {
		name, about string
		o           tracetest.Options
	}{
		{"link-vlan-qinq.pcapng", "Ethernet with two VLAN tags (802.1ad and 802.1Q).", tracetest.Options{Format: tracetest.PcapNG, Interfaces: []tracetest.Iface{{Link: tracetest.EthernetQinQ}}}},
		{"link-linux-cooked.pcap", "Linux cooked capture (tcpdump -i any).", tracetest.Options{Format: tracetest.Pcap, Interfaces: []tracetest.Iface{{Link: tracetest.SLL}}}},
		{"link-loopback.pcap", "BSD loopback (captured on lo0, a simulator on the same machine).", tracetest.Options{Format: tracetest.Pcap, Interfaces: []tracetest.Iface{{Link: tracetest.Null}}}},
		{"link-raw-ip.pcap", "Raw IP, no link layer.", tracetest.Options{Format: tracetest.Pcap, Interfaces: []tracetest.Iface{{Link: tracetest.Raw}}}},
		{"pcap-big-endian.pcap", "Classic pcap written big endian.", tracetest.Options{Format: tracetest.PcapBE}},
		{"pcap-nanoseconds.pcap", "Classic pcap with nanosecond timestamps.", tracetest.Options{Format: tracetest.PcapNano}},
		{"pcapng-big-endian.pcapng", "pcapng written big endian.", tracetest.Options{Format: tracetest.PcapNGBE}},
		{"pcapng-ns-resolution.pcapng", "pcapng with nanosecond timestamp resolution (if_tsresol 9).", tracetest.Options{Format: tracetest.PcapNG, Interfaces: []tracetest.Iface{{Link: tracetest.Ethernet, TSResol: 9}}}},
	} {
		simple(v.name, v.about, v.o, shortPoll, 10, nil)
	}
	simple("simple-packets.pcapng", "pcapng Simple Packet Blocks: packets without timestamps.",
		tracetest.Options{Format: tracetest.PcapNG}, simplePacketScript, 1, nil)

	// Captures with unusual content.
	capFile := func(name, about string, expect string, min uint64, q map[string]uint64, data []byte) {
		add(Entry{File: "edge/" + name, Category: "edge", Kind: "capture", Expect: expect, MinTransactions: min, About: about, Quality: q}, data)
	}
	capFile("serial-and-tls-targets.mbcap", "A capture of a serial RTU device and a TLS device (modbusctl reading them), with labels in several scripts and a very long one.", "ok", 6, nil, oddTargets(t, "mbcap"))
	capFile("serial-and-tls-targets.mbcap.jsonl", "The same, as JSON Lines.", "ok", 6, nil, oddTargets(t, "jsonl"))
	capFile("no-samples.mbcap", "A capture that holds targets and streams but no sample (a read that got nothing).", "empty", 0, nil, emptyCapture(t))
	capFile("no-end.mbcap.jsonl", "A capture never closed (the recorder was killed): no end record.", "ok", 3, map[string]uint64{"end_unknown": 1}, noEnd(t))
	capFile("time-disorder.mbcap", "Samples out of time order, as a merge of two recorders can leave them.", "ok", 6, nil, disorder(t))

	// --- broken ------------------------------------------------------------
	bad := func(name, kind, about, msg string, data []byte) {
		add(Entry{File: "broken/" + name, Category: "broken", Kind: kind, Expect: "error", Error: msg, About: about}, data)
	}
	partial := func(name, about string, min uint64, warning string, data []byte) {
		add(Entry{File: "broken/" + name, Category: "broken", Kind: "trace", Expect: "ok", MinTransactions: min, Warning: warning, About: about}, data)
	}
	short := tr(tracetest.Options{Format: tracetest.Pcap, Start: start}, shortPoll)
	shortNG := tr(tracetest.Options{Format: tracetest.PcapNG, Start: start}, shortPoll)
	bad("empty.pcap", "trace", "A file of zero bytes.", "pcap", nil)
	bad("garbage.pcapng", "trace", "Random bytes with a trace's name.", "pcap", noise(4096, 7))
	bad("text-file.pcap", "trace", "A text file renamed .pcap.", "pcap", []byte("This is not a packet capture.\nIt is a note someone saved with the wrong name.\n"))
	add(Entry{File: "broken/unknown-link-type.pcap", Category: "broken", Kind: "trace", Expect: "empty",
		About: "A pcap whose header names a link type no reader knows (65000): no packet can be decoded, so no Modbus flow is found and the link type is counted as unsupported."}, withLinkType(short, 65000))
	add(Entry{File: "broken/header-only.pcap", Category: "broken", Kind: "trace", Expect: "empty", About: "A pcap header and no packet: a trace with nothing in it."}, short[:24])
	partial("cut-mid-packet.pcap", "A pcap cut in the middle of a packet (the capture was copied while it was written).", 3, "middle of a packet", short[:len(short)*2/3])
	partial("cut-mid-block.pcapng", "A pcapng cut in the middle of a block.", 3, "middle of a packet", shortNG[:len(shortNG)*2/3+3])
	partial("huge-record-length.pcap", "A packet record that claims 2 GiB: the reader stops there and keeps what came before.", 3, "damaged", hugeRecord(short))
	partial("gzip-cut.pcapng.gz", "A gzip trace cut before its end: what decompresses is read.", 1, "gzip", gz(shortNG, "cut.pcapng")[:len(gz(shortNG, "cut.pcapng"))*2/3])
	bad("gzip-garbage.pcap.gz", "trace", "A gzip header followed by garbage.", "", append([]byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 3}, noise(2048, 9)...))
	bad("pcap-named.mbcap.csv", "capture", "A packet trace named like a CSV capture: the name says capture, the content does not.", "capture", short)
	bad("empty.mbcap", "capture", "A capture of zero bytes.", "capture", nil)
	bad("garbage.mbcap.jsonl", "capture", "JSON Lines that are not a capture.", "capture", []byte("{\"hello\":\"world\"}\nnot json at all\n"))
	bad("wrong-columns.mbcap.csv", "capture", "A CSV whose columns are not those of a capture.", "capture", []byte("time,value\n1,2\n3,4\n"))
	plantCSV := export(t, plantPath, "csv", false)
	partial2 := func(name, about string, min uint64, data []byte) {
		add(Entry{File: "broken/" + name, Category: "broken", Kind: "capture", Expect: "ok", MinTransactions: min, Warning: "capture", About: about}, data)
	}
	plantJSONL := export(t, plantPath, "jsonl", false)
	partial2("cut.mbcap.jsonl", "A JSON Lines capture cut in the middle of a line: the samples before it are shown.", 100, cutAt(plantJSONL, len(plantJSONL)/2))
	add(Entry{File: "broken/bad-line.mbcap.jsonl", Category: "broken", Kind: "capture", Expect: "ok", MinTransactions: 100, Quality: map[string]uint64{"damaged": 1}, Warning: "damaged",
		About: "A JSON Lines capture with a broken line in the middle: the samples before it are shown, and Quality says where it broke."}, breakLine(plantJSONL))
	partial2("cut.mbcap.csv", "A CSV capture cut in the middle of a row.", 100, cutAt(plantCSV, len(plantCSV)/2))
	plantBin := export(t, plantPath, "mbcap", false)
	partial2("cut.mbcap", "A binary capture cut in the middle of a data block.", 1, plantBin[:len(plantBin)/2])
	add(Entry{File: "broken/flipped-byte.mbcap", Category: "broken", Kind: "capture", Expect: "ok", MinTransactions: 1, Quality: map[string]uint64{"damaged": 1}, Warning: "damaged",
		About: "A binary capture with one byte changed a third of the way in (that block's CRC no longer matches): the samples before it are shown, and Quality says it is damaged."}, flip(plantBin, len(plantBin)/3))
	for _, v := range []struct{ name, about, expect, msg string }{
		{"library-unknown-block.mbcap", "From go-modbus-capture's test vectors: a block type the reader does not know (skipped).", "ok", ""},
		{"library-no-index.mbcap", "From go-modbus-capture's test vectors: a capture without its index block.", "ok", ""},
		{"library-crc-corrupt.mbcap", "From go-modbus-capture's test vectors: a block whose CRC is wrong.", "error", "damaged"},
		{"library-truncated.mbcap", "From go-modbus-capture's test vectors: cut before its end.", "ok", ""},
	} {
		out = append(out, file{Entry: Entry{File: "broken/" + v.name, Category: "broken", Kind: "capture", Expect: v.expect, Error: v.msg, About: v.about, Static: true}})
	}
	return out
}

// build runs a script into a trace's bytes.
func build(t *testing.T, o tracetest.Options, script func(b *tracetest.Builder)) []byte {
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
	return buf.Bytes()
}

// gz compresses deterministically (no time in the header).
func gz(data []byte, name string) []byte {
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	w.Name = name
	w.ModTime = time.Time{}
	_, _ = w.Write(data)
	_ = w.Close()
	return buf.Bytes()
}

func noise(n int, seed uint64) []byte {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

func withLinkType(pcap []byte, lt uint32) []byte {
	b := bytes.Clone(pcap)
	binary.LittleEndian.PutUint32(b[20:], lt)
	return b
}

// hugeRecord changes the 30th packet record's lengths to 2 GiB.
func hugeRecord(pcap []byte) []byte {
	b := bytes.Clone(pcap)
	off := 24
	for range 29 {
		incl := binary.LittleEndian.Uint32(b[off+8:])
		off += 16 + int(incl)
	}
	binary.LittleEndian.PutUint32(b[off+8:], 1<<31)
	binary.LittleEndian.PutUint32(b[off+12:], 1<<31)
	return b
}

func cutAt(b []byte, n int) []byte { return bytes.Clone(b[:n]) }

func flip(b []byte, at int) []byte {
	c := bytes.Clone(b)
	c[at] ^= 0x5a
	return c
}

// breakLine replaces a line in the middle of JSON Lines by a broken one.
func breakLine(b []byte) []byte {
	lines := bytes.Split(b, []byte("\n"))
	mid := len(lines) / 2
	lines[mid] = []byte(`{"sample": [1, 2, oops`)
	return bytes.Join(lines, []byte("\n"))
}

// export writes a trace's reads as an MBCAP capture through the library.
func export(t *testing.T, path, enc string, changeOnly bool) []byte {
	t.Helper()
	f, err := trace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	open := func(h capture.Header) (capture.Writer, error) {
		switch enc {
		case "jsonl":
			return jsonl.NewWriter(&buf, h)
		case "csv":
			return mbcsv.NewWriter(&buf, h)
		}
		return mbcap.NewWriter(&buf, h, &mbcap.Options{BlockSamples: 256, Compression: mbcap.Deflate})
	}
	eo := mbcapexport.Options{Producer: "modbus-trace-explorer corpus", Step: "corpus export", ChangeOnly: changeOnly,
		Names: map[string]string{"10.20.1.11:502/1": "PV inverter", "10.20.1.21:502/1": "Energy meter",
			"10.20.1.30:502/1": "Battery rack 1", "10.20.1.30:502/2": "Battery rack 2", "10.20.1.30:502/3": "Battery rack 3"}}
	o := trace.Options{}
	o.ServerPorts = []uint16{502}
	if _, err := mbcapexport.Export(context.Background(), f, o, eo, open); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// capWriter makes a capture with the capture library's writers.
func capWriter(t *testing.T, enc string, h capture.Header, write func(w capture.Writer) error) []byte {
	t.Helper()
	var buf bytes.Buffer
	var w capture.Writer
	var err error
	switch enc {
	case "jsonl":
		w, err = jsonl.NewWriter(&buf, h)
	case "csv":
		w, err = mbcsv.NewWriter(&buf, h)
	default:
		w, err = mbcap.NewWriter(&buf, h, &mbcap.Options{BlockSamples: 64, Compression: mbcap.Deflate})
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := write(w); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func header() capture.Header {
	return capture.Header{TickNS: 1000, WallStart: start, Exceptions: true, Producer: "modbus-trace-explorer corpus",
		Tags: map[string]string{capture.TagProvenance: "corpus generator", capture.TagSource: "synthetic"}}
}

func oddTargets(t *testing.T, enc string) []byte {
	return capWriter(t, enc, header(), func(w capture.Writer) error {
		for _, tg := range []capture.Target{
			{ID: 1, Label: "Wärmepumpe Keller — ヒートポンプ — مضخة حرارية", Transport: capture.TransportRTU, Unit: 7,
				Serial: capture.Serial{Device: "/dev/ttyUSB0", Baud: 19200, Parity: capture.ParityEven, DataBits: 8, StopBits: 1}},
			{ID: 2, Label: strings.Repeat("A very long target label that does not fit anywhere ", 6), Transport: capture.TransportTLS, Host: "plc.example.internal", Port: 802, Unit: 1},
		} {
			if err := w.WriteTarget(tg); err != nil {
				return err
			}
		}
		for _, st := range []capture.Stream{{ID: 1, Target: 1, Role: capture.RolePoll, Label: "poll 1 s"}, {ID: 2, Target: 2, Role: capture.RoleSnapshot, Label: "snapshot"}} {
			if err := w.WriteStream(st); err != nil {
				return err
			}
		}
		for k := range 6 {
			s := capture.Sample{Stream: uint32(k%2 + 1), At: capture.Ticks(1_000_000 * k), Request: []byte{3, 0, 10, 0, 2},
				Response: []byte{3, 4, 0, byte(20 + k), 1, 0}}
			if err := w.WriteSample(s); err != nil {
				return err
			}
		}
		return w.Close(capture.End{Known: true, At: 6_000_000})
	})
}

func emptyCapture(t *testing.T) []byte {
	return capWriter(t, "mbcap", header(), func(w capture.Writer) error {
		if err := w.WriteTarget(capture.Target{ID: 1, Transport: capture.TransportTCP, Host: "127.0.0.1", Port: 502, Unit: 1}); err != nil {
			return err
		}
		if err := w.WriteStream(capture.Stream{ID: 1, Target: 1, Role: capture.RoleSnapshot, Label: "read FC3 100 x3"}); err != nil {
			return err
		}
		return w.Close(capture.End{Known: true, At: 585})
	})
}

func noEnd(t *testing.T) []byte {
	b := capWriter(t, "jsonl", header(), func(w capture.Writer) error {
		if err := w.WriteTarget(capture.Target{ID: 1, Transport: capture.TransportTCP, Host: "10.20.1.11", Port: 502, Unit: 1}); err != nil {
			return err
		}
		if err := w.WriteStream(capture.Stream{ID: 1, Target: 1, Role: capture.RolePoll, Label: "client 10.20.0.10"}); err != nil {
			return err
		}
		for k := range 3 {
			if err := w.WriteSample(capture.Sample{Stream: 1, At: capture.Ticks(1_000_000 * k), Request: []byte{3, 0, 0, 0, 1}, Response: []byte{3, 2, 0, byte(k)}}); err != nil {
				return err
			}
		}
		return w.Close(capture.End{})
	})
	return b
}

func disorder(t *testing.T) []byte {
	return capWriter(t, "mbcap", header(), func(w capture.Writer) error {
		if err := w.WriteTarget(capture.Target{ID: 1, Transport: capture.TransportTCP, Host: "10.20.1.21", Port: 502, Unit: 1}); err != nil {
			return err
		}
		for _, st := range []capture.Stream{{ID: 1, Target: 1, Role: capture.RolePoll, Label: "client 10.20.0.10"}, {ID: 2, Target: 1, Role: capture.RolePoll, Label: "client 10.20.0.11"}} {
			if err := w.WriteStream(st); err != nil {
				return err
			}
		}
		for _, at := range []int{5, 1, 4, 2, 6, 3} {
			if err := w.WriteSample(capture.Sample{Stream: uint32(at%2 + 1), At: capture.Ticks(at * 2_000_000), Request: []byte{4, 0, 0, 0, 1}, Response: []byte{4, 2, 0, byte(at)}}); err != nil {
				return err
			}
		}
		return w.Close(capture.End{Known: true, At: 13_000_000})
	})
}

// ---------------------------------------------------------------------------
// PDUs

func u16(v ...uint16) []byte {
	b := make([]byte, 0, 2*len(v))
	for _, x := range v {
		b = binary.BigEndian.AppendUint16(b, x)
	}
	return b
}

func readReq(fc uint8, addr, qty uint16) []byte { return tracetest.ReadRequest(fc, addr, qty) }

func regs(fc uint8, v ...uint16) []byte { return tracetest.RegistersResponse(fc, v...) }

func bits(fc uint8, n int, r *rand.Rand) []byte {
	nb := (n + 7) / 8
	b := []byte{fc, byte(nb)}
	for range nb {
		b = append(b, byte(r.Uint32()))
	}
	return b
}

func deviceID(vendor, product, version string) []byte {
	b := []byte{0x2B, 0x0E, 0x01, 0x01, 0x00, 0x00, 3}
	for k, s := range []string{vendor, product, version} {
		b = append(b, byte(k), byte(len(s)))
		b = append(b, s...)
	}
	return b
}

// ---------------------------------------------------------------------------
// Scripts

// poller is one client's connection to one server, with its own TIDs.
type poller struct {
	b   *tracetest.Builder
	c   *tracetest.Conn
	tid uint16
	r   *rand.Rand
}

// txn writes a request, the device's think time, and its response (nil:
// no response). retransmit sends the response twice.
func (p *poller) txn(unit uint8, req, resp []byte, retransmit bool) {
	p.tid++
	p.c.Request(p.tid, unit, req)
	p.b.Advance(time.Duration(3+p.r.IntN(35)) * time.Millisecond)
	if resp == nil {
		return
	}
	o := tracetest.WriteOpts{}
	if retransmit {
		o.Retransmit = []int{0}
	}
	p.c.Write(tracetest.ToClient, o, tracetest.Frame{TID: p.tid, Unit: unit, PDU: resp})
}

func ep(s string) netip.AddrPort { return netip.MustParseAddrPort(s) }

// plantScript: the plant of plant-scada.pcapng for d.
func plantScript(d time.Duration) func(b *tracetest.Builder) {
	return func(b *tracetest.Builder) { plant(b, d, nil) }
}

// plant writes the plant; next, when set, moves its connections into
// another builder every time it returns one (the ring buffer).
func plant(b *tracetest.Builder, d time.Duration, rotate func(at time.Duration) *tracetest.Builder) {
	r := rand.New(rand.NewPCG(14, 10))
	scada := "10.20.0.10"
	inv := &poller{b: b, r: r, c: b.Conn(tracetest.ConnOptions{Client: ep(scada + ":49152"), Server: ep("10.20.1.11:502")})}
	meter := &poller{b: b, r: r, c: b.Conn(tracetest.ConnOptions{Client: ep(scada + ":49153"), Server: ep("10.20.1.21:502")})}
	batt := &poller{b: b, r: r, c: b.Conn(tracetest.ConnOptions{Client: ep(scada + ":49154"), Server: ep("10.20.1.30:502")})}
	inv.txn(1, []byte{0x2B, 0x0E, 0x01, 0x00}, deviceID("OT Fabric Labs", "PV-5000", "2.4.1"), false)
	meter.txn(1, []byte{0x2B, 0x0E, 0x01, 0x00}, deviceID("OT Fabric Labs", "EM-340", "1.12"), false)
	for u := uint8(1); u <= 3; u++ {
		batt.txn(u, []byte{0x2B, 0x0E, 0x01, 0x00}, deviceID("OT Fabric Labs", fmt.Sprintf("BR-48 rack %d", u), "3.0.7"), false)
	}
	energy := uint32(18_402_311) // Wh
	soc := [4]float64{0, 81.5, 64.2, 92.0}
	setpoint := uint16(2500)
	hmiPort := 50000
	ticks := int(d / time.Second)
	for s := range ticks {
		at := time.Duration(s) * time.Second
		if rotate != nil {
			if nb := rotate(at); nb != nil {
				b = nb
				inv.b, meter.b, batt.b = nb, nb, nb
				inv.c, meter.c, batt.c = nb.ConnContinue(inv.c), nb.ConnContinue(meter.c), nb.ConnContinue(batt.c)
			}
		}
		sun := math.Max(0, math.Sin(math.Pi*(float64(s)/float64(ticks)*0.25+0.1)))
		power := uint16(sun*4800 + float64(r.IntN(60)))
		energy += uint32(power) / 3600
		// Inverter: status block every second, energy every 5 s.
		inv.txn(1, readReq(3, 100, 10), regs(3, power, 2300+uint16(r.IntN(40)), uint16(float64(power)/23.0), 5000+uint16(r.IntN(6)),
			uint16(350+sun*200+float64(r.IntN(8))), 3, 0, 0, setpoint, 1), s%211 == 17)
		if s%5 == 0 {
			inv.txn(1, readReq(4, 0, 4), regs(4, uint16(energy>>16), uint16(energy), 0, uint16(s/60)), false)
		}
		// Meter: every 2 s; a block it does not have every minute; it
		// resets its connection at minute 10.
		if s%2 == 0 {
			v := make([]uint16, 20)
			for k := range v {
				v[k] = uint16(2290 + r.IntN(30) + k)
			}
			meter.txn(1, readReq(3, 0x2000, 20), regs(3, v...), false)
		}
		if s%60 == 30 {
			meter.txn(1, readReq(3, 0x3000, 10), tracetest.Exception(3, 2), false)
		}
		if s == 600 {
			meter.c.Reset(tracetest.ToClient)
			b.Advance(2 * time.Second)
			meter = &poller{b: b, r: r, c: b.Conn(tracetest.ConnOptions{Client: ep(scada + ":49160"), Server: ep("10.20.1.21:502")})}
		}
		// Battery racks every 5 s; rack 3 silent from 7:00 to 8:00, then
		// reported unreachable by the gateway until 8:30.
		if s%5 == 0 {
			for u := uint8(1); u <= 3; u++ {
				soc[u] = math.Min(100, math.Max(5, soc[u]+(r.Float64()-0.45)*0.3))
				resp := regs(3, uint16(soc[u]*10), 520+uint16(r.IntN(6)), 2400+uint16(r.IntN(30)), uint16(r.IntN(3)), setpoint, 0, 0, 1)
				switch {
				case u == 3 && s >= 420 && s < 480:
					resp = nil
				case u == 3 && s >= 480 && s < 510:
					resp = tracetest.Exception(3, 0x0B)
				}
				batt.txn(u, readReq(3, 500, 8), resp, false)
			}
		}
		// The HMI writes a new setpoint every 4 minutes and reads it back.
		if s%240 == 120 {
			hmiPort++
			hmi := &poller{b: b, r: r, c: b.Conn(tracetest.ConnOptions{Client: ep(fmt.Sprintf("10.20.0.20:%d", hmiPort)), Server: ep("10.20.1.30:502")})}
			setpoint = 2000 + uint16(r.IntN(8))*100
			hmi.txn(1, []byte{6, 0x01, 0xF8, byte(setpoint >> 8), byte(setpoint)}, []byte{6, 0x01, 0xF8, byte(setpoint >> 8), byte(setpoint)}, false)
			hmi.txn(1, append([]byte{16, 0x01, 0xF9, 0, 2, 4}, u16(1, 0)...), []byte{16, 0x01, 0xF9, 0, 2}, false)
			hmi.txn(1, readReq(3, 504, 2), regs(3, setpoint, 1), false)
			hmi.c.Close()
		}
		b.Advance(time.Second - time.Duration(30+r.IntN(20))*time.Millisecond)
	}
	inv.c.Close()
	meter.c.Close()
	batt.c.Close()
}

// ring splits the plant into n files of d/n each.
func ring(t *testing.T, n int, d time.Duration) [][]byte {
	t.Helper()
	bufs := make([]*bytes.Buffer, n)
	builders := make([]*tracetest.Builder, n)
	for k := range n {
		bufs[k] = &bytes.Buffer{}
		b, err := tracetest.New(bufs[k], tracetest.Options{Format: tracetest.PcapNG, Start: start, Step: 40 * time.Microsecond})
		if err != nil {
			t.Fatal(err)
		}
		builders[k] = b
	}
	part := d / time.Duration(n)
	next := 1
	plant(builders[0], d, func(at time.Duration) *tracetest.Builder {
		if next < n && at >= time.Duration(next)*part {
			next++
			return builders[next-1]
		}
		return nil
	})
	out := make([][]byte, n)
	for k, b := range builders {
		if err := b.Close(); err != nil {
			t.Fatal(err)
		}
		out[k] = bufs[k].Bytes()
	}
	return out
}

func gatewayScript(b *tracetest.Builder) {
	r := rand.New(rand.NewPCG(8, 8))
	p := &poller{b: b, r: r, c: b.Conn(tracetest.ConnOptions{Client: ep("192.168.10.2:51000"), Server: ep("192.168.10.5:502")})}
	for round := range 60 {
		for u := uint8(1); u <= 8; u++ {
			switch u {
			case 6:
				p.txn(u, readReq(3, 0, 4), tracetest.Exception(3, 0x0A), false)
				continue
			case 8:
				p.txn(u, readReq(3, 0, 4), tracetest.Exception(3, 0x0B), false)
				continue
			}
			p.txn(u, readReq(1, 0, 16), bits(1, 16, r), false)
			p.txn(u, readReq(2, 0, 8), bits(2, 8, r), false)
			p.txn(u, readReq(3, 0, 4), regs(3, uint16(round), uint16(200+r.IntN(10)), uint16(u), 1), false)
		}
		b.Advance(500 * time.Millisecond)
	}
	p.c.Close()
}

func tourScript(b *tracetest.Builder) {
	r := rand.New(rand.NewPCG(1, 2))
	p := &poller{b: b, r: r, c: b.Conn(tracetest.ConnOptions{Client: ep("10.0.0.10:40000"), Server: ep("10.0.0.2:502")})}
	for _, x := range [][2][]byte{
		{readReq(1, 0, 10), bits(1, 10, r)},
		{readReq(2, 100, 5), bits(2, 5, r)},
		{readReq(3, 40000, 3), regs(3, 1, 2, 3)},
		{readReq(4, 0, 2), regs(4, 0xFFFF, 0)},
		{{5, 0, 3, 0xFF, 0}, {5, 0, 3, 0xFF, 0}},
		{{6, 0, 7, 0x12, 0x34}, {6, 0, 7, 0x12, 0x34}},
		{{15, 0, 0, 0, 10, 2, 0xCD, 0x01}, {15, 0, 0, 0, 10}},
		{append([]byte{16, 0, 20, 0, 3, 6}, u16(7, 8, 9)...), {16, 0, 20, 0, 3}},
		{append([]byte{23, 0, 0, 0, 2, 0, 50, 0, 1, 2}, u16(42)...), regs(23, 10, 11)},
		{{24, 0x04, 0xDE}, append([]byte{24, 0, 6, 0, 2}, u16(1, 2)...)},
		{{20, 7, 6, 0, 4, 0, 1, 0, 2}, append([]byte{20, 6, 5, 6}, u16(0x0D0E, 0x0F10)...)},
		{append([]byte{21, 9, 6, 0, 4, 0, 7, 0, 1}, u16(0x0A0B)...), append([]byte{21, 9, 6, 0, 4, 0, 7, 0, 1}, u16(0x0A0B)...)},
		{{8, 0, 0, 0xA5, 0x37}, {8, 0, 0, 0xA5, 0x37}},
		{{8, 0, 1, 0, 0}, {8, 0, 1, 0, 0}},
		{{11}, {11, 0, 0, 0, 42}},
		{{17}, {17, 6, 'O', 'T', 'F', 'B', 0xFF, 1}},
		{{0x2B, 0x0E, 0x01, 0x00}, {0x2B, 0x0E, 0x01, 0x01, 0xFF, 0x02, 2, 0, 4, 'A', 'c', 'm', 'e', 1, 3, 'X', '-', '1'}},
		{{0x2B, 0x0E, 0x01, 0x02}, deviceID("Acme", "X-1", "9.9")},
		{{0x41, 1, 2, 3}, {0x41, 4, 5}},
		{readReq(3, 9999, 2), tracetest.Exception(3, 2)},
		{{6, 0, 1, 0, 0}, tracetest.Exception(6, 3)},
		{{0x41}, tracetest.Exception(0x41, 1)},
		{readReq(4, 0, 1), tracetest.Exception(4, 4)},
		{readReq(3, 0, 1), tracetest.Exception(3, 6)},
	} {
		p.txn(1, x[0], x[1], false)
		b.Advance(200 * time.Millisecond)
	}
	p.c.Close()
}

// shortPoll: a few seconds of one client polling one device.
func shortPoll(b *tracetest.Builder) {
	r := rand.New(rand.NewPCG(3, 3))
	p := &poller{b: b, r: r, c: b.Conn(tracetest.ConnOptions{Client: ep("10.0.0.10:40000"), Server: ep("10.0.0.2:502")})}
	for k := range 12 {
		p.txn(1, readReq(3, 0, 10), regs(3, uint16(k), 1, 2, 3, 4, 5, 6, 7, 8, 9), false)
		b.Advance(time.Second)
	}
	p.c.Close()
}

func midstreamScript(b *tracetest.Builder) {
	r := rand.New(rand.NewPCG(4, 4))
	for k, srv := range []string{"10.0.5.2:502", "10.0.5.3:502"} {
		p := &poller{b: b, r: r, c: b.Conn(tracetest.ConnOptions{Client: ep(fmt.Sprintf("10.0.5.10:%d", 41000+k)), Server: ep(srv), Midstream: true})}
		for i := range 25 {
			p.txn(1, readReq(3, uint16(10*k), 2), regs(3, uint16(i), 0), false)
			b.Advance(400 * time.Millisecond)
		}
	}
}

func otherPortScript(b *tracetest.Builder) {
	r := rand.New(rand.NewPCG(5, 5))
	ssh := b.Conn(tracetest.ConnOptions{Client: ep("10.0.0.10:52000"), Server: ep("10.0.0.2:22")})
	ssh.Write(tracetest.ToClient, tracetest.WriteOpts{}, tracetest.Frame{Raw: []byte("SSH-2.0-OpenSSH_9.8\r\n")})
	ssh.Write(tracetest.ToServer, tracetest.WriteOpts{}, tracetest.Frame{Raw: noise(300, 1)})
	p := &poller{b: b, r: r, c: b.Conn(tracetest.ConnOptions{Client: ep("10.0.0.10:52001"), Server: ep("10.0.0.2:5020")})}
	for i := range 30 {
		p.txn(1, readReq(4, 0, 3), regs(4, uint16(i), 7, 9), false)
		b.Advance(time.Second)
	}
	ssh.Close()
	p.c.Close()
}

func lossyScript(b *tracetest.Builder) {
	p := &poller{b: b, r: rand.New(rand.NewPCG(6, 6)), c: b.Conn(tracetest.ConnOptions{Client: ep("10.0.0.10:40000"), Server: ep("10.0.0.2:502")})}
	f := func(tid uint16, pdu []byte) tracetest.Frame { return tracetest.Frame{TID: tid, Unit: 1, PDU: pdu} }
	resp := regs(3, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20)
	for i := range 30 {
		tid := uint16(i + 1)
		p.c.Request(tid, 1, readReq(3, 0, 20))
		b.Advance(10 * time.Millisecond)
		o := tracetest.WriteOpts{}
		switch i % 6 {
		case 1:
			o.Seg, o.Retransmit = []int{10, 10}, []int{1}
		case 2:
			o.Seg, o.Reverse = []int{10, 10, 10}, true
		case 3:
			o.OneByte = true
		case 4:
			o.Seg, o.Conflict = []int{20}, []int{0}
		case 5:
			if i == 11 {
				o.Seg, o.Lose = []int{10, 10}, []int{0}
			}
		}
		p.c.Write(tracetest.ToClient, o, f(tid, resp))
		if i%10 == 0 {
			p.c.Keepalive(tracetest.ToServer)
		}
		b.Advance(500 * time.Millisecond)
	}
	p.c.Close()
}

func fragmentScript(b *tracetest.Builder) {
	p := &poller{b: b, r: rand.New(rand.NewPCG(7, 7)), c: b.Conn(tracetest.ConnOptions{Client: ep("10.0.0.10:40000"), Server: ep("10.0.0.2:502")})}
	big := make([]uint16, 125)
	for k := range big {
		big[k] = uint16(k * 3)
	}
	for i := range 10 {
		p.tid++
		p.c.Request(p.tid, 1, readReq(3, 0, 125))
		b.Advance(15 * time.Millisecond)
		p.c.Write(tracetest.ToClient, tracetest.WriteOpts{Fragment: 64 + 8*(i%4)}, tracetest.Frame{TID: p.tid, Unit: 1, PDU: regs(3, big...)})
		b.Advance(time.Second)
	}
	p.c.Close()
}

func cutScript(b *tracetest.Builder) {
	p := &poller{b: b, r: rand.New(rand.NewPCG(8, 1)), c: b.Conn(tracetest.ConnOptions{Client: ep("10.0.0.10:40000"), Server: ep("10.0.0.2:502")})}
	for i := range 8 {
		n := uint16(2 + 20*(i%3))
		v := make([]uint16, n)
		p.txn(1, readReq(3, 0, n), regs(3, v...), false)
		b.Advance(time.Second)
	}
	p.c.Close()
}

func pairingScript(b *tracetest.Builder) {
	c := b.Conn(tracetest.ConnOptions{Client: ep("10.0.0.10:40000"), Server: ep("10.0.0.2:502")})
	req := func(tid uint16, unit uint8, pdu []byte) { c.Request(tid, unit, pdu); b.Advance(5 * time.Millisecond) }
	resp := func(tid uint16, unit uint8, pdu []byte) { c.Response(tid, unit, pdu); b.Advance(5 * time.Millisecond) }
	req(1, 1, readReq(3, 0, 1))
	req(1, 1, readReq(3, 10, 1)) // same TID while the first is outstanding
	resp(1, 1, regs(3, 5))
	resp(1, 1, regs(3, 6))
	resp(9, 1, regs(3, 7))        // no request
	req(10, 1, readReq(3, 20, 1)) // never answered
	req(11, 1, readReq(3, 0, 2))  // a byte count that does not fit
	resp(11, 1, []byte{3, 9, 0, 1, 0, 2})
	req(12, 1, readReq(3, 0, 1))
	resp(12, 1, []byte{0x83, 0})      // exception code 0
	req(13, 0, []byte{6, 0, 1, 0, 1}) // broadcast write
	req(14, 255, readReq(4, 0, 1))
	resp(14, 255, regs(4, 1))
	c.Write(tracetest.ToServer, tracetest.WriteOpts{}, tracetest.Frame{Raw: []byte{0, 15, 0, 7, 0, 6, 1, 3, 0, 0, 0, 1}}) // protocol ID 7
	b.Advance(5 * time.Millisecond)
	for k := uint16(16); k < 26; k++ {
		req(k, 1, readReq(3, 0, 1))
		resp(k, 1, regs(3, k))
	}
	c.Close()
}

func clockScript(b *tracetest.Builder) {
	p := &poller{b: b, r: rand.New(rand.NewPCG(9, 9)), c: b.Conn(tracetest.ConnOptions{Client: ep("10.0.0.10:40000"), Server: ep("10.0.0.2:502")})}
	for i := range 24 {
		p.txn(1, readReq(3, 0, 1), regs(3, uint16(i)), false)
		b.Advance(time.Second)
		if i == 12 {
			b.Advance(-3 * time.Second)
		}
	}
	p.c.Close()
}

func manyScript(b *tracetest.Builder) {
	r := rand.New(rand.NewPCG(10, 10))
	for k := range 300 {
		client := fmt.Sprintf("10.30.%d.%d:%d", 1+k%12, 10+k%12, 40000+k)
		server := fmt.Sprintf("10.31.0.%d:502", 1+k%25)
		p := &poller{b: b, r: r, c: b.Conn(tracetest.ConnOptions{Client: ep(client), Server: ep(server)})}
		p.txn(uint8(1+k%4), readReq(3, uint16(k%7)*10, 4), regs(3, uint16(k), 1, 2, 3), false)
		p.c.Close()
		b.Advance(200 * time.Millisecond)
	}
}

func ipv6Script(b *tracetest.Builder) {
	p := &poller{b: b, r: rand.New(rand.NewPCG(11, 11)), c: b.Conn(tracetest.ConnOptions{Client: ep("[fd00:20::10]:40000"), Server: ep("[fd00:20::2]:502")})}
	for i := range 12 {
		p.txn(1, readReq(3, 0, 2), regs(3, uint16(i), 1), false)
		b.Advance(time.Second)
	}
	p.c.Close()
}

func tlsScript(b *tracetest.Builder) {
	r := rand.New(rand.NewPCG(12, 12))
	tls := b.Conn(tracetest.ConnOptions{Client: ep("10.0.0.10:43000"), Server: ep("10.0.0.3:802")})
	hello := append([]byte{0x16, 0x03, 0x01, 0x00, 0xF0, 0x01, 0x00, 0x00, 0xEC, 0x03, 0x03}, noise(236, 12)...)
	tls.Write(tracetest.ToServer, tracetest.WriteOpts{}, tracetest.Frame{Raw: hello})
	tls.Write(tracetest.ToClient, tracetest.WriteOpts{}, tracetest.Frame{Raw: append([]byte{0x16, 0x03, 0x03, 0x00, 0x5a, 0x02}, noise(89, 13)...)})
	for range 10 {
		tls.Write(tracetest.ToServer, tracetest.WriteOpts{}, tracetest.Frame{Raw: append([]byte{0x17, 0x03, 0x03, 0x00, 0x20}, noise(32, uint64(r.Uint32()))...)})
		tls.Write(tracetest.ToClient, tracetest.WriteOpts{}, tracetest.Frame{Raw: append([]byte{0x17, 0x03, 0x03, 0x00, 0x24}, noise(36, uint64(r.Uint32()))...)})
		b.Advance(time.Second)
	}
	tls.Close()
	shortPoll(b)
}

func twoIfaceScript(b *tracetest.Builder) {
	c := b.Conn(tracetest.ConnOptions{Client: ep("10.0.0.10:40000"), Server: ep("10.0.0.2:502")})
	for i := range 12 {
		tid := uint16(i + 1)
		c.Request(tid, 1, readReq(3, 0, 1))
		b.Advance(8 * time.Millisecond)
		o := tracetest.WriteOpts{}
		if i%3 == 0 {
			o.Duplicate = []int{0}
		}
		c.Write(tracetest.ToClient, o, tracetest.Frame{TID: tid, Unit: 1, PDU: regs(3, uint16(i))})
		b.Advance(time.Second)
	}
	c.Close()
	b.InterfaceDrops(0, 17)
	b.InterfaceDrops(1, 0)
}

// noiseScript puts ARP, UDP and ICMP frames between a short poll.
func noiseScript(b *tracetest.Builder) {
	eth := func(typ uint16, payload []byte) []byte {
		h := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02, 0, 0, 0, 0, 9}
		return append(binary.BigEndian.AppendUint16(h, typ), payload...)
	}
	ipv4 := func(proto byte, payload []byte) []byte {
		ip := []byte{0x45, 0, 0, 0, 0, 1, 0, 0, 64, proto, 0, 0, 10, 0, 0, 9, 10, 0, 0, 255}
		binary.BigEndian.PutUint16(ip[2:], uint16(20+len(payload)))
		return append(ip, payload...)
	}
	arp := []byte{0, 1, 8, 0, 6, 4, 0, 1, 2, 0, 0, 0, 0, 9, 10, 0, 0, 9, 0, 0, 0, 0, 0, 0, 10, 0, 0, 2}
	for k := range 6 {
		b.Packet(0, eth(0x0806, arp))
		b.Packet(0, eth(0x0800, ipv4(17, append([]byte{0x13, 0x88, 0x13, 0x88, 0, 16, 0, 0}, noise(8, uint64(k))...))))
		b.Packet(0, eth(0x0800, ipv4(1, []byte{8, 0, 0, 0, 0, 1, 0, byte(k)})))
	}
	shortPoll(b)
}

func simplePacketScript(b *tracetest.Builder) {
	shortPoll(b)
	b.SimplePacket([]byte{0x02, 0, 0, 0, 0, 2, 0x02, 0, 0, 0, 0, 1, 0x08, 0x06})
}

// ---------------------------------------------------------------------------

func readmeText(m []Entry) string {
	var b strings.Builder
	b.WriteString(`# Test corpus

Packet traces and MBCAP captures for tests, demos and screenshots, all synthetic: written by
` + "`internal/corpus`" + ` (` + "`make golden`" + ` rewrites them; a test checks they are what the generator makes).
` + "`manifest.json`" + ` says how the application must take each file; the engine's corpus test, the fuzz test
and ` + "`make smoke-corpus`" + ` (the built application on every file) hold it to that.

- **realistic/**: scenarios as a site would record them; use these for screenshots and demos.
- **edge/**: valid input that is hard to read: losses, odd link types and file variants, pairing trouble,
  clocks, scale, IPv6, TLS, unusual captures.
- **broken/**: empty, garbage, cut, corrupt and misnamed files. "error" means a clear message and no
  crash; "ok" means what can be read is shown.

| File | Kind | Expect | About |
|---|---|---|---|
`)
	for _, e := range m {
		exp := e.Expect
		if e.Expect == "ok" && e.MinTransactions > 0 {
			exp = fmt.Sprintf("ok, ≥ %d", e.MinTransactions)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", e.File, e.Kind, exp, strings.ReplaceAll(e.About, "|", "\\|"))
	}
	return b.String()
}
