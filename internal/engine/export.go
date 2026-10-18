// SPDX-License-Identifier: MIT

package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/otfabric/go-modbus-capture/capture"
	"github.com/otfabric/go-modbus-capture/jsonl"
	"github.com/otfabric/go-modbus-capture/mbcap"
	"github.com/otfabric/go-modbus-capture/mbcsv"

	"github.com/otfabric/go-modbus-trace/export/mbcapexport"
	"github.com/otfabric/go-modbus-trace/export/subtrace"
	"github.com/otfabric/go-modbus-trace/export/timeline"
	"github.com/otfabric/go-modbus-trace/mbtcp"
	"github.com/otfabric/go-modbus-trace/model"
	"github.com/otfabric/go-modbus-trace/selection"
	"github.com/otfabric/go-modbus-trace/trace"

	"github.com/otfabric/modbus-trace-explorer/internal/buildinfo"
	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// ExportMode is the mode of exported files: the user's own files, readable
// as their other documents are (the umask still applies to the directory).
const ExportMode = 0o644

// maxResults bounds the finished exports kept for ExportResult.
const maxResults = 32

// replayDoc is the library's replay walkthrough (R16).
const replayDoc = "https://github.com/otfabric/go-modbus-trace/blob/main/docs/REPLAY-WORKFLOW.md"

// producer names the application in the files it writes.
func producer() string { return "modbus-trace-explorer " + buildinfo.Version }

// selectionOf turns a filter into the library's selection. The selection
// has no terms for exception codes or a request summary, so those are
// refused rather than silently dropped.
func selectionOf(f service.Filter) (selection.Selection, error) {
	bad := func(format string, args ...any) error {
		return service.Errorf(service.CodeInvalidArgument, format, args...)
	}
	var s selection.Selection
	if len(f.Exceptions) > 0 {
		return s, bad("an export cannot select by exception code: remove the exc: term")
	}
	if f.Summary != "" {
		return s, bad("an export cannot select by request: remove the request term")
	}
	for _, v := range f.Servers {
		if ap, err := netip.ParseAddrPort(v); err == nil {
			s.Servers = append(s.Servers, ap)
		} else if a, err := netip.ParseAddr(v); err == nil {
			s.Servers = append(s.Servers, netip.AddrPortFrom(a, 0))
		} else {
			return s, bad("server %q: an address, or an address and a port", v)
		}
	}
	for _, v := range f.Clients {
		a, err := netip.ParseAddr(v)
		if err != nil {
			return s, bad("client %q: an address", v)
		}
		s.Clients = append(s.Clients, a)
	}
	s.Units, s.Conns, s.Functions = slices.Clone(f.Units), slices.Clone(f.Conns), slices.Clone(f.Functions)
	s.Statuses, s.Classes = slices.Clone(f.Statuses), slices.Clone(f.Classes)
	s.Require, s.Exclude = slices.Clone(f.Flags), slices.Clone(f.NotFlags)
	if f.FromNS != nil {
		t := time.Unix(0, *f.FromNS)
		s.From = &t
	}
	if f.ToNS != nil {
		t := time.Unix(0, *f.ToNS)
		s.To = &t
	}
	if f.Space != "" {
		s.Space, s.Start, s.End = mbtcp.Space(f.Space), 0, 0xFFFF
		if f.Start != nil {
			s.Start = *f.Start
		}
		if f.End != nil {
			s.End = *f.End
		}
	} else if f.Start != nil || f.End != nil {
		return s, bad("an address range in an export needs a data space")
	}
	return s, nil
}

// exclusionMeaning explains each reason an export leaves a transaction out.
var exclusionMeaning = map[mbcapexport.Reason]string{
	mbcapexport.RoleUnknown:         "Which side is the server is not known, so the target is not either.",
	mbcapexport.Malformed:           "The request or the response is too short or cut.",
	mbcapexport.WriteFunction:       "Writes change a device; a capture holds reads only.",
	mbcapexport.FC23:                "Read/Write Multiple both writes and reads; a capture holds reads only.",
	mbcapexport.UnsupportedFunction: "Functions MBCAP has no place for (vendor and diagnostic functions).",
	mbcapexport.FC08StateChanging:   "Diagnostics sub-functions that change the device's state.",
	mbcapexport.MEINot14:            "Encapsulated transport other than device identification.",
	mbcapexport.Unanswered:          "No response was observed.",
	mbcapexport.OrphanResponse:      "A response without its request.",
	mbcapexport.AmbiguousPair:       "The response could belong to more than one request.",
	mbcapexport.Uncertain:           "Touched by a gap or a resync, so the bytes may not be what was sent.",
	mbcapexport.SnaplenTruncated:    "The capture cut the ADU short.",
	mbcapexport.Resynced:            "The ADU follows lost framing and may not be real.",
	mbcapexport.InvalidPDU:          "The PDU is not valid for its function.",
	mbcapexport.ResponseMismatch:    "The response does not answer the request (function, size or count).",
	mbcapexport.ClockOrder:          "The response is timed before its request.",
	mbcapexport.ExceptionsDisabled:  "Exception responses are switched off for this export.",
	mbcapexport.NotSelected:         "Outside the selection.",
	mbcapexport.OutsideWindow:       "Outside the time window.",
	mbcapexport.InvalidTime:         "The response has no usable timestamp.",
}

