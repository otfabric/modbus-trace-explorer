// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/otfabric/go-modbus-trace/model"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// MaxPage bounds a timeline page.
const MaxPage = 2000

// Overview returns the file strip's facts.
func (e *Engine) Overview(_ context.Context, id string) (service.Overview, error) {
	t, ix, err := e.index(id)
	if err != nil {
		return service.Overview{}, err
	}
	t.mu.Lock()
	sum, info := t.sum, t.info
	t.mu.Unlock()
	ov := service.Overview{File: info.Name, Size: info.Size, FirstNS: ix.Meta.FirstNS, LastNS: ix.Meta.LastNS, Rows: ix.Rows(),
		Summary: sum, TraceMissing: !t.present()}
	if sum != nil {
		ov.Packets = sum.Packets
		ov.Connections = sum.Connections.Total
		for _, n := range sum.Transactions {
			ov.Transactions += n
		}
		if sum.First != nil && sum.Last != nil {
			ov.FirstNS, ov.LastNS = sum.First.UnixNano(), sum.Last.UnixNano()
		}
	}
	for _, d := range ix.Devices {
		if _, whole := deviceName(d); !whole && d.Server != "" {
			ov.Devices++
		}
	}
	ov.Quality, ov.QualityCount = quality(sum)
	return ov, nil
}

