// SPDX-License-Identifier: MIT

// Package store is the analysis cache: one directory per trace holding the
// timeline as fixed-width column files, the dictionaries they refer to,
// the provenance of every row, and permutations by time and by latency
// (docs/ARCHITECTURE.md sections 5 and 6). It is written once by an import
// and then only read, memory-mapped, by queries that scan the columns a
// filter names into a selection vector and page it by rank.
//
// The cache is disposable: everything here can be rebuilt from the trace.
// It holds no packet bytes, only references into the trace.
package store

import (
	"encoding/binary"
	"errors"
	"math"
)

// FormatVersion changes whenever the layout of an index directory changes;
// a directory of another version is rebuilt.
const FormatVersion = 1

// ErrFormat is wrapped by the error Open returns for a directory that is
// not a complete index of this version.
var ErrFormat = errors.New("store: not a complete index of this version")

// Row kinds.
const (
	KindTxn uint8 = iota
	KindConnOpen
	KindConnClose
	KindGap
	KindResync
	KindNote
)

// KindNames are the kinds as the service names them.
var KindNames = [...]string{"txn", "conn_open", "conn_close", "gap", "resync", "note"}

// Statuses, classes and data spaces are stored as small codes; 0 means
// "none" (a row that is not a transaction, or a transaction without one).
var (
	StatusNames = [...]string{"", "ok", "exception", "unanswered", "orphan_response", "ambiguous", "malformed", "uncertain"}
	ClassNames  = [...]string{"", "read", "write", "other"}
	SpaceNames  = [...]string{"", "coil", "discrete", "holding", "input", "file", "fifo"}
)

// FlagNames are the quality flags, one bit each in the flags column, in
// bit order. A flag the library adds later and this list lacks sets
// FlagOther.
var FlagNames = [...]string{
	"retransmitted", "out_of_order", "gap_adjacent", "resynced", "snaplen_truncated", "midstream",
	"role_guessed", "role_unknown", "retried", "clock_order", "slow", "invalid_pdu",
	"serial_function_over_tcp", "unknown_function", "broadcast", "foreign_protocol",
}

// FlagOther marks a flag that is not in FlagNames.
const FlagOther uint32 = 1 << 31

// None values of the optional numeric columns.
const (
	NoSub     = math.MaxUint16
	NoLatency = math.MaxUint32
)

// Code returns the index of name in names (0 when it is not there).
func Code[T ~string](names []string, name T) uint8 {
	for i, n := range names {
		if n == string(name) {
			return uint8(i)
		}
	}
	return 0
}

// Flags packs flag names into the bitmask.
func Flags(names []string) uint32 {
	var m uint32
	for _, n := range names {
		found := false
		for i, f := range FlagNames {
			if f == n {
				m |= 1 << i
				found = true
				break
			}
		}
		if !found {
			m |= FlagOther
		}
	}
	return m
}

// FlagList unpacks the bitmask.
func FlagList(m uint32) []string {
	out := []string{}
	for i, f := range FlagNames {
		if m&(1<<i) != 0 {
			out = append(out, f)
		}
	}
	if m&FlagOther != 0 {
		out = append(out, "other")
	}
	return out
}

// Row is one timeline row as the import writes it and a query reads it.
// Strings live in dictionaries (Device, Client, Summary); the provenance
// in the refs file.
type Row struct {
	Kind    uint8
	TimeNS  int64
	Conn    uint32
	Device  uint16 // index into the device dictionary (server endpoint and unit)
	Client  uint16 // index into the client dictionary (address)
	TID     uint16
	FC      uint8
	Sub     uint16 // NoSub when absent
	Class   uint8
	Status  uint8
	Exc     uint8
	Flags   uint32
	Space   uint8 // the first range's data space
	Start   uint16
	Count   uint16
	WStart  uint16 // the write range of FC23, else 0
	WCount  uint16
	LatUS   uint32 // NoLatency when absent
	MBCAP   uint8  // the export classification (mbcapexport reason index); 0 = exported
	Summary uint32 // a transaction's index into the summary dictionary
	Extra   uint32 // another row's index into its extra data (stored in the summary column)
	Refs    uint64 // offset of the row's provenance in refs.bin
}