// PreviewMBCAP counts from the index what the export would do, applying the
// stored Classify decision and then the exporter's own order: exceptions,
// selection, time window.
func (e *Engine) PreviewMBCAP(ctx context.Context, id string, x service.MBCAPExport) (service.MBCAPPreview, error) {
	t, ix, err := e.index(id)
	if err != nil {
		return service.MBCAPPreview{}, err
	}
	if t.info.Kind == "capture" {
		return service.MBCAPPreview{}, service.Errorf(service.CodeInvalidArgument, "exports work on traces; this is already a capture")
	}
	sel, err := selectionOf(x.Filter)
	if err != nil {
		return service.MBCAPPreview{}, err
	}
	noTime := x.Filter
	noTime.FromNS, noTime.ToNS = nil, nil
	inSel, err := e.membership(ctx, ix, noTime)
	if err != nil {
		return service.MBCAPPreview{}, err
	}
	inWin := inSel
	if x.Filter.FromNS != nil || x.Filter.ToNS != nil {
		if inWin, err = e.membership(ctx, ix, x.Filter); err != nil {
			return service.MBCAPPreview{}, err
		}
	}
	c := ix.Columns()
	exception := store.Code(store.StatusNames[:], model.StatusException)
	counts := map[mbcapexport.Reason]uint64{}
	targets := map[uint16]*service.PreviewTarget{}
	var order []uint16
	clients := map[uint16]map[uint16]bool{}
	p := service.MBCAPPreview{Selection: sel.String(), UpTo: x.ChangeOnly, Targets: []service.PreviewTarget{},
		Excluded: []service.Exclusion{}, Notes: []string{}}
	for i := range len(c.Kind) {
		if c.Kind[i] != store.KindTxn {
			continue
		}
		p.Transactions++
		r := reasonOf(c.MBCAP[i])
		if r == mbcapexport.Admit && c.Status[i] == exception && !x.Exceptions {
			r = mbcapexport.ExceptionsDisabled
		}
		if r == mbcapexport.Admit && !inSel[i] {
			r = mbcapexport.NotSelected
		}
		if r == mbcapexport.Admit && !inWin[i] {
			r = mbcapexport.OutsideWindow
		}
		if r != mbcapexport.Admit {
			counts[r]++
			continue
		}
		p.Samples++
		d := c.Device[i]
		t := targets[d]
		if t == nil {
			server, _ := deviceName(ix.Devices[d])
			unit := ix.Devices[d].Unit
			key := fmt.Sprintf("%s/%d", server, unit)
			label := x.Labels[key]
			if label == "" {
				label = fmt.Sprintf("%s unit %d", server, unit)
			}
			t = &service.PreviewTarget{Key: key, Server: server, Unit: unit, Label: label, Clients: []string{}}
			targets[d] = t
			order = append(order, d)
			clients[d] = map[uint16]bool{}
		}
		t.Samples++
		if !clients[d][c.Client[i]] {
			clients[d][c.Client[i]] = true
			t.Clients = append(t.Clients, ix.Clients[c.Client[i]])
		}
	}
	for _, d := range order {
		p.Targets = append(p.Targets, *targets[d])
	}
	for _, r := range mbcapexport.Reasons {
		if n := counts[r]; n > 0 {
			p.Excluded = append(p.Excluded, service.Exclusion{Reason: string(r), Count: n, Meaning: exclusionMeaning[r]})
		}
	}
	if x.ChangeOnly {
		p.Notes = append(p.Notes, "Change-only drops a response equal to the last one of its range: the export can write fewer samples than shown.")
	}
	p.Notes = append(p.Notes, "A response without a usable timestamp is found only by the export (invalid_time).")
	return p, nil
}

