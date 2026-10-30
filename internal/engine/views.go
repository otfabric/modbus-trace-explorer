// SPDX-License-Identifier: MIT

package engine

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"math/bits"
	"slices"
	"strconv"

	"github.com/otfabric/go-modbus-trace/model"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// The views aggregate the rows of the active filter (R7 applies to every
// view) by scanning their columns: no per-view index exists, so a new
// filter costs one pass over the selection.

// selection returns the filtered rows in time order with the columns, the
// index held until the caller calls release.
func (e *Engine) selection(ctx context.Context, id string, f service.Filter) (*store.Index, *store.Selection, store.Columns, func(), error) {
	_, ix, release, err := e.index(id)
	if err != nil {
		return nil, nil, store.Columns{}, nil, err
	}
	sf, err := compile(ix, f)
	if err != nil {
		release()
		return nil, nil, store.Columns{}, nil, err
	}
	sel, err := ix.Select(ctx, sf, store.ByTime)
	if err != nil {
		release()
		return nil, nil, store.Columns{}, nil, err
	}
	return ix, sel, ix.Columns(), release, nil
}

// distribution summarises durations: exact quantiles and a histogram of
// buckets a power of two apart.
func distribution(ns []int64) *service.Distribution {
	if len(ns) == 0 {
		return nil
	}
	slices.Sort(ns)
	q := func(p float64) int64 { return ns[min(len(ns)-1, int(math.Ceil(p*float64(len(ns))))-1)] }
	d := &service.Distribution{Count: uint64(len(ns)), MinNS: ns[0], P50NS: q(0.5), P95NS: q(0.95), P99NS: q(0.99),
		MaxNS: ns[len(ns)-1], Histogram: []service.Bucket{}}
	lo := bits.Len64(uint64(max(ns[0], 1))) - 1
	hi := bits.Len64(uint64(max(ns[len(ns)-1], 1)))
	k := 0
	for b := lo; b < hi; b++ {
		bucket := service.Bucket{LoNS: 1 << b, HiNS: 1 << (b + 1)}
		for k < len(ns) && ns[k] < bucket.HiNS {
			bucket.Count++
			k++
		}
		d.Histogram = append(d.Histogram, bucket)
	}
	return d
}

// Devices aggregates the filtered transactions per server and unit.
func (e *Engine) Devices(ctx context.Context, id string, f service.Filter) ([]service.DeviceStats, error) {
	ix, sel, c, release, err := e.selection(ctx, id, f)
	if err != nil {
		return nil, err
	}
	defer release()
	type acc struct {
		s       *service.DeviceStats
		clients map[uint16]bool
		lat     []int64
	}
	by := map[uint16]*acc{}
	ok := store.Code(store.StatusNames[:], model.StatusOK)
	exc := store.Code(store.StatusNames[:], model.StatusException)
	unans := store.Code(store.StatusNames[:], model.StatusUnanswered)
	read, write := store.Code(store.ClassNames[:], "read"), store.Code(store.ClassNames[:], "write")
	for k := range sel.Len() {
		if k&0xFFFF == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		i := sel.At(k, false)
		if c.Kind[i] != store.KindTxn {
			continue
		}
		d := c.Device[i]
		a := by[d]
		if a == nil {
			server, _ := deviceName(ix.Devices[d])
			a = &acc{s: &service.DeviceStats{Server: server, Unit: ix.Devices[d].Unit, ByFunction: map[string]uint64{},
				ByStatus: map[string]uint64{}, FirstNS: c.T[i], LastNS: c.T[i]}, clients: map[uint16]bool{}}
			by[d] = a
		}
		s := a.s
		s.Transactions++
		s.ByFunction[strconv.Itoa(int(c.FC[i]))]++
		s.ByStatus[store.StatusNames[c.Status[i]]]++
		switch c.Status[i] {
		case exc:
			s.Exceptions++
		case unans:
			s.Unanswered++
		}
		switch c.Class[i] {
		case read:
			s.Reads++
		case write:
			s.Writes++
		}
		a.clients[c.Client[i]] = true
		s.FirstNS, s.LastNS = min(s.FirstNS, c.T[i]), max(s.LastNS, c.T[i])
		s.Bytes += uint64(c.Bytes[i])
		if c.Lat[i] != store.NoLatency && (c.Status[i] == ok || c.Status[i] == exc) {
			a.lat = append(a.lat, int64(c.Lat[i])*1000)
		}
	}
	out := make([]service.DeviceStats, 0, len(by))
	for _, a := range by {
		for cl := range a.clients {
			a.s.Clients = append(a.s.Clients, ix.Clients[cl])
		}
		slices.Sort(a.s.Clients)
		if span := a.s.LastNS - a.s.FirstNS; span > 0 {
			a.s.Rate = float64(a.s.Transactions) / (float64(span) / 1e9)
		}
		a.s.Latency = distribution(a.lat)
		out = append(out, *a.s)
	}
	slices.SortFunc(out, func(a, b service.DeviceStats) int {
		return cmp.Or(cmp.Compare(a.Server, b.Server), cmp.Compare(a.Unit, b.Unit))
	})
	return out, nil
}