// column describes one column file: its name, its width and how a Row's
// field is put into and read from it.
type column struct {
	name  string
	width int
	put   func(b []byte, r *Row)
	get   func(b []byte, r *Row)
}

var le = binary.LittleEndian

// columns is the layout of an index directory's col/ files.
var columns = []column{
	{"kind", 1, func(b []byte, r *Row) { b[0] = r.Kind }, func(b []byte, r *Row) { r.Kind = b[0] }},
	{"t_ns", 8, func(b []byte, r *Row) { le.PutUint64(b, uint64(r.TimeNS)) }, func(b []byte, r *Row) { r.TimeNS = int64(le.Uint64(b)) }},
	{"conn", 4, func(b []byte, r *Row) { le.PutUint32(b, r.Conn) }, func(b []byte, r *Row) { r.Conn = le.Uint32(b) }},
	{"device", 2, func(b []byte, r *Row) { le.PutUint16(b, r.Device) }, func(b []byte, r *Row) { r.Device = le.Uint16(b) }},
	{"client", 2, func(b []byte, r *Row) { le.PutUint16(b, r.Client) }, func(b []byte, r *Row) { r.Client = le.Uint16(b) }},
	{"tid", 2, func(b []byte, r *Row) { le.PutUint16(b, r.TID) }, func(b []byte, r *Row) { r.TID = le.Uint16(b) }},
	{"fc", 1, func(b []byte, r *Row) { b[0] = r.FC }, func(b []byte, r *Row) { r.FC = b[0] }},
	{"sub", 2, func(b []byte, r *Row) { le.PutUint16(b, r.Sub) }, func(b []byte, r *Row) { r.Sub = le.Uint16(b) }},
	{"class", 1, func(b []byte, r *Row) { b[0] = r.Class }, func(b []byte, r *Row) { r.Class = b[0] }},
	{"status", 1, func(b []byte, r *Row) { b[0] = r.Status }, func(b []byte, r *Row) { r.Status = b[0] }},
	{"exc", 1, func(b []byte, r *Row) { b[0] = r.Exc }, func(b []byte, r *Row) { r.Exc = b[0] }},
	{"flags", 4, func(b []byte, r *Row) { le.PutUint32(b, r.Flags) }, func(b []byte, r *Row) { r.Flags = le.Uint32(b) }},
	{"space", 1, func(b []byte, r *Row) { b[0] = r.Space }, func(b []byte, r *Row) { r.Space = b[0] }},
	{"start", 2, func(b []byte, r *Row) { le.PutUint16(b, r.Start) }, func(b []byte, r *Row) { r.Start = le.Uint16(b) }},
	{"count", 2, func(b []byte, r *Row) { le.PutUint16(b, r.Count) }, func(b []byte, r *Row) { r.Count = le.Uint16(b) }},
	{"wstart", 2, func(b []byte, r *Row) { le.PutUint16(b, r.WStart) }, func(b []byte, r *Row) { r.WStart = le.Uint16(b) }},
	{"wcount", 2, func(b []byte, r *Row) { le.PutUint16(b, r.WCount) }, func(b []byte, r *Row) { r.WCount = le.Uint16(b) }},
	{"lat_us", 4, func(b []byte, r *Row) { le.PutUint32(b, r.LatUS) }, func(b []byte, r *Row) { r.LatUS = le.Uint32(b) }},
	{"mbcap", 1, func(b []byte, r *Row) { b[0] = r.MBCAP }, func(b []byte, r *Row) { r.MBCAP = b[0] }},
	// A transaction's summary, or another row's extra data: one column, by kind
	// (the kind column is read first).
	{"summary", 4, func(b []byte, r *Row) {
		v := r.Summary
		if r.Kind != KindTxn {
			v = r.Extra
		}
		le.PutUint32(b, v)
	}, func(b []byte, r *Row) {
		if r.Kind == KindTxn {
			r.Summary, r.Extra = le.Uint32(b), 0
		} else {
			r.Summary, r.Extra = 0, le.Uint32(b)
		}
	}},
	{"refs", 8, func(b []byte, r *Row) { le.PutUint64(b, r.Refs) }, func(b []byte, r *Row) { r.Refs = le.Uint64(b) }},
}

// RowBytes is the size of one row over all columns.
func RowBytes() int {
	n := 0
	for _, c := range columns {
		n += c.width
	}
	return n
}
