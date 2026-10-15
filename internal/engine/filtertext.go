// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// functionWords are the names a filter term or a bare word may use for
// function codes (UX.md section 6).
var functionWords = map[string][]uint8{
	"coils": {1}, "coil": {1}, "discrete": {2}, "holding": {3}, "input": {4},
	"write_coil": {5}, "write_register": {6}, "write_coils": {15}, "write_registers": {16},
	"diagnostics": {8}, "identification": {43}, "fifo": {24}, "file": {20, 21}, "mask": {22}, "readwrite": {23},
	"exception_status": {7}, "comm_counter": {11}, "comm_log": {12}, "server_id": {17},
}

var statusWords = map[string]string{"orphan": "orphan_response", "ambiguous": "ambiguous", "ok": "ok", "exception": "exception",
	"unanswered": "unanswered", "malformed": "malformed", "uncertain": "uncertain", "orphan_response": "orphan_response"}

// ParseFilter parses the filter box: space-separated terms that must all
// match, commas for alternatives, a leading "-" to negate a flag. A term
// that does not parse is returned as an error, never ignored.
func (e *Engine) ParseFilter(_ context.Context, id, text string) (service.FilterParse, error) {
	var first int64
	if _, ix, err := e.index(id); err == nil {
		first = ix.Meta.FirstNS
	}
	return parseFilter(text, first), nil
}

func parseFilter(text string, firstNS int64) service.FilterParse {
	out := service.FilterParse{Errors: []service.TermError{}}
	f := &out.Filter
	fail := func(term, msg string) { out.Errors = append(out.Errors, service.TermError{Term: term, Message: msg}) }
	for _, term := range strings.Fields(text) {
		neg := strings.HasPrefix(term, "-")
		body := strings.TrimPrefix(term, "-")
		key, val, hasKey := strings.Cut(body, ":")
		key = strings.ToLower(key)
		if !hasKey {
			// A bare word: a function name or alias.
			if fcs, ok := functionWords[strings.ToLower(body)]; ok && !neg {
				f.Functions = append(f.Functions, fcs...)
				continue
			}
			fail(term, "not a filter term; use key:value, for example fc:3 or unit:1")
			continue
		}
		if neg && key != "flag" {
			fail(term, "only flag: can be negated")
			continue
		}
		vals := strings.Split(val, ",")
		if val == "" {
			fail(term, "no value")
			continue
		}
		switch key {
		case "server":
			for _, v := range vals {
				if _, err := netip.ParseAddr(v); err == nil {
					f.Servers = append(f.Servers, v)
				} else if _, err := netip.ParseAddrPort(v); err == nil {
					f.Servers = append(f.Servers, v)
				} else {
					fail(term, "a server is an address or address:port")
				}
			}
		case "client":
			for _, v := range vals {
				if a, err := netip.ParseAddr(v); err == nil {
					f.Clients = append(f.Clients, a.String())
				} else {
					fail(term, "a client is an address")
				}
			}
		case "unit":
			f.Units = append(f.Units, ints[uint8](term, vals, 255, fail)...)
		case "conn":
			f.Conns = append(f.Conns, ints[uint32](term, vals, 1<<32-1, fail)...)
		case "exc":
			f.Exceptions = append(f.Exceptions, ints[uint8](term, vals, 255, fail)...)
		case "fc":
			for _, v := range vals {
				lv := strings.ToLower(v)
				switch {
				case lv == "read" || lv == "write" || lv == "other":
					f.Classes = append(f.Classes, lv)
				case functionWords[lv] != nil:
					f.Functions = append(f.Functions, functionWords[lv]...)
				default:
					f.Functions = append(f.Functions, ints[uint8](term, []string{v}, 255, fail)...)
				}
			}
		case "class":
			for _, v := range vals {
				if v == "read" || v == "write" || v == "other" {
					f.Classes = append(f.Classes, v)
				} else {
					fail(term, "a class is read, write or other")
				}
			}
		case "status":
			for _, v := range vals {
				if s, ok := statusWords[strings.ToLower(v)]; ok {
					f.Statuses = append(f.Statuses, s)
				} else {
					fail(term, "a status is ok, exception, unanswered, orphan, ambiguous, malformed or uncertain")
				}
			}
		case "flag":
			for _, v := range vals {
				if !slices.Contains(store.FlagNames[:], v) {
					fail(term, "unknown flag "+strconv.Quote(v))
					continue
				}
				if neg {
					f.NotFlags = append(f.NotFlags, v)
				} else {
					f.Flags = append(f.Flags, v)
				}
			}
		case "addr":
			parseAddr(term, val, f, fail)
		case "from", "to":
			ns, ok := parseTime(val, firstNS)
			if !ok {
				fail(term, "a time is hh:mm[:ss] of the trace's day, an ISO time, or an offset like +15m from the start")
				continue
			}
			if key == "from" {
				f.FromNS = &ns
			} else {
				f.ToNS = &ns
			}
		default:
			fail(term, "unknown key "+strconv.Quote(key))
		}
	}
	return out
}

