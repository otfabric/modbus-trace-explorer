// SPDX-License-Identifier: MIT

package engine

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-modbus-capture/capture"
	"github.com/otfabric/go-modbus-capture/jsonl"
	"github.com/otfabric/go-modbus-capture/mbcap"
	"github.com/otfabric/go-modbus-capture/mbcsv"

	"github.com/otfabric/go-modbus-trace/mbtcp"
	"github.com/otfabric/go-modbus-trace/model"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// An MBCAP capture (R24) is shown in the same timeline as a trace: one row
// per sample, its target as the device and its stream as the connection.
// A capture holds PDUs, not packets; JSON Lines and CSV cannot be read at a
// sample's position, so the import keeps the samples' PDUs in the cache
// entry (pdus.bin with an offset per row in pdus.idx), where the detail and
// the address history read them. The MBAP header the detail shows is
// rebuilt from the target's unit (transaction ID 0) and says so.

const (
	captureDoc = "capture.json"
	pduData    = "pdus.bin"
	pduIndex   = "pdus.idx"
)

// captureEncoding names the encoding of a capture file, or "" for a file
// that is not one. The binary encoding is recognised by its magic number;
// JSON Lines and CSV by the name, as the capture tools write them.
func captureEncoding(path string) string {
	f, err := os.Open(path)
	if err == nil {
		var b [len(mbcap.Magic)]byte
		_, rerr := io.ReadFull(f, b[:])
		_ = f.Close()
		if rerr == nil && string(b[:]) == mbcap.Magic {
			return "mbcap"
		}
	}
	name := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasSuffix(name, ".mbcap"):
		return "mbcap"
	case strings.HasSuffix(name, ".mbcap.jsonl"), strings.HasSuffix(name, ".jsonl"):
		return "jsonl"
	case strings.HasSuffix(name, ".mbcap.csv"), strings.HasSuffix(name, ".csv"):
		return "csv"
	}
	return ""
}

// counter counts the bytes read through it (the import's progress).
type counter struct {
	r io.Reader
	n int64
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// openCapture returns the reader of a capture file and the count of the
// bytes read so far.
func openCapture(path, encoding string) (capture.Reader, io.Closer, *counter, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, err
	}
	cnt := &counter{r: f}
	in := bufio.NewReaderSize(cnt, 1<<20)
	var r capture.Reader
	switch encoding {
	case "mbcap":
		r, err = mbcap.NewReader(in)
	case "jsonl":
		r, err = jsonl.NewReader(in)
	case "csv":
		r, err = mbcsv.NewReader(in)
	default:
		err = fmt.Errorf("not a capture")
	}
	if err != nil {
		_ = f.Close()
		return nil, nil, nil, err
	}
	return r, f, cnt, nil
}

// targetName is a target as the device column shows it: host:port for a
// network target (as a trace's server), the serial device or the label
// otherwise.
func targetName(t capture.Target) string {
	switch {
	case t.Host != "":
		host := t.Host
		if strings.Contains(host, ":") {
			host = "[" + host + "]" // IPv6, as a trace writes a server
		}
		return host + ":" + strconv.Itoa(int(t.Port))
	case t.Serial.Device != "":
		return t.Serial.Device
	case t.Label != "":
		return t.Label
	}
	return "target " + strconv.Itoa(int(t.ID))
}

// streamClient is a stream as the client column shows it: the client's
// address of an exported capture ("client 10.0.3.5"), else its label.
func streamClient(s capture.Stream) string {
	if a, ok := strings.CutPrefix(s.Label, "client "); ok {
		if _, err := netip.ParseAddr(a); err == nil {
			return a
		}
	}
	if s.Label != "" {
		return s.Label
	}
	return "stream " + strconv.Itoa(int(s.ID))
}

// ImportCapture reads an MBCAP capture into the analysis cache (R24).
func (e *Engine) ImportCapture(_ context.Context, id string) (service.JobRef, error) {
	t, err := e.trace(id)
	if err != nil {
		return service.JobRef{}, err
	}
	if t.info.Kind != "capture" {
		return service.JobRef{}, service.Errorf(service.CodeInvalidArgument, "this is a trace: scan and analyse it")
	}
	if !t.claim() {
		return service.JobRef{}, service.Errorf(service.CodeInvalidArgument, "an import of this capture is running")
	}
	return e.start("capture", id, func(ctx context.Context, j *job) (string, error) {
		defer t.unclaim()
		return e.importCapture(ctx, t, j)
	}), nil
}

