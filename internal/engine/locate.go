// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/otfabric/go-modbus-trace/trace"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
	"github.com/otfabric/modbus-trace-explorer/internal/store"
)

// Locate finds one row of the filtered timeline (R5, UX.md section 9).
func (e *Engine) Locate(ctx context.Context, id string, q service.LocateQuery) (service.Location, error) {
	_, ix, err := e.index(id)
	if err != nil {
		return service.Location{}, err
	}
	n := 0
	for _, set := range []bool{q.TimeNS != nil, q.Frame != nil, q.Seq != nil} {
		if set {
			n++
		}
	}
	if n != 1 {
		return service.Location{}, service.Errorf(service.CodeInvalidArgument, "locate needs exactly one of a time, a frame or a row")
	}
	f, err := compile(ix, q.Filter)
	if err != nil {
		return service.Location{}, err
	}
	order := store.ByTime
	if q.Seq != nil && q.Order == "latency" {
		order = store.ByLatency
		f.Kinds = []bool{true}
		f.Key += "|lat"
	}
	sel, err := ix.Select(ctx, f, order)
	if err != nil {
		return service.Location{}, err
	}
	total := sel.Len()
	if total == 0 {
		return service.Location{Note: "No rows match the filter."}, nil
	}
	// rank k of the time (or latency) order, as the query's direction shows it
	at := func(k uint64, exact bool) service.Location {
		r := k
		if q.Desc {
			r = total - 1 - k
		}
		return service.Location{Found: true, Rank: r, Seq: uint64(sel.At(k, false)) + 1, Exact: exact}
	}
	c := ix.Columns()
	switch {
	case q.TimeNS != nil:
		k := ix.TimeRank(sel, *q.TimeNS)
		if k >= total {
			loc := at(total-1, false)
			loc.Note = "The trace ends before that time: the last row."
			return loc, nil
		}
		return at(k, c.T[sel.At(k, false)] == *q.TimeNS), nil
	case q.Frame != nil:
		// Frame numbers grow with the file; the first row with a frame at
		// or after the one asked is the nearest when none carries it.
		best, bestFrame := uint64(0), uint64(0)
		found := false
		for k := range total {
			if k&0xFFFF == 0 && ctx.Err() != nil {
				return service.Location{}, ctx.Err()
			}
			req, resp, err := ix.Refs(ix.Row(sel.At(k, false)))
			if err != nil {
				return service.Location{}, service.Wrap(service.CodeInternal, err, "the analysis cache is damaged")
			}
			for _, r := range append(req, resp...) {
				if int(r.Part) != q.Part {
					continue
				}
				if r.Frame == *q.Frame {
					return at(k, true), nil
				}
				if r.Frame > *q.Frame && (!found || r.Frame < bestFrame) {
					best, bestFrame, found = k, r.Frame, true
				}
			}
		}
		if !found {
			return service.Location{Note: fmt.Sprintf("No row of the filter carries frame %d or a later one.", *q.Frame)}, nil
		}
		loc := at(best, false)
		loc.Note = fmt.Sprintf("No row carries frame %d; frame %d is the next one that does.", *q.Frame, bestFrame)
		return loc, nil
	default:
		if *q.Seq == 0 || *q.Seq > ix.Rows() {
			return service.Location{}, service.Errorf(service.CodeNotFound, "no row %d", *q.Seq)
		}
		i := uint32(*q.Seq - 1)
		rank, ok := sel.Rank(i, ix, order, q.Desc)
		if !ok {
			return service.Location{Note: "The row is not in the filter."}, nil
		}
		step := int64(1)
		if q.Step < 0 {
			step = -1
		}
		for r := int64(rank) + step; r >= 0 && r < int64(total); r += step {
			k := uint64(r)
			if q.Desc {
				k = total - 1 - k
			}
			if j := sel.At(k, false); c.Conn[j] == c.Conn[i] {
				return service.Location{Found: true, Rank: uint64(r), Seq: uint64(j) + 1, Exact: true}, nil
			}
		}
		return service.Location{Note: "No further row of this connection."}, nil
	}
}

// Diagnostics is the text of "Copy diagnostics" (R23): no paths, no
// addresses, no trace content.
func (e *Engine) Diagnostics(ctx context.Context, id string) (string, error) {
	ab, _ := e.About(ctx)
	var b strings.Builder
	fmt.Fprintf(&b, "Modbus Trace Explorer %s\ngo-modbus-trace %s\nWails %s\n%s %s/%s, %d CPUs\n", ab.Version, ab.GoModbusTrace, ab.Wails,
		ab.Go, ab.OS, ab.Arch, runtime.NumCPU())
	ci := e.cache.Info()
	fmt.Fprintf(&b, "cache: %d entries, %d MiB of %d MiB\n", ci.Entries, ci.Bytes>>20, ci.Cap>>20)
	if id == "" {
		return b.String(), nil
	}
	t, err := e.trace(id)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	info, sum, ix := t.info, t.sum, t.ix
	t.mu.Unlock()
	fmt.Fprintf(&b, "trace: %d files, %d MiB, gzip %v, analysed %v\n", len(info.Paths), info.Size>>20, info.Gzip, info.Analyzed)
	if sum != nil {
		links := map[string]bool{}
		for _, f := range sum.Interfaces {
			links[f.Link] = true
		}
		names := make([]string, 0, len(links))
		for l := range links {
			names = append(names, l)
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "format %s, %d packets, link types %s, connections %d (midstream %d)\n", sum.Format, sum.Packets,
			strings.Join(names, ","), sum.Connections.Total, sum.Connections.Midstream)
		fmt.Fprintf(&b, "quality: gaps %d, resyncs %d, retransmitted %d, snaplen truncated %d\n", sum.Quality.Gaps, sum.Quality.Resyncs,
			sum.Quality.Retransmitted, sum.Quality.SnaplenTruncated)
		if sum.Error != "" {
			b.WriteString("read error: yes\n")
		}
	}
	if ix != nil {
		fmt.Fprintf(&b, "index: %d rows, format %d, library %s\n", ix.Rows(), store.FormatVersion, ix.Meta.Library)
	}
	return b.String(), nil
}

// errStop ends a Walk after the first timestamped packet.
var errStop = errors.New("stop")

// firstTime is the time of a file's first timestamped packet; ok is false
// when it has none or cannot be read.
func firstTime(ctx context.Context, path string) (int64, bool) {
	f, err := trace.Open(path)
	if err != nil {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var t int64
	ok := false
	err = trace.Walk(ctx, f, trace.ScanOptions{}, func(p *trace.RawPacket) error {
		if p.HasTime {
			t, ok = p.TimeNS, true
			return errStop
		}
		return nil
	})
	_ = err // a damaged file without a time keeps its place
	return t, ok
}

// byFirstPacket orders the files of one trace by their first packet's
// time (R1); files without a time keep their place after those with one.
func byFirstPacket(ctx context.Context, paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	type file struct {
		path string
		t    int64
		ok   bool
		k    int
	}
	fs := make([]file, len(paths))
	for k, p := range paths {
		t, ok := firstTime(ctx, p)
		fs[k] = file{p, t, ok, k}
	}
	sort.SliceStable(fs, func(a, b int) bool {
		if fs[a].ok != fs[b].ok {
			return fs[a].ok
		}
		return fs[a].ok && fs[a].t < fs[b].t
	})
	out := make([]string, len(fs))
	for k, f := range fs {
		out[k] = f.path
	}
	return out
}