// Connections lists the connections with rows in the filter, with their
// counts in it.
func (e *Engine) Connections(ctx context.Context, id string, f service.Filter) ([]service.ConnStats, error) {
	t, err := e.trace(id)
	if err != nil {
		return nil, err
	}
	_, sel, c, release, err := e.selection(ctx, id, f)
	if err != nil {
		return nil, err
	}
	defer release()
	by := map[uint32]*service.ConnStats{}
	flags := map[uint32]uint32{}
	for k := range sel.Len() {
		if k&0xFFFF == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		i := sel.At(k, false)
		cn := c.Conn[i]
		if cn == 0 && c.Kind[i] == store.KindNote {
			continue
		}
		s := by[cn]
		if s == nil {
			s = &service.ConnStats{ID: cn, Flags: []string{}, FirstNS: c.T[i], LastNS: c.T[i]}
			by[cn] = s
		}
		s.FirstNS, s.LastNS = min(s.FirstNS, c.T[i]), max(s.LastNS, c.T[i])
		switch c.Kind[i] {
		case store.KindTxn:
			s.Transactions++
			flags[cn] |= c.Flags[i]
		case store.KindGap:
			s.Gaps++
		case store.KindResync:
			s.Resyncs++
		}
	}
	t.mu.Lock()
	conns := t.conns
	t.mu.Unlock()
	out := make([]service.ConnStats, 0, len(by))
	for cn, s := range by {
		if info, ok := conns[cn]; ok {
			s.Client, s.Server, s.Open, s.Close = info.Client.String(), info.Server.String(), info.Open, info.Close
			s.RoleSource, s.RoleConfidence = info.RoleSource, info.RoleConfidence
			s.RoleGuessed = info.RoleSource == "structure" || (info.RoleSource == "port" && info.Open == "midstream")
			s.Bytes = info.Bytes[0] + info.Bytes[1]
			if info.First != nil {
				s.FirstNS = info.First.UnixNano()
			}
			if info.Last != nil {
				s.LastNS = info.Last.UnixNano()
			}
		}
		s.Flags = store.FlagList(flags[cn])
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b service.ConnStats) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

// Patterns finds the recurring requests: the same client asking the same
// device the same thing (the same summary of the request).
func (e *Engine) Patterns(ctx context.Context, id string, f service.Filter) ([]service.Pattern, error) {
	ix, sel, c, release, err := e.selection(ctx, id, f)
	if err != nil {
		return nil, err
	}
	defer release()
	type key struct {
		client, device uint16
		summary        uint32
		fc             uint8
	}
	type acc struct {
		p        *service.Pattern
		times    []int64
		lastResp uint16
		haveResp bool
	}
	by := map[key]*acc{}
	var order []key
	ok := store.Code(store.StatusNames[:], model.StatusOK)
	exc := store.Code(store.StatusNames[:], model.StatusException)
	for k := range sel.Len() {
		if k&0xFFFF == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		i := sel.At(k, false)
		if c.Kind[i] != store.KindTxn || c.Summary[i] == 0 {
			continue
		}
		kk := key{c.Client[i], c.Device[i], c.Summary[i], c.FC[i]}
		a := by[kk]
		if a == nil {
			if len(by) >= maxPatterns {
				continue
			}
			server, _ := deviceName(ix.Devices[kk.device])
			a = &acc{p: &service.Pattern{Client: ix.Clients[kk.client], Server: server, Unit: ix.Devices[kk.device].Unit,
				Function: kk.fc, Summary: ix.Summaries[kk.summary], Space: store.SpaceNames[c.Space[i]], Start: c.Start[i],
				Count: c.Count[i], FirstNS: c.T[i], LastNS: c.T[i]}}
			by[kk] = a
			order = append(order, kk)
		}
		p := a.p
		p.Requests++
		p.FirstNS, p.LastNS = min(p.FirstNS, c.T[i]), max(p.LastNS, c.T[i])
		a.times = append(a.times, c.T[i])
		if c.Status[i] == ok || c.Status[i] == exc { // answered: an exception is an answer too
			if a.haveResp && c.RespHash[i] != a.lastResp {
				p.Changed++
			}
			p.Responses++
			a.lastResp, a.haveResp = c.RespHash[i], true
		}
	}
	out := make([]service.Pattern, 0, len(by))
	for _, kk := range order {
		a := by[kk]
		p := a.p
		if len(a.times) > 1 {
			gaps := make([]int64, 0, len(a.times)-1)
			for j := 1; j < len(a.times); j++ {
				gaps = append(gaps, a.times[j]-a.times[j-1])
			}
			p.Interval = distribution(gaps)
			dev := make([]int64, len(gaps))
			for j, g := range gaps {
				dev[j] = abs(g - p.Interval.P50NS)
			}
			slices.Sort(dev)
			p.JitterNS = dev[(len(dev)-1)/2]
		}
		if p.Responses > 1 {
			p.Volatility = float64(p.Changed) / float64(p.Responses-1)
		}
		switch {
		case p.Changed == 0:
			p.VolatilityClass = "static"
		case p.Volatility < 0.2:
			p.VolatilityClass = "slow"
		default:
			p.VolatilityClass = "noisy"
		}
		out = append(out, *p)
	}
	slices.SortStableFunc(out, func(a, b service.Pattern) int { return cmp.Compare(b.Requests, a.Requests) })
	return out, nil
}

// maxPatterns bounds the patterns of one view (as the library's analyzer).
const maxPatterns = 10_000

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// Coverage merges the address ranges read and written of a device's data
// space within the filter.
func (e *Engine) Coverage(ctx context.Context, id string, q service.CoverageQuery) (service.CoverageMap, error) {
	f := q.Filter
	f.Servers, f.Units = []string{q.Server}, []uint8{q.Unit}
	ix, sel, c, release, err := e.selection(ctx, id, f)
	if err != nil {
		return service.CoverageMap{}, err
	}
	defer release()
	space := store.Code(store.SpaceNames[:], q.Space)
	out := service.CoverageMap{Server: q.Server, Unit: q.Unit, Space: q.Space, Spaces: []string{}, Read: []service.Interval{},
		Written: []service.Interval{}}
	if q.Space != "" && space == 0 {
		return out, service.Errorf(service.CodeInvalidArgument, "unknown data space %q", q.Space)
	}
	spaces := map[uint8]bool{}
	ok := store.Code(store.StatusNames[:], model.StatusOK)
	write := store.Code(store.ClassNames[:], "write")
	add := func(list []service.Interval, start, count uint16, t int64) []service.Interval {
		end := uint16(min(0xFFFF, uint32(start)+uint32(count)-1))
		return mergeInterval(list, service.Interval{Start: start, End: end, Count: 1, FirstNS: t, LastNS: t})
	}
	for k := range sel.Len() {
		if k&0xFFFF == 0 && ctx.Err() != nil {
			return out, ctx.Err()
		}
		i := sel.At(k, false)
		if c.Kind[i] != store.KindTxn || c.Status[i] != ok || c.Count[i] == 0 {
			continue
		}
		spaces[c.Space[i]] = true
		if c.Space[i] != space {
			continue
		}
		if c.Class[i] == write && c.WCount[i] == 0 {
			out.Written = add(out.Written, c.Start[i], c.Count[i], c.T[i])
			continue
		}
		out.Read = add(out.Read, c.Start[i], c.Count[i], c.T[i])
		if c.WCount[i] > 0 { // FC23: its write part
			out.Written = add(out.Written, c.WStart[i], c.WCount[i], c.T[i])
		}
	}
	for s := range spaces {
		out.Spaces = append(out.Spaces, store.SpaceNames[s])
	}
	slices.Sort(out.Spaces)
	_ = ix
	return out, nil
}

// mergeInterval adds iv to a sorted list of disjoint intervals, joining
// those it overlaps or touches.
func mergeInterval(list []service.Interval, iv service.Interval) []service.Interval {
	i, _ := slices.BinarySearchFunc(list, iv.Start, func(x service.Interval, s uint16) int {
		if uint32(x.End)+1 < uint32(s) {
			return -1
		}
		return 1
	})
	j := i
	for j < len(list) && uint32(list[j].Start) <= uint32(iv.End)+1 {
		x := list[j]
		iv.Start, iv.End = min(iv.Start, x.Start), max(iv.End, x.End)
		iv.Count += x.Count
		iv.FirstNS, iv.LastNS = min(iv.FirstNS, x.FirstNS), max(iv.LastNS, x.LastNS)
		j++
	}
	return append(list[:i:i], append([]service.Interval{iv}, list[j:]...)...)
}

// Quality lists every counter of the library with its meaning (R12).
func (e *Engine) Quality(_ context.Context, id string) (service.Quality, error) {
	t, ix, release, err := e.index(id)
	if err != nil {
		return service.Quality{}, err
	}
	defer release()
	t.mu.Lock()
	s, facts := t.sum, t.capture
	t.mu.Unlock()
	if facts != nil {
		return captureQuality(facts, captureExceptions(ix)), nil
	}
	q := service.Quality{Conditions: []service.Condition{}, Facts: []service.Fact{}}
	if s == nil {
		return q, nil
	}
	// A counter is about segments or bytes; only rows that carry the flag can
	// back "Show in timeline", so a counter with none offers no filter.
	flag := func(f string) *service.Filter {
		if s.TransactionFlags[f] == 0 {
			return nil
		}
		return &service.Filter{Flags: []string{f}}
	}
	status := func(st string) *service.Filter { return &service.Filter{Statuses: []string{st}} }
	var drops uint64
	if s.CaptureDrop != nil {
		drops = *s.CaptureDrop
	}
	var unsupported uint64
	for _, n := range s.Unsupported {
		unsupported += n
	}
	add := func(code, label, severity, meaning string, n uint64, f *service.Filter) {
		q.Conditions = append(q.Conditions, service.Condition{Code: code, Label: label, Count: n, Severity: severity, Meaning: meaning, Filter: f})
	}
	add("capture_drops", "Dropped by the capture host", "problem", "The capture tool reported packets it could not keep: reads may be missing.", drops, nil)
	add("gaps", "Gaps", "problem", "Bytes of a connection the capture missed; transactions next to them are uncertain.", s.Quality.Gaps, flag("gap_adjacent"))
	add("resync_failed", "Lost framing", "problem", "Bytes that could not be framed as Modbus after a gap or junk.", s.Quality.ResyncFailed, nil)
	add("resyncs", "Resyncs", "warning", "Framing was lost and recovered; the ADU after it may not be real.", s.Quality.Resyncs, flag("resynced"))
	add("retransmitted", "Retransmissions", "info", "Segments seen more than once; the first copy was used. A copy that arrives after its transaction is complete marks no row.", s.Quality.Retransmitted, flag("retransmitted"))
	add("out_of_order", "Out of order", "info", "Segments that arrived before earlier bytes and were put back in order.", s.Quality.OutOfOrder, flag("out_of_order"))
	add("overlap_conflict", "Overlap conflicts", "warning", "Retransmitted bytes that differ from the first copy.", s.Quality.OverlapConflicts, nil)
	add("snaplen_truncated", "Cut by the snap length", "warning", "ADUs the capture cut short (capture with -s 0 to avoid it).", s.Quality.SnaplenTruncated, flag("snaplen_truncated"))
	add("foreign_protocol", "Not Modbus", "info", "Frames with a protocol ID other than 0, skipped by their length.", s.Quality.ForeignFrames, flag("foreign_protocol"))
	add("unsupported", "Unsupported encapsulations", "info", "Packets that are not TCP or not decodable (UDP, other link types, tunnels).", unsupported, nil)
	for _, st := range []struct{ code, label, sev, meaning string }{
		{model.StatusOrphan, "Orphan responses", "warning", "Responses without a request (a midstream start or a lost request)."},
		{model.StatusUnanswered, "Unanswered requests", "warning", "Requests with no response observed before the connection or the trace ended."},
		{model.StatusAmbiguous, "Ambiguous pairs", "warning", "Responses that could belong to more than one request."},
		{model.StatusMalformed, "Malformed", "warning", "ADUs too short or cut, reported alone."},
		{model.StatusUncertain, "Uncertain", "warning", "Transactions touched by a gap or a resync, or whose roles are unknown."},
	} {
		add(st.code, st.label, st.sev, st.meaning, s.Transactions[st.code], status(st.code))
	}
	for _, fl := range []struct{ code, label, sev string }{
		{"invalid_pdu", "Invalid PDUs", "warning"},
		{"clock_order", "Clock-order anomalies", "warning"},
		{"role_guessed", "Guessed roles", "info"},
		{"slow", "Slow responses", "info"},
	} {
		add(fl.code, fl.label, fl.sev, flagMeaning[fl.code], s.TransactionFlags[fl.code], flag(fl.code))
	}
	add("internal_errors", "Internal errors", "problem", "Packets the library could not process (a bug worth reporting).", s.InternalErrorCount, nil)
	links := []string{}
	for _, f := range s.Interfaces {
		if !slices.Contains(links, f.Link) {
			links = append(links, f.Link)
		}
	}
	fact := func(label, value string) { q.Facts = append(q.Facts, service.Fact{Label: label, Value: value}) }
	fact("Format", s.Format)
	fact("Link types", fmt.Sprint(links))
	fact("Interfaces", strconv.Itoa(len(s.Interfaces)))
	fact("Packets", strconv.FormatUint(s.Packets, 10))
	fact("Packets cut by the snap length", strconv.FormatUint(s.Snaplen.TruncatedPackets, 10))
	fact("Rows in the index", strconv.FormatUint(ix.Rows(), 10))
	if s.SHA256 != "" {
		fact("SHA-256", s.SHA256)
	}
	if s.Error != "" {
		fact("Read error", s.Error)
	}
	return q, nil
}