// membership marks the rows of the filter.
func (e *Engine) membership(ctx context.Context, ix *store.Index, f service.Filter) ([]bool, error) {
	sf, err := compile(ix, f)
	if err != nil {
		return nil, err
	}
	sf.Kinds = []bool{true}
	sel, err := ix.Select(ctx, sf, store.ByTime)
	if err != nil {
		return nil, err
	}
	in := make([]bool, ix.Rows())
	for k := range sel.Len() {
		in[sel.At(k, false)] = true
	}
	return in, nil
}

// mbcapOptions are the exporter's options for an export from the dialog.
func mbcapOptions(x service.MBCAPExport, sel selection.Selection) mbcapexport.Options {
	step := []string{producer(), "export mbcap"}
	if x.TickNS != 0 {
		step = append(step, fmt.Sprintf("--tick-ns=%d", x.TickNS))
	}
	if x.ChangeOnly {
		step = append(step, "--change-only=true")
	}
	if !x.Exceptions {
		step = append(step, "--no-exceptions=true")
	}
	if s := sel.String(); s != "" {
		step = append(step, "--select="+s)
	}
	names := map[string]string{}
	for k, v := range x.Labels {
		if v = strings.TrimSpace(v); v != "" {
			names[k] = v
		}
	}
	return mbcapexport.Options{Producer: producer(), Step: strings.Join(step, " "), TickNS: x.TickNS, ChangeOnly: x.ChangeOnly,
		NoExceptions: !x.Exceptions, Names: names, Selection: sel}
}

// mbcapOpen returns the writer for an encoding, as mbtracectl makes it.
func mbcapOpen(format string, w io.Writer) (mbcapexport.Open, error) {
	switch format {
	case "mbcap", "":
		return func(h capture.Header) (capture.Writer, error) {
			return mbcap.NewWriter(w, h, &mbcap.Options{BlockSamples: 256, Compression: mbcap.Deflate})
		}, nil
	case "jsonl":
		return func(h capture.Header) (capture.Writer, error) { return jsonl.NewWriter(w, h) }, nil
	case "csv":
		return func(h capture.Header) (capture.Writer, error) { return mbcsv.NewWriter(w, h) }, nil
	}
	return nil, service.Errorf(service.CodeInvalidArgument, "encoding %q: mbcap, jsonl or csv", format)
}

// ExportMBCAP re-streams the trace through the library into path (R13).
func (e *Engine) ExportMBCAP(_ context.Context, id string, x service.MBCAPExport, path string) (service.JobRef, error) {
	sel, err := selectionOf(x.Filter)
	if err != nil {
		return service.JobRef{}, err
	}
	if x.TickNS > capture.MaxTickNS {
		return service.JobRef{}, service.Errorf(service.CodeInvalidArgument, "tick of %d ns: 1 ns to 1 s", x.TickNS)
	}
	if _, err := mbcapOpen(x.Format, io.Discard); err != nil {
		return service.JobRef{}, err
	}
	eo := mbcapOptions(x, sel)
	return e.export("mbcap", id, path, func(ctx context.Context, f *trace.File, o trace.Options, w io.Writer) (service.ExportResult, error) {
		open, _ := mbcapOpen(x.Format, w)
		rep, err := mbcapexport.Export(ctx, f, o, eo, open)
		if err != nil {
			return service.ExportResult{}, err
		}
		cmds, caveats := replay(path, rep)
		format := x.Format
		if format == "" {
			format = "mbcap"
		}
		return service.ExportResult{Format: format, Report: rep, Replay: cmds, Caveats: caveats, Doc: replayDoc}, nil
	})
}

// ExportTimeline writes the timeline of the filter as CSV or JSON Lines
// through the library (R14).
func (e *Engine) ExportTimeline(_ context.Context, id string, x service.TimelineExport, path string) (service.JobRef, error) {
	sel, err := selectionOf(x.Filter)
	if err != nil {
		return service.JobRef{}, err
	}
	var format timeline.Format
	switch x.Format {
	case "csv":
		format = timeline.CSV
	case "jsonl":
		format = timeline.JSONL
	default:
		return service.JobRef{}, service.Errorf(service.CodeInvalidArgument, "format %q: csv or jsonl", x.Format)
	}
	return e.export("timeline", id, path, func(ctx context.Context, f *trace.File, o trace.Options, w io.Writer) (service.ExportResult, error) {
		_, st, err := timeline.Write(ctx, f, o, w, timeline.Options{Format: format, Order: timeline.ByTime, Selection: sel})
		if err != nil {
			return service.ExportResult{}, err
		}
		return service.ExportResult{Format: x.Format, Rows: st.Rows, Late: st.Late}, nil
	})
}

