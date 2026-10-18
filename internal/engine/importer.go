// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"slices"
	"strings"

	"github.com/otfabric/go-modbus-trace/export/mbcapexport"
	"github.com/otfabric/go-modbus-trace/mbtcp"
	"github.com/otfabric/go-modbus-trace/model"
	"github.com/otfabric/go-modbus-trace/trace"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// Scan starts the first pass over a trace: decompression of gzip files
// into the cache, then the library's Scan (file summary and flows).
func (e *Engine) Scan(_ context.Context, id string) (service.JobRef, error) {
	t, err := e.trace(id)
	if err != nil {
		return service.JobRef{}, err
	}
	if t.info.Kind == "capture" {
		return service.JobRef{}, service.Errorf(service.CodeInvalidArgument, "this is an MBCAP capture: import it, it has no flows to scan")
	}
	if !t.claim() {
		return service.JobRef{}, service.Errorf(service.CodeInvalidArgument, "a scan or an analysis of this trace is running")
	}
	return e.start("scan", id, func(ctx context.Context, j *job) (string, error) {
		defer t.unclaim()
		if err := e.decompress(ctx, t, j); err != nil {
			return "", err
		}
		f, err := trace.OpenFiles(t.data...)
		if err != nil {
			return "", service.Wrap(service.CodeUnavailable, err, "cannot open the trace")
		}
		sum, flows, err := trace.Scan(ctx, f, trace.ScanOptions{Progress: func(read, total int64) {
			j.progress("scan", read, total, 0)
		}})
		if sum == nil {
			return "", traceError(err)
		}
		res := suggest(sum, flows, e.defaultPorts())
		t.mu.Lock()
		t.scan = &res
		t.mu.Unlock()
		if err != nil {
			// A damaged file: what was read up to the damage is usable.
			return "the trace is damaged; it was read up to the damage: " + err.Error(), nil
		}
		if sum.Reader.TruncatedTail {
			return "the capture ends in the middle of a packet; the rest of the file was read", nil
		}
		return "", nil
	}), nil
}

func (t *openTrace) claim() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.busy {
		return false
	}
	t.busy = true
	return true
}

func (t *openTrace) unclaim() {
	t.mu.Lock()
	t.busy = false
	t.mu.Unlock()
}

// decompress replaces gzip inputs by their decompressed copies in the
// cache (once per open).
func (e *Engine) decompress(ctx context.Context, t *openTrace, j *job) error {
	for i, p := range t.info.Paths {
		if t.data[i] != p {
			continue // done earlier
		}
		gz, err := store.IsGzip(p)
		if err != nil {
			return service.Wrap(service.CodeUnavailable, err, "cannot read the trace")
		}
		if !gz {
			continue
		}
		out, err := e.cache.Decompress(ctx, p, fmt.Sprintf("%s-%d", t.info.ID, i), func(read, total int64) {
			j.progress("decompress", read, total, 0)
		})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			if errors.Is(err, store.ErrNoSpace) {
				return service.Wrap(service.CodeUnavailable, err, "cannot decompress the trace")
			}
			return service.Wrap(service.CodeUnavailable, err, "cannot decompress %s", t.info.Name)
		}
		t.data[i] = out
	}
	return nil
}

// traceError turns the library's errors into messages a user can act on.
func traceError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, trace.ErrFormat):
		return service.Wrap(service.CodeInvalidArgument, err, "this file is not a pcap or pcapng trace")
	case errors.Is(err, trace.ErrCorrupt):
		return service.Wrap(service.CodeInvalidArgument, err, "the trace is damaged")
	default:
		return service.Wrap(service.CodeUnavailable, err, "cannot read the trace")
	}
}

// suggest builds the Flows step: the Modbus-like flows are preselected and
// their server ports prefill the ports field (R2).
func suggest(sum *trace.FileSummary, flows []trace.Flow, extra []uint16) service.ScanResult {
	ports := []uint16{502}
	for _, p := range extra {
		if !slices.Contains(ports, p) {
			ports = append(ports, p)
		}
	}
	var sel []uint32
	for _, f := range flows {
		if f.Class != trace.ClassModbus {
			continue
		}
		sel = append(sel, f.ID)
		if p := f.ServerPort; p != 0 && !slices.Contains(ports, p) {
			ports = append(ports, p)
		}
	}
	slices.Sort(ports)
	modbusPorts := map[uint16]bool{}
	for _, f := range flows {
		if f.Class == trace.ClassModbus {
			modbusPorts[f.ServerPort] = true
		}
	}
	if flows == nil {
		flows = []trace.Flow{}
	}
	if sel == nil {
		sel = []uint32{}
	}
	return service.ScanResult{Summary: sum, Flows: flows, ServerPorts: ports, Selected: sel, OneClick: len(modbusPorts) == 1}
}