// ints parses numbers (decimal or 0x hex) and ranges a-b up to limit.
func ints[T uint8 | uint32](term string, vals []string, limit uint64, fail func(string, string)) []T {
	var out []T
	num := func(s string) (uint64, bool) {
		n, err := strconv.ParseUint(s, 0, 64)
		return n, err == nil && n <= limit
	}
	for _, v := range vals {
		lo, hi, isRange := strings.Cut(v, "-")
		a, ok1 := num(lo)
		b, ok2 := a, true
		if isRange {
			b, ok2 = num(hi)
		}
		if !ok1 || !ok2 || b < a || b-a > 4096 {
			fail(term, "not a number or range: "+strconv.Quote(v))
			continue
		}
		for n := a; n <= b; n++ {
			out = append(out, T(n))
		}
	}
	return out
}

// parseAddr reads "space/start-end", "space/addr", "start-end" or "addr".
func parseAddr(term, val string, f *service.Filter, fail func(string, string)) {
	space, rng, hasSpace := strings.Cut(val, "/")
	if !hasSpace {
		space, rng = "", val
	}
	if space != "" {
		names := map[string]string{"coil": "coil", "coils": "coil", "discrete": "discrete", "holding": "holding", "input": "input"}
		s, ok := names[strings.ToLower(space)]
		if !ok {
			fail(term, "a data space is coil, discrete, holding or input")
			return
		}
		f.Space = s
	}
	if rng == "" {
		return
	}
	lo, hi, isRange := strings.Cut(rng, "-")
	a, err1 := strconv.ParseUint(lo, 0, 16)
	b, err2 := a, error(nil)
	if isRange {
		b, err2 = strconv.ParseUint(hi, 0, 16)
	}
	if err1 != nil || err2 != nil || b < a {
		fail(term, "an address is 0-65535, a range is start-end")
		return
	}
	s, e := uint16(a), uint16(b)
	f.Start, f.End = &s, &e
}

// parseTime reads a clock time of the trace's day (local time), an ISO
// time, or an offset from the first packet ("+15m", "+1h30m").
func parseTime(s string, firstNS int64) (int64, bool) {
	if off, ok := strings.CutPrefix(s, "+"); ok {
		d, err := time.ParseDuration(off)
		if err != nil || d < 0 {
			return 0, false
		}
		return firstNS + int64(d), true
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.UnixNano(), true
		}
	}
	for _, layout := range []string{"15:04:05", "15:04"} {
		if c, err := time.Parse(layout, s); err == nil {
			day := time.Unix(0, firstNS).In(time.Local)
			t := time.Date(day.Year(), day.Month(), day.Day(), c.Hour(), c.Minute(), c.Second(), 0, time.Local)
			return t.UnixNano(), true
		}
	}
	return 0, false
}