// present reports whether every file of the trace is still where it was.
func (t *openTrace) present() bool {
	for _, p := range t.data {
		if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// Timeline returns a window of the filtered, ordered timeline.
func (e *Engine) Timeline(ctx context.Context, id string, q service.TimelineQuery) (service.TimelinePage, error) {
	_, ix, err := e.index(id)
	if err != nil {
		return service.TimelinePage{}, err
	}
	f, err := compile(ix, q.Filter)
	if err != nil {
		return service.TimelinePage{}, err
	}
	order := store.ByTime
	if q.Order == "latency" {
		order = store.ByLatency
		// Only transactions with a latency take part in a latency order.
		f.Kinds = []bool{true}
		f.Key += "|lat"
	}
	sel, err := ix.Select(ctx, f, order)
	if err != nil {
		return service.TimelinePage{}, err
	}
	total := sel.Len()
	if order == store.ByLatency {
		// Rows without a latency sort last (NoLatency); leave them out.
		for total > 0 && ixLatency(ix, sel.At(total-1, false)) == store.NoLatency {
			total--
		}
	}
	page := service.TimelinePage{Total: total, Rows: []service.Row{}}
	if total == 0 {
		return page, nil
	}
	off := min(q.Offset, total-1)
	limit := uint64(min(max(q.Limit, 0), MaxPage))
	page.Offset = off
	for k := off; k < min(total, off+limit); k++ {
		rank := k
		if q.Desc {
			rank = total - 1 - k
		}
		i := sel.At(rank, false)
		page.Rows = append(page.Rows, rowDTO(ix, i, ix.Row(i)))
	}
	return page, nil
}

func ixLatency(ix *store.Index, i uint32) uint32 { return ix.Row(i).LatUS }

// compile maps the user's filter onto the index: names to dictionary
// entries, codes to bitmaps.
func compile(ix *store.Index, f service.Filter) (*store.Filter, error) {
	key, _ := json.Marshal(f)
	out := &store.Filter{Key: string(key)}
	bad := func(format string, args ...any) error {
		return service.Errorf(service.CodeInvalidArgument, format, args...)
	}
	if len(f.Servers) > 0 || len(f.Units) > 0 {
		out.Devices = make([]bool, len(ix.Devices))
		for i, d := range ix.Devices {
			server, whole := deviceName(d)
			if server == "" {
				continue
			}
			if len(f.Servers) > 0 && !matchServer(f.Servers, server) {
				continue
			}
			if len(f.Units) > 0 && (whole || !slices.Contains(f.Units, d.Unit)) {
				continue
			}
			out.Devices[i] = true
		}
	}
	if len(f.Clients) > 0 {
		out.Clients = make([]bool, len(ix.Clients))
		for i, c := range ix.Clients {
			out.Clients[i] = c != "" && slices.Contains(f.Clients, c)
		}
	}
	if len(f.Conns) > 0 {
		out.Conns = map[uint32]bool{}
		for _, c := range f.Conns {
			out.Conns[c] = true
		}
	}
	if len(f.Functions) > 0 {
		out.Functions = make([]bool, 256)
		for _, fc := range f.Functions {
			out.Functions[fc] = true
		}
	}
	codes := func(names []string, table []string, what string) ([]bool, error) {
		if len(names) == 0 {
			return nil, nil
		}
		m := make([]bool, len(table))
		for _, n := range names {
			c := store.Code(table, n)
			if c == 0 {
				return nil, bad("unknown %s %q", what, n)
			}
			m[c] = true
		}
		return m, nil
	}
	var err error
	if out.Statuses, err = codes(f.Statuses, store.StatusNames[:], "status"); err != nil {
		return nil, err
	}
	if out.Classes, err = codes(f.Classes, store.ClassNames[:], "class"); err != nil {
		return nil, err
	}
	for _, list := range [][]string{f.Flags, f.NotFlags} {
		for _, n := range list {
			if !slices.Contains(store.FlagNames[:], n) {
				return nil, bad("unknown flag %q", n)
			}
		}
	}
	out.FlagsAll, out.FlagsNone = store.Flags(f.Flags), store.Flags(f.NotFlags)
	out.FromNS, out.ToNS = f.FromNS, f.ToNS
	if len(f.Exceptions) > 0 {
		out.Exc = make([]bool, 256)
		for _, c := range f.Exceptions {
			out.Exc[c] = true
		}
	}
	if f.Space != "" {
		out.Space = store.Code(store.SpaceNames[:], f.Space)
		if out.Space == 0 {
			return nil, bad("unknown data space %q", f.Space)
		}
	}
	if f.Start != nil || f.End != nil {
		out.Addr, out.AddrFrom, out.AddrTo = true, 0, 0xFFFF
		if f.Start != nil {
			out.AddrFrom = *f.Start
		}
		if f.End != nil {
			out.AddrTo = *f.End
		}
		if out.AddrFrom > out.AddrTo {
			return nil, bad("address range %d-%d is empty", out.AddrFrom, out.AddrTo)
		}
	}
	return out, nil
}

// matchServer reports whether server ("ip:port") is named by one of the
// terms ("ip" or "ip:port").
func matchServer(terms []string, server string) bool {
	ap, err := netip.ParseAddrPort(server)
	if err != nil {
		return false
	}
	for _, s := range terms {
		if s == server {
			return true
		}
		if a, err := netip.ParseAddr(s); err == nil && a == ap.Addr() {
			return true
		}
	}
	return false
}

// rowDTO renders row i for the timeline.
func rowDTO(ix *store.Index, i uint32, r store.Row) service.Row {
	out := service.Row{Seq: uint64(i) + 1, Kind: store.KindNames[r.Kind], TimeNS: r.TimeNS, Conn: r.Conn, Flags: store.FlagList(r.Flags)}
	if int(r.Client) < len(ix.Clients) {
		out.Client = ix.Clients[r.Client]
	}
	if int(r.Device) < len(ix.Devices) {
		out.Server, _ = deviceName(ix.Devices[r.Device])
	}
	if r.Kind != store.KindTxn {
		out.Summary = eventText(r.Kind, ix.Extra(r))
		return out
	}
	unit, tid, fc := ix.Devices[r.Device].Unit, r.TID, r.FC
	out.Unit, out.TID, out.FC = &unit, &tid, &fc
	out.Class, out.Status, out.Exception = store.ClassNames[r.Class], store.StatusNames[r.Status], r.Exc
	if r.LatUS != store.NoLatency {
		ns := int64(r.LatUS) * 1000
		out.LatencyNS = &ns
	}
	out.Summary = ix.SummaryOf(r)
	if out.Summary == "" {
		out.Summary = fmt.Sprintf("Function %d", r.FC)
	}
	out.Result = resultText(r)
	return out
}

// resultText is the Result column (UX.md section 3).
func resultText(r store.Row) string {
	switch store.StatusNames[r.Status] {
	case model.StatusOK:
		switch {
		case store.ClassNames[r.Class] == "read" && (r.FC == 1 || r.FC == 2) && r.Count > 0:
			return strconv.Itoa(int(r.Count)) + " bits"
		case store.ClassNames[r.Class] == "read" && r.Count > 0 && r.FC != 24:
			return strconv.Itoa(int(r.Count)) + " words"
		}
		return "OK"
	case model.StatusException:
		return fmt.Sprintf("Exception %02X %s", r.Exc, exceptionName(r.Exc))
	case model.StatusUnanswered:
		return "No response observed"
	case model.StatusOrphan:
		return "Response without request"
	case model.StatusAmbiguous:
		return "Ambiguous"
	case model.StatusMalformed:
		return "Malformed"
	case model.StatusUncertain:
		return "Uncertain (gap)"
	}
	return ""
}

func exceptionName(c uint8) string {
	names := map[uint8]string{1: "Illegal function", 2: "Illegal data address", 3: "Illegal data value", 4: "Server device failure",
		5: "Acknowledge", 6: "Server device busy", 8: "Memory parity error", 0x0A: "Gateway path unavailable",
		0x0B: "Gateway target device failed to respond"}
	if n, ok := names[c]; ok {
		return n
	}
	return "Unknown exception"
}

// eventText describes a row that is not a transaction (UX.md section 3).
func eventText(kind uint8, extra json.RawMessage) string {
	switch kind {
	case store.KindConnOpen, store.KindConnClose:
		var c model.Connection
		_ = json.Unmarshal(extra, &c)
		if kind == store.KindConnOpen {
			if c.Open == "midstream" {
				return "Connection seen mid-stream"
			}
			return "Connection opened (SYN)"
		}
		how := map[string]string{"fin": "FIN", "rst": "RST", "end_of_trace": "end of trace", "evicted": "evicted",
			"superseded": "a new connection on the same ports"}[c.Close]
		if how == "" {
			how = c.Close
		}
		return fmt.Sprintf("Connection closed (%s) after %d transactions", how, c.Transactions)
	case store.KindGap:
		var g model.Gap
		_ = json.Unmarshal(extra, &g)
		dir := map[string]string{"c2s": "client to server", "s2c": "server to client"}[g.Dir]
		return fmt.Sprintf("Gap: %d bytes missing (%s, %s)", g.Bytes, dir, strings.ReplaceAll(g.Reason, "_", " "))
	case store.KindResync:
		var r model.Resync
		_ = json.Unmarshal(extra, &r)
		return fmt.Sprintf("Framing recovered after %d bytes (confidence: %s)", r.Skipped, strings.ReplaceAll(r.Confidence, "_", " "))
	case store.KindNote:
		var n model.Note
		_ = json.Unmarshal(extra, &n)
		return n.Text
	}
	return ""
}