func (e *Engine) importCapture(ctx context.Context, t *openTrace, j *job) (string, error) {
	path := t.data[0]
	enc := captureEncoding(path)
	r, closer, cnt, err := openCapture(path, enc)
	if err != nil {
		return "", service.Wrap(service.CodeInvalidArgument, err, "%s is not a capture that can be read", t.info.Name)
	}
	defer func() { _ = closer.Close() }()
	h := r.Header()
	w, err := e.cache.Begin(t.info.ID)
	if err != nil {
		return "", service.Wrap(service.CodeUnavailable, err, "cannot write the analysis cache")
	}
	pd, err := newPDUWriter(w.Dir())
	if err != nil {
		_ = w.Abort()
		return "", service.Wrap(service.CodeUnavailable, err, "cannot write the analysis cache")
	}
	facts := service.CaptureFacts{Encoding: enc, Producer: h.Producer, TickNS: h.TickNS, WallStartNS: h.WallStart.UnixNano(),
		ChangeOnly: h.ChangeOnly, Exceptions: h.Exceptions, Provenance: []string{}, Source: h.Tags[capture.TagSource]}
	if p := h.Tags[capture.TagProvenance]; p != "" {
		facts.Provenance = strings.Split(p, " | ")
	}
	targets := map[uint32]capture.Target{}
	streams := map[uint32]capture.Stream{}
	var order []uint32
	conns := map[uint32]*model.Connection{}
	fail := func(err error) (string, error) {
		_ = pd.close()
		_ = w.Abort()
		return "", err
	}
	total := int64(0)
	if fi, err := os.Stat(path); err == nil {
		total = fi.Size()
	}
	warning := ""
read:
	for n := 0; ; n++ {
		if n&0xFFF == 0 {
			if ctx.Err() != nil {
				return fail(ctx.Err())
			}
			j.progress("import", min(cnt.n, total), total, w.Rows())
		}
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			break read
		}
		if err != nil {
			// What was read before a cut or damage is shown, as for a
			// trace; damage before the first sample leaves nothing to show.
			switch {
			case errors.Is(err, capture.ErrTruncated):
				facts.Truncated = true
				warning = "the capture ends inside a record; the samples before it are shown"
			case facts.Samples > 0:
				facts.Damage = err.Error()
				warning = "the capture is damaged after " + strconv.FormatUint(facts.Samples, 10) + " samples; those are shown: " + err.Error()
			default:
				return fail(service.Wrap(service.CodeInvalidArgument, err, "the capture is damaged"))
			}
			break read
		}
		switch x := rec.(type) {
		case capture.Target:
			targets[x.ID] = x
			facts.Targets++
		case capture.Stream:
			streams[x.ID] = x
			facts.Streams++
		case capture.End:
			facts.EndKnown = x.Known
		case capture.Sample:
			// The readers check that a sample's stream and target were
			// defined before it (a capture's invariants).
			st := streams[x.Stream]
			tg := targets[st.Target]
			row, err := sampleRow(w, h, tg, st, x)
			if err != nil {
				return fail(service.Wrap(service.CodeInternal, err, "cannot write the analysis cache"))
			}
			if err := pd.add(x.Request, x.Response); err != nil {
				return fail(service.Wrap(service.CodeUnavailable, err, "cannot write the analysis cache"))
			}
			if err := w.Append(row, nil, nil); err != nil {
				return fail(service.Wrap(service.CodeInternal, err, "cannot write the analysis cache"))
			}
			facts.Samples++
			c := conns[x.Stream]
			if c == nil {
				c = &model.Connection{ID: x.Stream, Open: "capture", Close: "capture", RoleSource: "capture", RoleConfidence: 1}
				if a, err := netip.ParseAddr(streamClient(st)); err == nil {
					c.Client = netip.AddrPortFrom(a, 0)
				}
				if ap, err := netip.ParseAddrPort(targetName(tg)); err == nil {
					c.Server = ap
				}
				conns[x.Stream] = c
				order = append(order, x.Stream)
			}
			at := h.Time(x.At)
			if c.First == nil {
				c.First = &at
			}
			c.Last = &at
			c.Bytes[0] += uint64(len(x.Request))
			c.Bytes[1] += uint64(len(x.Response))
		}
	}
	if err := pd.close(); err != nil {
		_ = w.Abort()
		return "", service.Wrap(service.CodeUnavailable, err, "cannot write the analysis cache")
	}
	j.progress("index", total, total, w.Rows())
	list := make([]model.Connection, 0, len(order))
	for _, id := range order {
		list = append(list, *conns[id])
	}
	meta := store.Meta{Library: e.lib, Semantics: model.EventSemanticsVersion, Fingerprint: t.info.ID, Paths: t.info.Paths}
	if err := e.cache.Commit(t.info.ID, w, meta, map[string]any{captureDoc: facts, "conns.json": list}); err != nil {
		return "", service.Wrap(service.CodeInternal, err, "cannot write the analysis cache")
	}
	ix, err := e.cache.Lookup(t.info.ID)
	if err != nil {
		return "", service.Wrap(service.CodeInternal, err, "cannot read the analysis cache")
	}
	if err := t.attach(ix); err != nil {
		_ = ix.Close()
		return "", service.Wrap(service.CodeInternal, err, "cannot read the analysis cache")
	}
	t.mu.Lock()
	t.info.Analyzed = true
	t.mu.Unlock()
	return warning, nil
}

