// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
	"unsafe"

	"github.com/otfabric/go-modbus-trace/model"
)

// The columns are read in place as the platform's integers; every target
// of this application is little-endian, as the files are.
func init() {
	var x uint16 = 1
	if *(*byte)(unsafe.Pointer(&x)) != 1 {
		panic("store: a big-endian platform cannot read the index in place")
	}
}

// mapped is the set of column files of a directory, mapped.
type mapped struct {
	n     uint64
	cols  map[string][]byte
	unmap []func() error
}

func mapColumns(dir string, n uint64) (*mapped, error) {
	m := &mapped{n: n, cols: map[string][]byte{}}
	for _, c := range columns {
		b, un, err := mapFile(filepath.Join(dir, "col", c.name+".bin"))
		if err != nil {
			m.close()
			return nil, err
		}
		m.unmap = append(m.unmap, un)
		if uint64(len(b)) != n*uint64(c.width) {
			m.close()
			return nil, fmt.Errorf("%w: column %s has %d bytes for %d rows", ErrFormat, c.name, len(b), n)
		}
		m.cols[c.name] = b
	}
	return m, nil
}

func (m *mapped) close() {
	for _, un := range m.unmap {
		_ = un()
	}
	m.unmap = nil
}

func view[T any](b []byte, n uint64) []T {
	if n == 0 || len(b) == 0 {
		return nil
	}
	return unsafe.Slice((*T)(unsafe.Pointer(&b[0])), int(n))
}

func (m *mapped) int64s(name string) []int64   { return view[int64](m.cols[name], m.n) }
func (m *mapped) uint32s(name string) []uint32 { return view[uint32](m.cols[name], m.n) }
func (m *mapped) uint16s(name string) []uint16 { return view[uint16](m.cols[name], m.n) }
func (m *mapped) uint8s(name string) []uint8   { return view[uint8](m.cols[name], m.n) }

// Index is an open, complete index directory. It is safe for concurrent
// use.
type Index struct {
	Meta      Meta
	Devices   []Device
	Clients   []string
	Summaries []string
	extra     []json.RawMessage
	dir       string

	cols  *mapped
	refs  []byte
	perms [2][]uint32 // by time, by latency
	unmap []func() error

	// typed views of the columns a filter reads
	kind, fc, class, status, space, mbcap, exc []uint8
	device, client, start, count               []uint16
	conn, flags, lat, summary                  []uint32
	t                                          []int64

	mu    sync.Mutex
	cache []cached // most recently used first

	// life keeps the mapping alive while queries read it: each holds a
	// read lock (Acquire, Release); Close takes the write lock, so it waits
	// for the queries in flight and none starts on an unmapped index.
	life   sync.RWMutex
	closed bool
}

// Acquire keeps the index mapped until Release; it reports false when the
// index is closed (the caller must not use it then).
func (ix *Index) Acquire() bool {
	ix.life.RLock()
	if ix.closed {
		ix.life.RUnlock()
		return false
	}
	return true
}

// Release ends what Acquire started.
func (ix *Index) Release() { ix.life.RUnlock() }