// ScanResult returns what the finished scan found.
func (e *Engine) ScanResult(_ context.Context, id string) (service.ScanResult, error) {
	t, err := e.trace(id)
	if err != nil {
		return service.ScanResult{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.scan == nil {
		return service.ScanResult{}, service.Errorf(service.CodeNotFound, "the trace has not been scanned")
	}
	return *t.scan, nil
}

// Prepare decompresses an analysed gzip trace again: its copy lives only
// while the trace is open (A16), its analysis stays in the cache.
func (e *Engine) Prepare(_ context.Context, id string) (service.JobRef, error) {
	t, err := e.trace(id)
	if err != nil {
		return service.JobRef{}, err
	}
	if !t.claim() {
		return service.JobRef{}, service.Errorf(service.CodeInvalidArgument, "a scan or an analysis of this trace is running")
	}
	return e.start("prepare", id, func(ctx context.Context, j *job) (string, error) {
		defer t.unclaim()
		if err := e.decompress(ctx, t, j); err != nil {
			return "", err
		}
		t.mu.Lock()
		t.info.Prepare, t.lib = false, nil
		t.mu.Unlock()
		return "", nil
	}), nil
}

// Analyze starts the second pass: the library's events become the index,
// which replaces any earlier one of the same trace when it is complete.
func (e *Engine) Analyze(_ context.Context, id string, cfg service.AnalyzeConfig) (service.JobRef, error) {
	t, err := e.trace(id)
	if err != nil {
		return service.JobRef{}, err
	}
	if t.info.Kind == "capture" {
		return service.JobRef{}, service.Errorf(service.CodeInvalidArgument, "this is an MBCAP capture: import it instead")
	}
	opts, err := traceOptions(cfg)
	if err != nil {
		return service.JobRef{}, err
	}
	if !t.claim() {
		return service.JobRef{}, service.Errorf(service.CodeInvalidArgument, "a scan or an analysis of this trace is running")
	}
	return e.start("analyze", id, func(ctx context.Context, j *job) (string, error) {
		defer t.unclaim()
		if err := e.decompress(ctx, t, j); err != nil {
			return "", err
		}
		return e.analyze(ctx, t, j, opts, cfg)
	}), nil
}

func (e *Engine) analyze(ctx context.Context, t *openTrace, j *job, opts trace.Options, cfg service.AnalyzeConfig) (string, error) {
	f, err := trace.OpenFiles(t.data...)
	if err != nil {
		return "", service.Wrap(service.CodeUnavailable, err, "cannot open the trace")
	}
	w, err := e.cache.Begin(t.info.ID)
	if err != nil {
		return "", service.Wrap(service.CodeUnavailable, err, "cannot write the analysis cache")
	}
	b := newBuilder(w)
	var read, total int64
	opts.Progress = func(r, tot int64) {
		read, total = r, tot
		j.progress("analyze", r, tot, w.Rows())
	}
	sum, err := trace.Analyze(ctx, f, opts, trace.SinkFunc(b.event))
	if sum == nil || errors.Is(err, context.Canceled) || b.err != nil {
		_ = w.Abort()
		if b.err != nil {
			return "", service.Wrap(service.CodeInternal, b.err, "cannot write the analysis cache")
		}
		return "", traceError(err)
	}
	warning := ""
	if err != nil {
		warning = "the trace is damaged; it was analysed up to the damage: " + err.Error()
	}
	j.progress("index", read, total, w.Rows())
	choices, _ := json.Marshal(cfg)
	meta := store.Meta{Library: e.lib, Semantics: model.EventSemanticsVersion, Fingerprint: t.info.ID, Paths: t.info.Paths, Choices: choices}
	docs := map[string]any{"summary.json": sum, "conns.json": b.connList()}
	if err := e.cache.Commit(t.info.ID, w, meta, docs); err != nil {
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
	t.info.Analyzed, t.info.Choices = true, &cfg
	t.mu.Unlock()
	return warning, nil
}

// builder turns the library's events into index rows.
type builder struct {
	w     *store.Writer
	conns map[uint32]model.Connection
	order []uint32
	last  int64 // time of the last event with a time: gaps, resyncs and notes have none
	err   error
}

func newBuilder(w *store.Writer) *builder {
	b := &builder{w: w, conns: map[uint32]model.Connection{}}
	// Dictionary entries 0: "none" (notes have no device and no client).
	_, b.err = w.Device("", 0)
	if b.err == nil {
		_, b.err = w.Client("")
	}
	return b
}

// connDevice is the dictionary entry of a connection's server without a
// unit (unit 0 with the server-only marker), so that connection, gap and
// resync rows follow a server filter.
func (b *builder) connDevice(c uint32) uint16 {
	cn, ok := b.conns[c]
	if !ok {
		return 0
	}
	d, err := b.w.Device(serverOnly(cn.Server.String()), 0)
	if err != nil {
		b.err = err
	}
	return d
}

// serverOnly marks a device entry that stands for a server as a whole.
func serverOnly(server string) string { return server + "#" }

func (b *builder) client(c uint32) uint16 {
	cn, ok := b.conns[c]
	if !ok {
		return 0
	}
	id, err := b.w.Client(cn.Client.Addr().String())
	if err != nil {
		b.err = err
	}
	return id
}

func (b *builder) event(ev *model.Event) error {
	if b.err != nil {
		return b.err
	}
	if ev.Time != nil {
		b.last = ev.Time.UnixNano()
	}
	r := store.Row{TimeNS: b.last, Sub: store.NoSub, LatUS: store.NoLatency}
	var req, resp []model.FrameRef
	switch ev.Kind {
	case model.KindConnOpen, model.KindConnClose:
		c := *ev.Conn
		if _, seen := b.conns[c.ID]; !seen {
			b.order = append(b.order, c.ID)
		}
		b.conns[c.ID] = c
		r.Kind = store.KindConnOpen
		if ev.Kind == model.KindConnClose {
			r.Kind = store.KindConnClose
		}
		r.Conn, r.Device, r.Client = c.ID, b.connDevice(c.ID), b.client(c.ID)
		r.Extra, b.err = b.w.Extra(c)
	case model.KindGap:
		r.Kind, r.Conn = store.KindGap, ev.Gap.Conn
		r.Device, r.Client = b.connDevice(r.Conn), b.client(r.Conn)
		r.Extra, b.err = b.w.Extra(ev.Gap)
	case model.KindResync:
		r.Kind, r.Conn = store.KindResync, ev.Resync.Conn
		r.Device, r.Client = b.connDevice(r.Conn), b.client(r.Conn)
		r.Extra, b.err = b.w.Extra(ev.Resync)
	case model.KindNote:
		r.Kind = store.KindNote
		r.Extra, b.err = b.w.Extra(ev.Note)
	case model.KindTxn:
		x := ev.Txn
		if tt := x.Time(); tt != nil {
			r.TimeNS = tt.UnixNano()
			b.last = r.TimeNS
		}
		r.Kind, r.Conn, r.TID, r.FC = store.KindTxn, x.Conn, x.TID, x.Function
		r.Device, b.err = b.w.Device(x.Server.String(), x.Unit)
		if b.err == nil {
			r.Client, b.err = b.w.Client(x.Client.Addr().String())
		}
		if x.Sub != nil {
			r.Sub = *x.Sub
		}
		r.Class = store.Code(store.ClassNames[:], x.Class)
		r.Status = store.Code(store.StatusNames[:], x.Status)
		r.Exc = x.Exception
		r.Flags = store.Flags(x.Flags)
		for _, rg := range x.Ranges {
			switch {
			case rg.Write && r.Space != 0:
				r.WStart, r.WCount = rg.Start, rg.Count // FC23's write part
			case r.Space == 0:
				r.Space, r.Start, r.Count = store.Code(store.SpaceNames[:], rg.Space), rg.Start, rg.Count
			}
		}
		if x.Response != nil && x.Request != nil {
			r.LatUS = uint32(min(math.MaxUint32-1, max(0, x.LatencyNS/1000)))
		}
		r.MBCAP = reasonCode(mbcapexport.Classify(x))
		var size int
		if x.Request != nil {
			size += x.Request.Size
		}
		if x.Response != nil {
			size += x.Response.Size
			h := fnv.New32a()
			_, _ = h.Write(x.Response.PDU)
			r.RespHash = uint16(h.Sum32()) // collisions of 1 in 65,536 only hide a change from the volatility
		}
		r.Bytes = uint16(min(size, math.MaxUint16))
		// The request in words (UX.md section 3); the outcome is the Result
		// column, built from the status. A request alone groups a pattern.
		summary := x.Summary
		if x.Request != nil && x.Request.View != nil {
			summary = mbtcp.Summary(*x.Request.View, nil)
		}
		r.Summary = b.w.Summary(summary)
		if x.Request != nil {
			req = x.Request.Refs
		}
		if x.Response != nil {
			resp = x.Response.Refs
		}
	default:
		return nil
	}
	if b.err != nil {
		return b.err
	}
	b.err = b.w.Append(r, req, resp)
	return b.err
}

// connList returns the connections as last seen, in order of first sight.
func (b *builder) connList() []model.Connection {
	out := make([]model.Connection, 0, len(b.order))
	for _, id := range b.order {
		out = append(out, b.conns[id])
	}
	return out
}

// reasonCode stores an export classification: 0 for exported, else the
// reason's position in mbcapexport.Reasons plus one.
func reasonCode(r mbcapexport.Reason) uint8 {
	if r == mbcapexport.Admit {
		return 0
	}
	if i := slices.Index(mbcapexport.Reasons, r); i >= 0 {
		return uint8(i + 1)
	}
	return math.MaxUint8
}

func reasonOf(code uint8) mbcapexport.Reason {
	if code == 0 {
		return mbcapexport.Admit
	}
	if int(code) <= len(mbcapexport.Reasons) {
		return mbcapexport.Reasons[code-1]
	}
	return "unknown"
}

// deviceName is the server of a device entry and whether it stands for the
// whole server.
func deviceName(d store.Device) (server string, whole bool) {
	if s, ok := strings.CutSuffix(d.Server, "#"); ok {
		return s, true
	}
	return d.Server, false
}