// ExportSubtrace writes the packets of the selected connections as pcapng
// (R15).
func (e *Engine) ExportSubtrace(_ context.Context, id string, x service.SubtraceExport, path string) (service.JobRef, error) {
	f := x.Filter
	if len(f.Units)+len(f.Functions)+len(f.Statuses)+len(f.Classes)+len(f.Exceptions)+len(f.Flags)+len(f.NotFlags) > 0 ||
		f.Summary != "" || f.Space != "" || f.Start != nil || f.End != nil {
		return service.JobRef{}, service.Errorf(service.CodeInvalidArgument,
			"a sub-trace selects connections (server, client, conn) and a time window, not transactions: remove the other terms")
	}
	sel, err := selectionOf(f)
	if err != nil {
		return service.JobRef{}, err
	}
	return e.export("subtrace", id, path, func(ctx context.Context, tf *trace.File, o trace.Options, w io.Writer) (service.ExportResult, error) {
		st, err := subtrace.Extract(ctx, tf, o.ScanOptions, subtrace.Options{Selection: sel, Tool: producer()}, w)
		if err != nil {
			return service.ExportResult{}, err
		}
		return service.ExportResult{Format: "pcapng", Packets: st.Packets, Bytes: st.Bytes, Connections: st.Connections}, nil
	})
}

// export runs write as a job into path: through a temporary file in the
// same directory, renamed over path only when it is complete.
func (e *Engine) export(kind, id, path string, write func(ctx context.Context, f *trace.File, o trace.Options, w io.Writer) (service.ExportResult, error)) (service.JobRef, error) {
	t, _, err := e.index(id)
	if err != nil {
		return service.JobRef{}, err
	}
	if !t.present() {
		return service.JobRef{}, service.Errorf(service.CodeUnavailable, "the trace is not at its path: exports read it again")
	}
	if t.info.Kind == "capture" {
		return service.JobRef{}, service.Errorf(service.CodeInvalidArgument, "exports work on traces; this is already a capture")
	}
	if t.preparing() {
		return service.JobRef{}, service.Errorf(service.CodeUnavailable, "the trace is not decompressed yet")
	}
	if err := checkTarget(path, t); err != nil {
		return service.JobRef{}, err
	}
	var cfg service.AnalyzeConfig
	t.mu.Lock()
	if t.info.Choices != nil {
		cfg = *t.info.Choices
	}
	t.mu.Unlock()
	opts, err := traceOptions(cfg)
	if err != nil {
		return service.JobRef{}, err
	}
	return e.start(kind, id, func(ctx context.Context, j *job) (string, error) {
		f, err := trace.OpenFiles(t.data...)
		if err != nil {
			return "", service.Wrap(service.CodeUnavailable, err, "cannot open the trace")
		}
		o := opts
		o.Progress = func(r, tot int64) { j.progress(kind, r, tot, 0) }
		var res service.ExportResult
		err = writeAtomic(path, ExportMode, func(w io.Writer) error {
			var err error
			res, err = write(ctx, f, o, w)
			return err
		})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return "", err
			}
			var se *service.Error
			if errors.As(err, &se) {
				return "", err
			}
			return "", service.Wrap(service.CodeInternal, err, "the export failed")
		}
		res.Job, res.Kind, res.File = j.state.ID, kind, filepath.Base(path)
		e.keepResult(res)
		e.log.Info("export written", "kind", kind) // no path at info level (R23)
		return "", nil
	}), nil
}

// checkTarget refuses a path that is not absolute, whose directory is
// missing, or that is one of the trace's own files.
func checkTarget(path string, t *openTrace) error {
	if !filepath.IsAbs(path) {
		return service.Errorf(service.CodeInvalidArgument, "the export needs a full path")
	}
	if fi, err := os.Stat(filepath.Dir(path)); err != nil || !fi.IsDir() {
		return service.Errorf(service.CodeInvalidArgument, "the folder %s does not exist", filepath.Base(filepath.Dir(path)))
	}
	for _, p := range append(slices.Clone(t.info.Paths), t.data...) {
		if same(p, path) {
			return service.Errorf(service.CodeInvalidArgument, "the export would overwrite the trace")
		}
	}
	return nil
}