// sampleRow is the index row of a sample: a transaction of its stream.
func sampleRow(w *store.Writer, h capture.Header, tg capture.Target, st capture.Stream, s capture.Sample) (store.Row, error) {
	r := store.Row{Kind: store.KindTxn, TimeNS: h.Time(s.At).UnixNano(), Conn: s.Stream, Sub: store.NoSub, LatUS: store.NoLatency}
	var err error
	if r.Device, err = w.Device(targetName(tg), tg.Unit); err != nil {
		return r, err
	}
	if r.Client, err = w.Client(streamClient(st)); err != nil {
		return r, err
	}
	req := mbtcp.Decode(mbtcp.AsRequest, s.Request)
	r.FC = req.Function
	if req.Sub != nil {
		r.Sub = *req.Sub
	}
	r.Class = store.Code(store.ClassNames[:], string(req.Class))
	// A capture holds answered requests only (its readers refuse a sample
	// without a response): a sample is ok or an exception.
	status := model.StatusOK
	if s.IsException() {
		status = model.StatusException
		if len(s.Response) > 1 {
			r.Exc = s.Response[1]
		}
	}
	r.Status = store.Code(store.StatusNames[:], status)
	// A capture holds reads only (no writes, no FC23): one read range.
	if len(req.Ranges) > 0 {
		rg := req.Ranges[0]
		r.Space, r.Start, r.Count = store.Code(store.SpaceNames[:], string(rg.Space)), rg.Start, rg.Count
	}
	r.Bytes = uint16(min(len(s.Request)+len(s.Response), math.MaxUint16))
	if len(s.Response) > 0 {
		f := fnv.New32a()
		_, _ = f.Write(s.Response)
		r.RespHash = uint16(f.Sum32())
	}
	r.Summary = w.Summary(mbtcp.Summary(req, nil))
	return r, nil
}

// pduWriter keeps the samples' PDUs: pdus.bin holds, per row, the request
// and the response each as a u16 length and its bytes; pdus.idx holds each
// row's u64 offset in pdus.bin.
type pduWriter struct {
	data, idx *os.File
	bd, bi    *bufio.Writer
	off       uint64
}

func newPDUWriter(dir string) (*pduWriter, error) {
	d, err := os.OpenFile(filepath.Join(dir, pduData), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, store.FileMode)
	if err != nil {
		return nil, err
	}
	i, err := os.OpenFile(filepath.Join(dir, pduIndex), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, store.FileMode)
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	return &pduWriter{data: d, idx: i, bd: bufio.NewWriterSize(d, 1<<20), bi: bufio.NewWriterSize(i, 256<<10)}, nil
}

func (p *pduWriter) add(req, resp []byte) error {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], p.off)
	if _, err := p.bi.Write(b[:]); err != nil {
		return err
	}
	for _, pdu := range [][]byte{req, resp} {
		n := min(len(pdu), math.MaxUint16)
		binary.LittleEndian.PutUint16(b[:2], uint16(n))
		if _, err := p.bd.Write(b[:2]); err != nil {
			return err
		}
		if _, err := p.bd.Write(pdu[:n]); err != nil {
			return err
		}
		p.off += 2 + uint64(n)
	}
	return nil
}

func (p *pduWriter) close() error {
	return errors.Join(p.bd.Flush(), p.bi.Flush(), p.data.Close(), p.idx.Close())
}

// pduReader reads a row's PDUs from a capture's cache entry.
type pduReader struct {
	mu   sync.Mutex
	data *os.File
	idx  *os.File
}

func openPDUs(dir string) (*pduReader, error) {
	d, err := os.Open(filepath.Join(dir, pduData))
	if err != nil {
		return nil, err
	}
	i, err := os.Open(filepath.Join(dir, pduIndex))
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	return &pduReader{data: d, idx: i}, nil
}

