// SPDX-License-Identifier: MIT

package store

import (
	"bufio"
	"cmp"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"

	"github.com/otfabric/go-modbus-trace/model"
)

// Directory and file permissions: the cache holds operational metadata.
const (
	DirMode  os.FileMode = 0o700
	FileMode os.FileMode = 0o600
)

// MaxSummaries bounds the summary dictionary; past it a row's summary is
// rebuilt from its columns when displayed (without values of writes).
const MaxSummaries = 1 << 20

// Device is one entry of the device dictionary: a server endpoint and a
// unit.
type Device struct {
	Server string `json:"server"`
	Unit   uint8  `json:"unit"`
}

// Writer writes an index directory. Append rows in the order the library
// determines them, then Finish; Abort removes a partial directory.
type Writer struct {
	dir    string
	files  []*os.File
	bufs   []*bufio.Writer
	row    []byte
	n      uint64
	refs   *bufio.Writer
	refsF  *os.File
	refOff uint64

	devices   []Device
	deviceIx  map[Device]uint16
	clients   []string
	clientIx  map[string]uint16
	summaries []string
	summaryIx map[string]uint32
	extra     []json.RawMessage
	err       error
}

// ErrTooMany is returned when a dictionary or the row count would exceed
// what the columns can address.
var ErrTooMany = errors.New("store: more devices, clients or rows than the index can hold")