// Open opens a complete index directory of this format version.
func Open(dir string) (*Index, error) {
	ix := &Index{dir: dir}
	if err := readJSON(filepath.Join(dir, "meta.json"), &ix.Meta); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	if !ix.Meta.Complete || ix.Meta.Format != FormatVersion {
		return nil, ErrFormat
	}
	for name, v := range map[string]any{"devices.json": &ix.Devices, "clients.json": &ix.Clients,
		"summaries.json": &ix.Summaries, "extra.json": &ix.extra} {
		if err := readJSON(filepath.Join(dir, name), v); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrFormat, err)
		}
	}
	cols, err := mapColumns(dir, ix.Meta.Rows)
	if err != nil {
		return nil, err
	}
	ix.cols = cols
	for i, name := range []string{"time.bin", "latency.bin"} {
		b, un, err := mapFile(filepath.Join(dir, "perm", name))
		if err != nil {
			_ = ix.Close()
			return nil, err
		}
		ix.unmap = append(ix.unmap, un)
		if uint64(len(b)) != 4*ix.Meta.Rows {
			_ = ix.Close()
			return nil, fmt.Errorf("%w: permutation %s", ErrFormat, name)
		}
		ix.perms[i] = view[uint32](b, ix.Meta.Rows)
	}
	b, un, err := mapFile(filepath.Join(dir, "refs.bin"))
	if err != nil {
		_ = ix.Close()
		return nil, err
	}
	ix.unmap = append(ix.unmap, un)
	ix.refs = b
	ix.kind, ix.fc, ix.class, ix.status, ix.space, ix.mbcap = cols.uint8s("kind"), cols.uint8s("fc"), cols.uint8s("class"),
		cols.uint8s("status"), cols.uint8s("space"), cols.uint8s("mbcap")
	ix.exc = cols.uint8s("exc")
	ix.device, ix.client, ix.start, ix.count = cols.uint16s("device"), cols.uint16s("client"), cols.uint16s("start"), cols.uint16s("count")
	ix.conn, ix.flags, ix.lat, ix.summary = cols.uint32s("conn"), cols.uint32s("flags"), cols.uint32s("lat_us"), cols.uint32s("summary")
	ix.t = cols.int64s("t_ns")
	return ix, nil
}

// Close unmaps the files once no query holds the index (Acquire); later
// Acquires fail. Closing twice is harmless.
func (ix *Index) Close() error {
	ix.life.Lock()
	defer ix.life.Unlock()
	if ix.closed {
		return nil
	}
	ix.closed = true
	if ix.cols != nil {
		ix.cols.close()
		ix.cols = nil
	}
	var err error
	for _, un := range ix.unmap {
		err = errors.Join(err, un())
	}
	ix.unmap = nil
	return err
}

// Columns are the index's columns as slices indexed by row, for scans
// that aggregate a selection (the views). They are memory-mapped and
// read-only: writing to them faults.
type Columns struct {
	Kind, FC, Class, Status, Exc, Space  []uint8
	MBCAP                                []uint8
	Device, Client, Start, Count, WStart []uint16
	WCount, Bytes, RespHash              []uint16
	Conn, Flags, Lat, Summary            []uint32
	T                                    []int64
}

// Columns returns the column slices.
func (ix *Index) Columns() Columns {
	c := ix.cols
	return Columns{
		Kind: c.uint8s("kind"), FC: c.uint8s("fc"), Class: c.uint8s("class"), Status: c.uint8s("status"), Exc: c.uint8s("exc"),
		Space: c.uint8s("space"), MBCAP: c.uint8s("mbcap"), Device: c.uint16s("device"), Client: c.uint16s("client"), Start: c.uint16s("start"),
		Count: c.uint16s("count"), WStart: c.uint16s("wstart"), WCount: c.uint16s("wcount"), Bytes: c.uint16s("bytes"),
		RespHash: c.uint16s("resp_hash"), Conn: c.uint32s("conn"), Flags: c.uint32s("flags"), Lat: c.uint32s("lat_us"),
		Summary: c.uint32s("summary"), T: c.int64s("t_ns"),
	}
}

// Dir returns the index directory.
func (ix *Index) Dir() string { return ix.dir }

// Rows returns the number of rows.
func (ix *Index) Rows() uint64 { return ix.Meta.Rows }

// Row reads row i.
func (ix *Index) Row(i uint32) Row {
	var r Row
	for _, c := range columns {
		off := int(i) * c.width
		c.get(ix.cols.cols[c.name][off:off+c.width], &r)
	}
	return r
}

// Extra returns the JSON kept for a row that is not a transaction.
func (ix *Index) Extra(r Row) json.RawMessage {
	if r.Kind == KindTxn || int(r.Extra) >= len(ix.extra) {
		return nil
	}
	return ix.extra[r.Extra]
}

// SummaryOf returns the summary text of a row ("" when the dictionary was
// full at import).
func (ix *Index) SummaryOf(r Row) string {
	if r.Kind == KindTxn && int(r.Summary) < len(ix.Summaries) {
		return ix.Summaries[r.Summary]
	}
	return ""
}