// pdus returns row i's request and response PDUs.
func (p *pduReader) pdus(i uint32) (req, resp []byte, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var b [8]byte
	if _, err := p.idx.ReadAt(b[:], int64(i)*8); err != nil {
		return nil, nil, err
	}
	off := int64(binary.LittleEndian.Uint64(b[:]))
	out := [2][]byte{}
	for k := range out {
		if _, err := p.data.ReadAt(b[:2], off); err != nil {
			return nil, nil, err
		}
		n := int(binary.LittleEndian.Uint16(b[:2]))
		buf := make([]byte, n)
		if _, err := p.data.ReadAt(buf, off+2); err != nil {
			return nil, nil, err
		}
		out[k] = buf
		off += 2 + int64(n)
	}
	return out[0], out[1], nil
}

func (p *pduReader) close() error { return errors.Join(p.data.Close(), p.idx.Close()) }

// withMBAP rebuilds the MBAP header of a PDU (transaction ID 0), so a
// capture's sample is shown as a trace's ADU is.
func withMBAP(unit uint8, pdu []byte) []byte {
	if len(pdu) == 0 {
		return nil
	}
	adu := make([]byte, mbtcp.HeaderLen+len(pdu))
	binary.BigEndian.PutUint16(adu[4:], uint16(min(len(pdu)+1, math.MaxUint16)))
	adu[6] = unit
	copy(adu[mbtcp.HeaderLen:], pdu)
	return adu
}

// captureADUs returns row i's request and response as ADUs with a rebuilt
// MBAP header, read from the capture's cache entry.
func (t *openTrace) captureADUs(ix *store.Index, i uint32) (req, resp []byte, err error) {
	t.mu.Lock()
	if t.pdus == nil {
		p, err := openPDUs(ix.Dir())
		if err != nil {
			t.mu.Unlock()
			return nil, nil, err
		}
		t.pdus = p
	}
	p := t.pdus
	t.mu.Unlock()
	rq, rs, err := p.pdus(i)
	if err != nil {
		return nil, nil, err
	}
	unit := ix.Devices[ix.Row(i).Device].Unit
	return withMBAP(unit, rq), withMBAP(unit, rs), nil
}

// captureExceptions counts a capture's exception responses.
func captureExceptions(ix *store.Index) (n uint64) {
	exc := store.Code(store.StatusNames[:], model.StatusException)
	for _, st := range ix.Columns().Status {
		if st == exc {
			n++
		}
	}
	return n
}

// captureQuality is the Quality view of a capture: what it records and what
// may be missing from it.
func captureQuality(f *service.CaptureFacts, exceptions uint64) service.Quality {
	q := service.Quality{Conditions: []service.Condition{}, Facts: []service.Fact{}}
	add := func(code, label, severity, meaning string, n uint64, flt *service.Filter) {
		q.Conditions = append(q.Conditions, service.Condition{Code: code, Label: label, Count: n, Severity: severity, Meaning: meaning, Filter: flt})
	}
	b := func(v bool) uint64 {
		if v {
			return 1
		}
		return 0
	}
	add("truncated", "Ends inside a record", "problem", "The file is cut; the samples before the cut are shown.", b(f.Truncated), nil)
	damage := "The file is damaged after its first samples; the samples before the damage are shown."
	if f.Damage != "" {
		damage = "The file is damaged after its first samples (" + f.Damage + "); the samples before the damage are shown."
	}
	add("damaged", "Damaged", "problem", damage, b(f.Damage != ""), nil)
	add("end_unknown", "No recorded end", "warning", "The capture was not closed (a crash, or a copy taken while it was recording): its last moment is not known.", b(!f.EndKnown), nil)
	add("change_only", "Change-only", "info", "Repeated identical responses were not stored: a gap between samples does not mean nothing was asked.", b(f.ChangeOnly), nil)
	add("exceptions", "Exception responses", "info", "Requests the device answered with an exception.", exceptions, &service.Filter{Statuses: []string{model.StatusException}})
	fact := func(label, value string) { q.Facts = append(q.Facts, service.Fact{Label: label, Value: value}) }
	fact("Encoding", f.Encoding)
	fact("Producer", f.Producer)
	fact("Tick", (time.Duration(f.TickNS) * time.Nanosecond).String())
	fact("Start", time.Unix(0, f.WallStartNS).UTC().Format(time.RFC3339Nano))
	fact("Targets", strconv.Itoa(f.Targets))
	fact("Streams", strconv.Itoa(f.Streams))
	fact("Samples", strconv.FormatUint(f.Samples, 10))
	fact("Exceptions recorded", strconv.FormatBool(f.Exceptions))
	if f.Source != "" {
		fact("Source", f.Source)
	}
	for k, p := range f.Provenance {
		fact(fmt.Sprintf("Provenance %d", k+1), p)
	}
	return q
}