// Create starts an index in dir, which must not exist yet.
func Create(dir string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(dir), DirMode); err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, DirMode); err != nil {
		return nil, err
	}
	for _, sub := range []string{"col", "perm"} {
		if err := os.Mkdir(filepath.Join(dir, sub), DirMode); err != nil {
			return nil, err
		}
	}
	w := &Writer{dir: dir, row: make([]byte, 8), deviceIx: map[Device]uint16{}, clientIx: map[string]uint16{},
		summaryIx: map[string]uint32{}}
	// Index 0 of the summary dictionary is "no summary".
	w.summaries = append(w.summaries, "")
	w.summaryIx[""] = 0
	for _, c := range columns {
		f, err := os.OpenFile(filepath.Join(dir, "col", c.name+".bin"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, FileMode)
		if err != nil {
			w.close()
			return nil, err
		}
		w.files = append(w.files, f)
		w.bufs = append(w.bufs, bufio.NewWriterSize(f, 64<<10))
	}
	f, err := os.OpenFile(filepath.Join(dir, "refs.bin"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, FileMode)
	if err != nil {
		w.close()
		return nil, err
	}
	w.refsF, w.refs = f, bufio.NewWriterSize(f, 256<<10)
	return w, nil
}

// Dir returns the directory being written.
func (w *Writer) Dir() string { return w.dir }

// Rows returns the number of rows appended so far.
func (w *Writer) Rows() uint64 { return w.n }

// Device returns the dictionary index of a server and unit.
func (w *Writer) Device(server string, unit uint8) (uint16, error) {
	k := Device{server, unit}
	if i, ok := w.deviceIx[k]; ok {
		return i, nil
	}
	if len(w.devices) >= math.MaxUint16 {
		return 0, ErrTooMany
	}
	i := uint16(len(w.devices))
	w.devices = append(w.devices, k)
	w.deviceIx[k] = i
	return i, nil
}

// Client returns the dictionary index of a client address.
func (w *Writer) Client(addr string) (uint16, error) {
	if i, ok := w.clientIx[addr]; ok {
		return i, nil
	}
	if len(w.clients) >= math.MaxUint16 {
		return 0, ErrTooMany
	}
	i := uint16(len(w.clients))
	w.clients = append(w.clients, addr)
	w.clientIx[addr] = i
	return i, nil
}

// Summary returns the dictionary index of a summary text; 0 when the
// dictionary is full.
func (w *Writer) Summary(s string) uint32 {
	if i, ok := w.summaryIx[s]; ok {
		return i
	}
	if len(w.summaries) >= MaxSummaries {
		return 0
	}
	i := uint32(len(w.summaries))
	w.summaries = append(w.summaries, s)
	w.summaryIx[s] = i
	return i
}

// Extra keeps a small JSON value for a row that is not a transaction and
// returns its index.
func (w *Writer) Extra(v any) (uint32, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	w.extra = append(w.extra, b)
	return uint32(len(w.extra) - 1), nil
}

// Append writes one row with the provenance of its request and response.
func (w *Writer) Append(r Row, req, resp []model.FrameRef) error {
	if w.err != nil {
		return w.err
	}
	if w.n >= math.MaxUint32 {
		return ErrTooMany
	}
	r.Refs = w.refOff
	var prev refBase
	w.writeRefs(req, r.TimeNS, &prev)
	w.writeRefs(resp, r.TimeNS, &prev)
	for i, c := range columns {
		c.put(w.row[:c.width], &r)
		if _, err := w.bufs[i].Write(w.row[:c.width]); err != nil {
			w.err = err
			return err
		}
	}
	w.n++
	return w.err
}

// refBase is what a row's references are encoded against: each frame and
// file offset as the difference to the previous reference of the row (the
// first against 0), each time as the difference to the row's time. A
// request and its response sit close in the file, so most numbers are a
// byte or two.
type refBase struct {
	frame  uint64
	offset int64
}

// writeRefs appends a list of frame references to refs.bin: a count, then
// per reference the part, the frame and offset deltas, the interface, the
// time (delta<<1|1, or 0 for none), the payload offset and length.
func (w *Writer) writeRefs(refs []model.FrameRef, rowNS int64, prev *refBase) {
	var b [binary.MaxVarintLen64]byte
	put := func(n int) {
		if w.err != nil {
			return
		}
		_, w.err = w.refs.Write(b[:n])
		w.refOff += uint64(n)
	}
	put(binary.PutUvarint(b[:], uint64(len(refs))))
	for _, f := range refs {
		put(binary.PutUvarint(b[:], uint64(f.Part)))
		put(binary.PutVarint(b[:], int64(f.Frame-prev.frame)))
		put(binary.PutVarint(b[:], f.Offset-prev.offset))
		prev.frame, prev.offset = f.Frame, f.Offset
		put(binary.PutUvarint(b[:], uint64(f.Iface)))
		var t uint64
		if f.Time != nil {
			d := f.Time.UnixNano() - rowNS
			t = uint64(d<<1^d>>63)<<1 | 1 // zigzag, then the "present" bit
		}
		put(binary.PutUvarint(b[:], t))
		put(binary.PutVarint(b[:], int64(f.PayOff)))
		put(binary.PutVarint(b[:], int64(f.PayLen)))
	}
}

// Meta is meta.json: what the index was built from and with.
type Meta struct {
	Format      int             `json:"format"`
	Library     string          `json:"library"`
	Semantics   int             `json:"event_semantics"`
	Fingerprint string          `json:"fingerprint"`
	Paths       []string        `json:"paths"`
	Rows        uint64          `json:"rows"`
	FirstNS     int64           `json:"first_ns"`
	LastNS      int64           `json:"last_ns"`
	Choices     json.RawMessage `json:"choices,omitempty"`
	Complete    bool            `json:"complete"`
	// Live: a snapshot of an index still being written (OpenLive), the
	// first Rows rows and RefsBytes bytes of references.
	Live      bool   `json:"live,omitempty"`
	RefsBytes uint64 `json:"refs_bytes,omitempty"`
}

// Finish flushes the columns, builds the time and latency permutations,
// writes the dictionaries and the given JSON documents (name -> value, for
// example "summary.json"), and writes meta.json last, marked complete. On
// an error the directory is left incomplete; call Abort.
func (w *Writer) Finish(meta Meta, docs map[string]any) error {
	if err := w.flush(); err != nil {
		return err
	}
	meta.Format, meta.Rows, meta.Complete = FormatVersion, w.n, false
	if err := w.permutations(w.dir, &meta, false); err != nil {
		return err
	}
	if err := w.writeDocs(w.dir, docs); err != nil {
		return err
	}
	meta.Complete = true
	return writeJSON(filepath.Join(w.dir, "meta.json"), meta)
}

// writeDocs writes the dictionaries and the given JSON documents into dir.
func (w *Writer) writeDocs(dir string, docs map[string]any) error {
	all := map[string]any{"devices.json": w.devices, "clients.json": w.clients, "summaries.json": w.summaries, "extra.json": w.extra}
	for k, v := range docs {
		all[k] = v
	}
	for name, v := range all {
		if err := writeJSON(filepath.Join(dir, name), v); err != nil {
			return err
		}
	}
	return nil
}

// Snapshot makes the rows appended so far readable while the writer goes
// on (OpenLive(w.Dir(), dir)): it flushes the buffered rows and writes into
// dir, which must not exist, the dictionaries, the time and latency
// permutations of those rows and a metadata file marked live. The column
// and reference files stay in the writer's directory and only grow.
func (w *Writer) Snapshot(dir string, meta Meta) error {
	if w.files == nil {
		return errors.New("store: a snapshot of a finished index")
	}
	meta.Format, meta.Rows, meta.Live, meta.RefsBytes = FormatVersion, w.n, true, w.refOff
	err := w.flushBuffers()
	if err == nil {
		err = os.Mkdir(dir, DirMode)
	}
	if err == nil {
		err = os.Mkdir(filepath.Join(dir, "perm"), DirMode)
	}
	if err == nil {
		err = w.permutations(dir, &meta, true)
	}
	if err == nil {
		err = w.writeDocs(dir, nil)
	}
	if err != nil {
		return err
	}
	meta.Complete = true
	return writeJSON(filepath.Join(dir, "meta.json"), meta)
}

// permutations writes the time and latency orders of the rows into
// dir/perm, sorting with the columns mapped (prefix: of files still being
// written), and sets the first and last time.
func (w *Writer) permutations(dir string, meta *Meta, prefix bool) error {
	cols, err := mapColumnsOf(w.dir, w.n, prefix)
	if err != nil {
		return err
	}
	defer cols.close()
	t, lat := cols.int64s("t_ns"), cols.uint32s("lat_us")
	if w.n > 0 {
		meta.FirstNS, meta.LastNS = slices.Min(t), slices.Max(t)
	}
	perm := make([]uint32, w.n)
	for i := range perm {
		perm[i] = uint32(i)
	}
	slices.SortFunc(perm, func(a, b uint32) int { return cmp.Or(cmp.Compare(t[a], t[b]), cmp.Compare(a, b)) })
	if err := writeU32s(filepath.Join(dir, "perm", "time.bin"), perm); err != nil {
		return err
	}
	for i := range perm {
		perm[i] = uint32(i)
	}
	slices.SortFunc(perm, func(a, b uint32) int { return cmp.Or(cmp.Compare(lat[a], lat[b]), cmp.Compare(a, b)) })
	return writeU32s(filepath.Join(dir, "perm", "latency.bin"), perm)
}

// Abort closes and removes the directory.
func (w *Writer) Abort() error {
	w.close()
	return os.RemoveAll(w.dir)
}

func (w *Writer) flush() error {
	if err := w.flushBuffers(); err != nil {
		return err
	}
	for _, f := range w.files {
		if err := f.Close(); err != nil {
			return err
		}
	}
	w.bufs, w.files = nil, nil
	err := w.refsF.Close()
	w.refsF = nil
	return err
}

// flushBuffers writes the buffered rows and references to their files.
func (w *Writer) flushBuffers() error {
	if w.err != nil {
		return w.err
	}
	for _, b := range w.bufs {
		if err := b.Flush(); err != nil {
			return err
		}
	}
	return w.refs.Flush()
}

func (w *Writer) close() {
	for _, f := range w.files {
		_ = f.Close()
	}
	w.files, w.bufs = nil, nil
	if w.refsF != nil {
		_ = w.refsF.Close()
		w.refsF = nil
	}
}

func writeU32s(path string, v []uint32) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, FileMode)
	if err != nil {
		return err
	}
	b := bufio.NewWriterSize(f, 256<<10)
	var x [4]byte
	for _, n := range v {
		le.PutUint32(x[:], n)
		if _, err := b.Write(x[:]); err != nil {
			_ = f.Close()
			return err
		}
	}
	if err := b.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("store: %s: %w", filepath.Base(path), err)
	}
	return os.WriteFile(path, b, FileMode)
}
