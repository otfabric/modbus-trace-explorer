// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/otfabric/go-modbus-trace/mbtcp"
	"github.com/otfabric/go-modbus-trace/model"
	"github.com/otfabric/go-modbus-trace/trace"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// file returns the library's view of the trace for random access, opened
// once per open trace (its frame index is built on first use).
func (t *openTrace) file() (*trace.File, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lib == nil {
		f, err := trace.OpenFiles(t.data...)
		if err != nil {
			return nil, err
		}
		t.lib = f
	}
	return t.lib, nil
}

// Detail returns row seq with its ADUs decoded from the trace's bytes.
func (e *Engine) Detail(_ context.Context, id string, seq uint64) (service.Detail, error) {
	t, ix, release, err := e.index(id)
	if err != nil {
		return service.Detail{}, err
	}
	defer release()
	if seq == 0 || seq > ix.Rows() {
		return service.Detail{}, service.Errorf(service.CodeNotFound, "no row %d", seq)
	}
	i := uint32(seq - 1)
	r := ix.Row(i)
	d := service.Detail{Row: rowDTO(ix, i, r), Notes: []service.Note{}}
	t.mu.Lock()
	conn, haveConn := t.conns[r.Conn]
	t.mu.Unlock()
	if haveConn && r.Kind != store.KindNote {
		d.Conn = &service.ConnDetail{ID: conn.ID, Client: conn.Client.String(), Server: conn.Server.String(), Open: conn.Open,
			Close: conn.Close, RoleSource: conn.RoleSource, RoleConfidence: conn.RoleConfidence, Transactions: conn.Transactions}
	}
	if r.Kind != store.KindTxn {
		var extra any
		_ = json.Unmarshal(ix.Extra(r), &extra)
		d.Extra, d.Text = extra, d.Row.Summary
		return d, nil
	}
	for _, f := range d.Row.Flags {
		d.Notes = append(d.Notes, service.Note{Kind: "flag", Code: f, Meaning: flagMeaning[f]})
	}
	if d.Conn != nil {
		d.Notes = append(d.Notes, service.Note{Kind: "fact", Code: "connection",
			Meaning: fmt.Sprintf("Connection %d opened %s; the server was decided by %s (confidence %.2f).", conn.ID,
				map[string]string{"syn": "with a handshake", "midstream": "before the capture started"}[conn.Open],
				strings.ReplaceAll(conn.RoleSource, "_", " "), conn.RoleConfidence)})
	}
	if t.info.Kind == "capture" {
		return captureDetail(t, ix, i, d)
	}
	d.MBCAP = reasonText(reasonOf(r.MBCAP))
	req, resp, err := ix.Refs(r)
	if err != nil {
		return d, service.Wrap(service.CodeInternal, err, "the analysis cache is damaged")
	}
	d.Request, d.Response = &service.ADUDetail{Packets: packets(req)}, &service.ADUDetail{Packets: packets(resp)}
	if len(req) == 0 {
		d.Request = nil
	}
	if len(resp) == 0 {
		d.Response = nil
	}
	var reqADU, respADU []byte
	if t.present() && !t.preparing() {
		f, ferr := t.file()
		if ferr == nil {
			reqADU, err = aduBytes(f, req)
			if err == nil {
				respADU, err = aduBytes(f, resp)
			}
		}
		if ferr != nil || (err != nil && !errors.Is(err, trace.ErrNotSeekable)) {
			d.Notes = append(d.Notes, service.Note{Kind: "fact", Code: "bytes", Meaning: "The bytes could not be read from the trace."})
		}
	} else {
		d.Notes = append(d.Notes, service.Note{Kind: "fact", Code: "trace_missing",
			Meaning: "The trace is not at its path any more: the bytes and the decoded fields are not available."})
	}
	decodeInto(&d, reqADU, respADU)
	d.Text = paragraph(d.Row, ix)
	return d, nil
}

// decodeInto fills the request and response details from their ADUs.
func decodeInto(d *service.Detail, reqADU, respADU []byte) {
	var reqV mbtcp.View
	if d.Request != nil && len(reqADU) > mbtcp.HeaderLen {
		reqV = mbtcp.Decode(mbtcp.AsRequest, reqADU[mbtcp.HeaderLen:])
		fill(d.Request, reqADU, reqV, nil)
	}
	if d.Response != nil && len(respADU) > mbtcp.HeaderLen {
		respV := mbtcp.Decode(mbtcp.AsResponse, respADU[mbtcp.HeaderLen:])
		var pair []string
		if len(reqADU) > mbtcp.HeaderLen {
			pair = mbtcp.CheckPair(reqADU[mbtcp.HeaderLen:], reqV, respADU[mbtcp.HeaderLen:], &respV)
		}
		fill(d.Response, respADU, respV, pair)
	}
	for _, a := range []*service.ADUDetail{d.Request, d.Response} {
		if a == nil {
			continue
		}
		for _, w := range a.Warnings {
			if slices.ContainsFunc(d.Notes, func(n service.Note) bool { return n.Kind == "warning" && n.Code == w }) {
				continue
			}
			meaning := meaningOfWarning(w)
			if w == mbtcp.WarnMismatch && len(reqADU) > mbtcp.HeaderLen && len(respADU) > mbtcp.HeaderLen {
				meaning += readSizes(reqADU[mbtcp.HeaderLen:], respADU[mbtcp.HeaderLen:])
			}
			d.Notes = append(d.Notes, service.Note{Kind: "warning", Code: w, Meaning: meaning})
		}
	}
}