// Refs decodes the provenance of a row's request and response.
func (ix *Index) Refs(r Row) (req, resp []model.FrameRef, err error) {
	if r.Refs > uint64(len(ix.refs)) {
		return nil, nil, fmt.Errorf("%w: references out of range", ErrFormat)
	}
	b := ix.refs[r.Refs:]
	var prev refBase
	req, b, err = decodeRefs(b, r.TimeNS, &prev)
	if err != nil {
		return nil, nil, err
	}
	resp, _, err = decodeRefs(b, r.TimeNS, &prev)
	return req, resp, err
}

func decodeRefs(b []byte, rowNS int64, prev *refBase) ([]model.FrameRef, []byte, error) {
	bad := fmt.Errorf("%w: damaged references", ErrFormat)
	uv := func() uint64 {
		v, n := binary.Uvarint(b)
		if n <= 0 {
			b = nil
			return 0
		}
		b = b[n:]
		return v
	}
	sv := func() int64 {
		v, n := binary.Varint(b)
		if n <= 0 {
			b = nil
			return 0
		}
		b = b[n:]
		return v
	}
	n := uv()
	if b == nil || n > 1<<16 {
		return nil, nil, bad
	}
	out := make([]model.FrameRef, 0, n)
	for range n {
		var f model.FrameRef
		f.Part = uint16(uv())
		f.Frame = prev.frame + uint64(sv())
		f.Offset = prev.offset + sv()
		prev.frame, prev.offset = f.Frame, f.Offset
		f.Iface = uint32(uv())
		if t := uv(); t&1 == 1 {
			z := t >> 1
			d := int64(z>>1) ^ -int64(z&1)
			tt := time.Unix(0, rowNS+d).UTC()
			f.Time = &tt
		}
		f.PayOff = int32(sv())
		f.PayLen = int32(sv())
		if b == nil {
			return nil, nil, bad
		}
		out = append(out, f)
	}
	return out, b, nil
}

// Order is a row order.
type Order int

// Orders.
const (
	ByTime Order = iota
	ByLatency
)

// Filter selects rows. Nil and empty fields do not narrow; a row must pass
// every field that narrows. Transaction terms (functions, statuses,
// classes, flags, space and addresses) pass only transactions.
type Filter struct {
	Devices   []bool // indexed by device dictionary index
	Clients   []bool // indexed by client dictionary index
	Conns     map[uint32]bool
	Kinds     []bool  // indexed by kind
	Functions []bool  // indexed by function code (256)
	Statuses  []bool  // indexed by status code
	Classes   []bool  // indexed by class code
	Exc       []bool  // indexed by exception code (256)
	Summary   *uint32 // a transaction's summary dictionary index
	FlagsAll  uint32  // every one of these flags
	FlagsNone uint32  // none of these flags
	FromNS    *int64
	ToNS      *int64 // exclusive
	Space     uint8  // 0: any data space
	Addr      bool   // AddrFrom..AddrTo narrows: rows whose range overlaps
	AddrFrom  uint16
	AddrTo    uint16 // inclusive
	Key       string // identifies the filter for the selection cache
}

func (f *Filter) txnTerms() bool {
	return f.Functions != nil || f.Statuses != nil || f.Classes != nil || f.Exc != nil || f.Summary != nil || f.FlagsAll != 0 || f.FlagsNone != 0 ||
		f.Space != 0 || f.Addr
}

func (f *Filter) empty() bool {
	return f.Devices == nil && f.Clients == nil && f.Conns == nil && f.Kinds == nil && !f.txnTerms() && f.FromNS == nil && f.ToNS == nil
}

