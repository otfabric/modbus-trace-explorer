// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"encoding/binary"
	"time"

	"github.com/otfabric/go-modbus-trace/mbtcp"
	"github.com/otfabric/go-modbus-trace/model"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// DefaultHistoryLimit caps the points of one address history (R11).
const DefaultHistoryLimit = 200_000

// historyBatch is how many candidate rows are read from the trace between
// two chunks (and two checks for cancellation).
const historyBatch = 2_000

// AddressHistory starts a job that reads the observed values of one
// address from the trace: every read that covers it and every write to
// it, in time order. Points arrive in "history" events; a capped history
// ends with Next, which continues it.
func (e *Engine) AddressHistory(_ context.Context, id string, q service.HistoryQuery) (service.JobRef, error) {
	t, ix, err := e.index(id)
	if err != nil {
		return service.JobRef{}, err
	}
	space := store.Code(store.SpaceNames[:], q.Space)
	if space == 0 {
		return service.JobRef{}, service.Errorf(service.CodeInvalidArgument, "unknown data space %q", q.Space)
	}
	if !t.present() {
		return service.JobRef{}, service.Errorf(service.CodeUnavailable, "the trace is not at its path: values are read from it")
	}
	if t.preparing() {
		return service.JobRef{}, service.Errorf(service.CodeUnavailable, "the trace is not decompressed yet")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultHistoryLimit
	}
	return e.start("history", id, func(ctx context.Context, j *job) (string, error) {
		f, err := t.file()
		if err != nil {
			return "", service.Wrap(service.CodeUnavailable, err, "cannot open the trace")
		}
		cands, err := historyCandidates(ctx, ix, q, space)
		if err != nil {
			return "", err
		}
		chunk := service.HistoryChunk{Job: j.state.ID, Points: []service.HistoryPoint{}, Candidates: uint64(len(cands)),
			Scanned: min(q.After, uint64(len(cands)))}
		var prev *uint16
		points := 0
		last := e.now()
		flush := func(done bool) {
			if !done && len(chunk.Points) == 0 {
				return
			}
			chunk.Done = done
			c := chunk
			e.broadcast(service.Event{Kind: "history", History: &c})
			chunk.Points = []service.HistoryPoint{}
			last = e.now()
		}
		for k := int(chunk.Scanned); k < len(cands); k++ {
			if k%historyBatch == 0 && ctx.Err() != nil {
				return "", ctx.Err()
			}
			if points >= limit {
				next := uint64(k)
				chunk.Next = &next
				break
			}
			i := cands[k]
			r := ix.Row(i)
			req, resp, err := ix.Refs(r)
			if err != nil {
				return "", service.Wrap(service.CodeInternal, err, "the analysis cache is damaged")
			}
			reqADU, err := aduBytes(f, req)
			if err != nil || len(reqADU) <= mbtcp.HeaderLen {
				chunk.Scanned++
				continue
			}
			p := service.HistoryPoint{TimeNS: r.TimeNS, Seq: uint64(i) + 1, Client: ix.Clients[r.Client]}
			v, ok := writeValueAt(reqADU[mbtcp.HeaderLen:], q.Address)
			if ok {
				p.Write, p.Value = true, v
			} else {
				respADU, err := aduBytes(f, resp)
				if err != nil || len(respADU) <= mbtcp.HeaderLen {
					chunk.Scanned++
					continue
				}
				v, ok = mbtcp.ValueAt(reqADU[mbtcp.HeaderLen:], respADU[mbtcp.HeaderLen:], q.Address)
				if !ok {
					chunk.Scanned++
					continue
				}
				p.Value = v
				p.Changed = prev != nil && *prev != v
				if !q.Every && prev != nil && *prev == v {
					chunk.Scanned++
					continue
				}
				prev = &v
			}
			chunk.Points = append(chunk.Points, p)
			chunk.Scanned++
			points++
			if len(chunk.Points) >= historyBatch || e.now().Sub(last) > 200*time.Millisecond {
				j.progress("history", int64(chunk.Scanned), int64(len(cands)), uint64(points))
				flush(false)
			}
		}
		flush(true)
		return "", nil
	}), nil
}

// historyCandidates are the rows, in time order, of the device's answered
// transactions whose read range or write range covers the address.
func historyCandidates(ctx context.Context, ix *store.Index, q service.HistoryQuery, space uint8) ([]uint32, error) {
	dev := make([]bool, len(ix.Devices))
	for i, d := range ix.Devices {
		server, whole := deviceName(d)
		dev[i] = !whole && server == q.Server && d.Unit == q.Unit
	}
	ok := store.Code(store.StatusNames[:], model.StatusOK)
	sel, err := ix.Select(ctx, &store.Filter{Devices: dev, Kinds: []bool{true}}, store.ByTime)
	if err != nil {
		return nil, err
	}
	c := ix.Columns()
	covers := func(start, count uint16) bool {
		return count > 0 && q.Address >= start && uint32(q.Address) < uint32(start)+uint32(count)
	}
	var out []uint32
	for k := range sel.Len() {
		i := sel.At(k, false)
		if c.Status[i] != ok || c.Space[i] != space {
			continue
		}
		if covers(c.Start[i], c.Count[i]) || covers(c.WStart[i], c.WCount[i]) {
			out = append(out, i)
		}
	}
	return out, nil
}

// writeValueAt returns the value a write request puts at addr: FC05 and
// FC15 (bits as 0/1), FC06 and FC16 (words), and the write part of FC23.
func writeValueAt(pdu []byte, addr uint16) (uint16, bool) {
	be := binary.BigEndian
	if len(pdu) < 5 {
		return 0, false
	}
	switch pdu[0] {
	case 5:
		if be.Uint16(pdu[1:]) == addr {
			return map[bool]uint16{true: 1, false: 0}[be.Uint16(pdu[3:]) == 0xFF00], true
		}
	case 6:
		if be.Uint16(pdu[1:]) == addr {
			return be.Uint16(pdu[3:]), true
		}
	case 15, 16:
		if len(pdu) < 6 {
			return 0, false
		}
		start, n := be.Uint16(pdu[1:]), be.Uint16(pdu[3:])
		if addr < start || uint32(addr) >= uint32(start)+uint32(n) {
			return 0, false
		}
		k := int(addr - start)
		data := pdu[6:]
		if pdu[0] == 15 {
			if k/8 < len(data) {
				return uint16(data[k/8]>>(k%8)) & 1, true
			}
			return 0, false
		}
		if 2*k+2 <= len(data) {
			return be.Uint16(data[2*k:]), true
		}
	case 23:
		if len(pdu) < 10 {
			return 0, false
		}
		start, n := be.Uint16(pdu[5:]), be.Uint16(pdu[7:])
		if addr < start || uint32(addr) >= uint32(start)+uint32(n) {
			return 0, false
		}
		k := int(addr - start)
		if data := pdu[10:]; 2*k+2 <= len(data) {
			return be.Uint16(data[2*k:]), true
		}
	}
	return 0, false
}