// same reports whether two paths name one file.
func same(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}

// writeAtomic writes path with mode through a temporary file beside it,
// renamed into place when write succeeds and removed when it does not.
func writeAtomic(path string, mode os.FileMode, write func(io.Writer) error) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.partial")
	if err != nil {
		return service.Wrap(service.CodeUnavailable, err, "cannot write in %s", filepath.Base(filepath.Dir(path)))
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	bw := bufio.NewWriterSize(tmp, 1<<20)
	if err = write(bw); err != nil {
		return err
	}
	if err = bw.Flush(); err != nil {
		return service.Wrap(service.CodeUnavailable, err, "cannot write the export")
	}
	if err = tmp.Sync(); err != nil {
		return service.Wrap(service.CodeUnavailable, err, "cannot write the export")
	}
	if err = tmp.Chmod(mode); err != nil {
		return service.Wrap(service.CodeUnavailable, err, "cannot write the export")
	}
	if err = tmp.Close(); err != nil {
		return service.Wrap(service.CodeUnavailable, err, "cannot write the export")
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return service.Wrap(service.CodeUnavailable, err, "cannot write the export")
	}
	return nil
}

// replay is the hand-off to mbcapctl and modbusctl (R16).
func replay(path string, rep *mbcapexport.Report) (cmds, caveats []string) {
	q := shellQuote(path)
	cmds = []string{"mbcapctl verify " + q}
	base := "modbusctl server replay --input " + q + " --listen 127.0.0.1"
	tail := " --before-first absent --missing exception"
	if len(rep.Targets) <= 1 {
		cmds = append(cmds, base+" --port 1502"+tail)
	} else {
		for i, t := range rep.Targets {
			cmds = append(cmds, fmt.Sprintf("%s --target %d --port %d%s", base, t.ID, 1502+i, tail))
		}
	}
	caveats = []string{
		"Writes are not replayed: the device answers with what was read.",
		"Only what was read exists: other addresses answer with an exception.",
		"Each target is served on its own port; targets that shared a unit ID need their own port.",
	}
	for _, w := range rep.Warnings {
		if w.Code == mbcapexport.WarnUnitCollision {
			caveats = append(caveats, "This export has targets with the same unit ID: serve them on separate ports.")
			break
		}
	}
	return cmds, caveats
}

// shellQuote quotes a path for a POSIX shell when it needs it.
func shellQuote(s string) string {
	plain := func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+:@", r)
	}
	if s != "" && strings.IndexFunc(s, func(r rune) bool { return !plain(r) }) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (e *Engine) keepResult(r service.ExportResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.results == nil {
		e.results = map[string]service.ExportResult{}
	}
	e.results[r.Job] = r
	e.resultOrder = append(e.resultOrder, r.Job)
	for len(e.resultOrder) > maxResults {
		delete(e.results, e.resultOrder[0])
		e.resultOrder = e.resultOrder[1:]
	}
}

// ExportResult returns what a finished export wrote.
func (e *Engine) ExportResult(_ context.Context, jobID string) (service.ExportResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.results[jobID]
	if !ok {
		return service.ExportResult{}, service.Errorf(service.CodeNotFound, "no finished export %s", jobID)
	}
	return r, nil
}

// SaveReport writes a finished MBCAP export's report as indented JSON.
func (e *Engine) SaveReport(ctx context.Context, jobID, path string) error {
	r, err := e.ExportResult(ctx, jobID)
	if err != nil {
		return err
	}
	if r.Report == nil {
		return service.Errorf(service.CodeInvalidArgument, "export %s has no report", jobID)
	}
	if !filepath.IsAbs(path) {
		return service.Errorf(service.CodeInvalidArgument, "the report needs a full path")
	}
	return writeAtomic(path, ExportMode, func(w io.Writer) error {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r.Report)
	})
}

// traceOptions are the library options of an analysis choice; exports
// use the trace's remembered choice, so they see the same transactions.
func traceOptions(cfg service.AnalyzeConfig) (trace.Options, error) {
	opts := trace.Options{Flows: cfg.Flows}
	opts.ServerPorts = cfg.ServerPorts
	for _, s := range cfg.Servers {
		ap, err := netip.ParseAddrPort(s)
		if err != nil {
			return opts, service.Errorf(service.CodeInvalidArgument, "server %q: an address and a port, such as 10.0.0.2:502", s)
		}
		opts.Servers = append(opts.Servers, ap)
	}
	return opts, nil
}