// pass reports whether row i passes f.
func (ix *Index) pass(f *Filter, i uint32) bool {
	if f.Devices != nil && (int(ix.device[i]) >= len(f.Devices) || !f.Devices[ix.device[i]]) {
		return false
	}
	if f.Clients != nil && (int(ix.client[i]) >= len(f.Clients) || !f.Clients[ix.client[i]]) {
		return false
	}
	if f.Conns != nil && !f.Conns[ix.conn[i]] {
		return false
	}
	if f.Kinds != nil && (int(ix.kind[i]) >= len(f.Kinds) || !f.Kinds[ix.kind[i]]) {
		return false
	}
	if f.FromNS != nil && ix.t[i] < *f.FromNS {
		return false
	}
	if f.ToNS != nil && ix.t[i] >= *f.ToNS {
		return false
	}
	if !f.txnTerms() {
		return true
	}
	if ix.kind[i] != KindTxn {
		return false
	}
	if f.Functions != nil && !f.Functions[ix.fc[i]] {
		return false
	}
	if f.Statuses != nil && (int(ix.status[i]) >= len(f.Statuses) || !f.Statuses[ix.status[i]]) {
		return false
	}
	if f.Classes != nil && (int(ix.class[i]) >= len(f.Classes) || !f.Classes[ix.class[i]]) {
		return false
	}
	if f.Summary != nil && ix.summary[i] != *f.Summary {
		return false
	}
	if f.Exc != nil && (ix.status[i] != excStatus || !f.Exc[ix.exc[i]]) {
		return false
	}
	if fl := ix.flags[i]; fl&f.FlagsAll != f.FlagsAll || fl&f.FlagsNone != 0 {
		return false
	}
	if f.Space != 0 && ix.space[i] != f.Space {
		return false
	}
	if f.Addr {
		if ix.count[i] == 0 {
			return false
		}
		lo, hi := uint32(ix.start[i]), uint32(ix.start[i])+uint32(ix.count[i])-1
		if hi < uint32(f.AddrFrom) || lo > uint32(f.AddrTo) {
			return false
		}
	}
	return true
}

// excStatus is the status code of an exception response.
var excStatus = Code(StatusNames[:], "exception")

// Selection is the rows that pass a filter, in an order. A nil Rows means
// every row, in the order's permutation.
type Selection struct {
	rows  []uint32
	perm  []uint32
	total uint64
}

// Len returns the number of rows in the selection.
func (s *Selection) Len() uint64 { return s.total }

// At returns the row index at rank k (0 <= k < Len), from the start or,
// with desc, from the end.
func (s *Selection) At(k uint64, desc bool) uint32 {
	if desc {
		k = s.total - 1 - k
	}
	if s.rows != nil {
		return s.rows[k]
	}
	return s.perm[k]
}

// Rank returns the rank of row i in the selection, if it is in it.
func (s *Selection) Rank(i uint32, ix *Index, order Order, desc bool) (uint64, bool) {
	list := s.rows
	if list == nil {
		list = s.perm
	}
	for k, j := range list { // a linear search: called for one row on a user action
		if j == i {
			r := uint64(k)
			if desc {
				r = s.total - 1 - r
			}
			return r, true
		}
	}
	return 0, false
}

type cached struct {
	key   string
	order Order
	sel   *Selection
}

// maxCached bounds the selection cache by count; each selection holds at
// most 4 bytes per row.
const maxCached = 6

// Select returns the rows that pass f in order, from the cache when the
// same filter and order were asked for recently. ctx cancels a scan.
func (ix *Index) Select(ctx context.Context, f *Filter, order Order) (*Selection, error) {
	perm := ix.perms[order]
	if f.empty() {
		return &Selection{perm: perm, total: uint64(len(perm))}, nil
	}
	ix.mu.Lock()
	for k, c := range ix.cache {
		if c.key == f.Key && c.order == order {
			ix.cache = append([]cached{c}, append(ix.cache[:k:k], ix.cache[k+1:]...)...)
			ix.mu.Unlock()
			return c.sel, nil
		}
	}
	ix.mu.Unlock()
	rows := make([]uint32, 0, 1024)
	for k, i := range perm {
		if k&0xFFFF == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if ix.pass(f, i) {
			rows = append(rows, i)
		}
	}
	sel := &Selection{rows: rows, total: uint64(len(rows))}
	if f.Key != "" {
		ix.mu.Lock()
		ix.cache = append([]cached{{f.Key, order, sel}}, ix.cache...)
		if len(ix.cache) > maxCached {
			ix.cache = ix.cache[:maxCached]
		}
		ix.mu.Unlock()
	}
	return sel, nil
}

// TimeRank returns the rank of the first row at or after t in a time-order
// selection (binary search), for "go to time".
func (ix *Index) TimeRank(s *Selection, t int64) uint64 {
	return uint64(sort.Search(int(s.total), func(k int) bool { return ix.t[s.At(uint64(k), false)] >= t }))
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