// readSizes states the sizes of a register read that does not fit its
// request, from the bytes themselves.
func readSizes(req, resp []byte) string {
	if len(req) < 5 || len(resp) < 2 || req[0] != resp[0] || (req[0] != 3 && req[0] != 4) {
		return ""
	}
	n := int(binary.BigEndian.Uint16(req[3:]))
	return fmt.Sprintf(" It asked for %d registers (%d bytes); the byte count says %d and %d data bytes follow.", n, 2*n, resp[1], len(resp)-2)
}

// captureDetail is the detail of a capture's sample (R24): its PDUs from
// the cache entry, behind a rebuilt MBAP header, and no packets.
func captureDetail(t *openTrace, ix *store.Index, i uint32, d service.Detail) (service.Detail, error) {
	req, resp, err := t.captureADUs(ix, i)
	if err != nil {
		return d, service.Wrap(service.CodeInternal, err, "the analysis cache is damaged")
	}
	d.Conn = nil
	d.Request = &service.ADUDetail{Packets: []service.PacketRef{}}
	if len(resp) > 0 {
		d.Response = &service.ADUDetail{Packets: []service.PacketRef{}}
	}
	d.Notes = append(d.Notes, service.Note{Kind: "fact", Code: "capture_sample",
		Meaning: "A sample of an MBCAP capture: the capture keeps the PDUs and the time of the response; the MBAP header shown is rebuilt (transaction ID 0), and there are no packets."})
	decodeInto(&d, req, resp)
	d.Text = paragraph(d.Row, ix)
	return d, nil
}

// aduBytes concatenates the payload of an ADU's packets. A packet whose
// payload was reassembled from IP fragments has no payload range: then
// the ADU's bytes are not available (nil, nil).
func aduBytes(f *trace.File, refs []model.FrameRef) ([]byte, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	frames, err := f.Frames(refs)
	if err != nil {
		return nil, err
	}
	var out []byte
	for _, fr := range frames {
		if fr.Payload == nil {
			return nil, nil
		}
		out = append(out, fr.Payload...)
	}
	return out, nil
}

func fill(a *service.ADUDetail, adu []byte, v mbtcp.View, extra []string) {
	a.Hex = hex.EncodeToString(adu)
	a.Name = v.Name
	u := func(n uint64) *uint64 { return &n }
	a.Fields = []service.Field{
		{Name: "transaction_id", Offset: 0, Length: 2, Value: u(uint64(adu[0])<<8 | uint64(adu[1]))},
		{Name: "protocol_id", Offset: 2, Length: 2, Value: u(uint64(adu[2])<<8 | uint64(adu[3]))},
		{Name: "length", Offset: 4, Length: 2, Value: u(uint64(adu[4])<<8 | uint64(adu[5]))},
		{Name: "unit_id", Offset: 6, Length: 1, Value: u(uint64(adu[6]))},
	}
	for _, f := range v.Fields {
		a.Fields = append(a.Fields, service.Field{Name: f.Name, Offset: f.Offset, Length: f.Length, Value: f.Value, Hex: f.Hex})
	}
	a.Warnings = append(append([]string{}, v.Warnings...), extra...)
}

func packets(refs []model.FrameRef) []service.PacketRef {
	out := make([]service.PacketRef, 0, len(refs))
	for _, r := range refs {
		p := service.PacketRef{Frame: r.Frame, Part: r.Part, Iface: r.Iface, Offset: r.Offset, PayOff: r.PayOff, PayLen: r.PayLen,
			Display: "frame.number == " + strconv.FormatUint(r.Frame, 10)}
		if r.Time != nil {
			ns := r.Time.UnixNano()
			p.TimeNS = &ns
		}
		out = append(out, p)
	}
	return out
}

// paragraph says what happened in words (UX.md section 4).
func paragraph(row service.Row, ix *store.Index) string {
	_ = ix
	unit := ""
	if row.Unit != nil {
		unit = " unit " + strconv.Itoa(int(*row.Unit))
	}
	what := row.Summary
	if what != "" {
		what = strings.ToLower(what[:1]) + what[1:]
	}
	s := fmt.Sprintf("%s asked %s%s: %s.", row.Client, row.Server, unit, what)
	switch row.Status {
	case model.StatusOK, model.StatusException:
		if row.LatencyNS != nil {
			s += fmt.Sprintf(" It answered in %s: %s.", latency(*row.LatencyNS), row.Result)
		} else {
			s += " It answered: " + row.Result + "."
		}
	default:
		s += " " + row.Result + "."
	}
	return s
}

func latency(ns int64) string {
	if ns < 1_000_000 {
		return strconv.FormatInt(ns/1000, 10) + " µs"
	}
	return strconv.FormatFloat(float64(ns)/1e6, 'f', 1, 64) + " ms"
}
