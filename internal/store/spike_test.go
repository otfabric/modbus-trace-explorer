// SPDX-License-Identifier: MIT

package store

// The M0 store spike (docs/ARCHITECTURE.md sections 5 and 16): the same
// synthetic timeline in SQLite (modernc.org/sqlite, pure Go) and in a
// compact columnar index in memory, measured on what the user interface
// asks of the index. Run it with
//
//	make spike                     # 10,000,000 rows
//	SPIKE_ROWS=1000000 make spike
//
// and record the numbers in ARCHITECTURE.md section 5. Without
// MTE_SPIKE_ROWS the test is skipped.

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// spikeRow is one timeline row as the index holds it: compact fields and
// provenance references, no bytes and no text.
type spikeRow struct {
	seq     uint32
	tNs     int64
	latUs   uint32
	conn    uint32
	device  uint16
	client  uint16
	tid     uint16
	start   uint16
	count   uint16
	flags   uint16
	fc      uint8
	status  uint8
	class   uint8
	kind    uint8
	reqRef  uint64 // file offset of the request's first packet (references are 8-24 bytes)
	respRef uint64
}

// spikeRows generates n rows of a plausible site: 40 devices behind 6
// clients, mostly FC03 polls, some writes and exceptions, time mostly
// increasing with jitter (determination order is not time order).
func spikeRows(n int) []spikeRow {
	rng := rand.New(rand.NewPCG(1, 2))
	rows := make([]spikeRow, n)
	t := int64(1_760_000_000_000_000_000)
	for i := range rows {
		t += int64(rng.IntN(2_000_000))
		r := &rows[i]
		r.seq = uint32(i + 1)
		r.tNs = t - int64(rng.IntN(1_000_000))
		r.device = uint16(rng.IntN(40))
		r.client = uint16(r.device % 6)
		r.conn = uint32(r.device)*10 + uint32(rng.IntN(3))
		r.tid = uint16(i)
		r.kind = 0
		switch x := rng.IntN(1000); {
		case x < 800:
			r.fc, r.class = 3, 0
		case x < 950:
			r.fc, r.class = 4, 0
		case x < 980:
			r.fc, r.class = 1, 0
		default:
			r.fc, r.class = 16, 1
		}
		r.status = 0
		if rng.IntN(500) == 0 {
			r.status = 1
		}
		r.start = uint16(rng.IntN(64) * 100)
		r.count = uint16(1 + rng.IntN(125))
		r.latUs = uint32(300 + rng.IntN(20_000))
		if rng.IntN(300) == 0 {
			r.flags = 1
		}
		r.reqRef = uint64(i) * 180
		r.respRef = r.reqRef + 90
	}
	return rows
}

type measure struct {
	name string
	d    time.Duration
	note string
}

func timeIt(name string, runs int, fn func() string) measure {
	var ds []time.Duration
	note := ""
	for range runs {
		t0 := time.Now()
		note = fn()
		ds = append(ds, time.Since(t0))
	}
	slices.Sort(ds)
	// p95 of the runs (the slowest for few runs).
	return measure{name, ds[min(len(ds)-1, len(ds)*95/100)], note}
}

func heapMiB() float64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapAlloc) / (1 << 20)
}

func TestSpike(t *testing.T) {
	n, err := strconv.Atoi(os.Getenv("MTE_SPIKE_ROWS"))
	if err != nil || n <= 0 {
		t.Skip("set MTE_SPIKE_ROWS (make spike) to run the store spike")
	}
	rows := spikeRows(n)
	t.Logf("rows: %d (Go %s, %s/%s, %d CPUs)", n, runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	sqliteSpike(t, rows)
	columnarSpike(t, rows)
}

func sqliteSpike(t *testing.T, rows []spikeRow) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "spike.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	for _, p := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=OFF", "PRAGMA cache_size=-65536", "PRAGMA temp_store=MEMORY"} {
		if _, err := db.Exec(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE item (
		seq INTEGER PRIMARY KEY, kind INTEGER, t_ns INTEGER, conn INTEGER, device INTEGER, client INTEGER,
		tid INTEGER, fc INTEGER, class INTEGER, status INTEGER, flags INTEGER, space INTEGER,
		start INTEGER, count INTEGER, lat_us INTEGER, req_ref INTEGER, resp_ref INTEGER) WITHOUT ROWID`); err != nil {
		t.Fatal(err)
	}
	var ms []measure
	t0 := time.Now()
	for i := 0; i < len(rows); i += 10_000 {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		st, err := tx.Prepare(`INSERT INTO item VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows[i:min(len(rows), i+10_000)] {
			if _, err := st.Exec(r.seq, r.kind, r.tNs, r.conn, r.device, r.client, r.tid, r.fc, r.class, r.status, r.flags, 3,
				r.start, r.count, r.latUs, r.reqRef, r.respRef); err != nil {
				t.Fatal(err)
			}
		}
		_ = st.Close()
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	insert := time.Since(t0)
	t0 = time.Now()
	for _, ix := range []string{
		"CREATE INDEX i_time ON item(t_ns, seq)",
		"CREATE INDEX i_device ON item(device, t_ns, seq)",
		"CREATE INDEX i_fc ON item(fc, t_ns, seq)",
		"CREATE INDEX i_status ON item(status, t_ns, seq)",
		"CREATE INDEX i_lat ON item(lat_us, seq)",
	} {
		if _, err := db.Exec(ix); err != nil {
			t.Fatal(err)
		}
	}
	indexes := time.Since(t0)
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	n := len(rows)
	t.Logf("SQLite import: %v rows at %.0f rows/s, indexes %v, file %.1f MiB = %.0f bytes/row",
		insert.Round(time.Millisecond), float64(n)/insert.Seconds(), indexes.Round(time.Millisecond),
		float64(fi.Size())/(1<<20), float64(fi.Size())/float64(n))

	q := func(query string, args ...any) string {
		rs, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rs.Close() }()
		k := 0
		var last int64
		for rs.Next() {
			var a, b int64
			if err := rs.Scan(&a, &b); err != nil {
				t.Fatal(err)
			}
			last = a
			k++
		}
		return fmt.Sprintf("%d rows, last %d", k, last)
	}
	one := func(query string, args ...any) string {
		var v int64
		if err := db.QueryRowContext(ctx, query, args...).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return strconv.FormatInt(v, 10)
	}
	mid := rows[n/2]
	ms = append(ms,
		timeIt("first page, time order", 20, func() string { return q("SELECT t_ns, seq FROM item ORDER BY t_ns, seq LIMIT 100") }),
		timeIt("keyset next page in the middle", 20, func() string {
			return q("SELECT t_ns, seq FROM item WHERE (t_ns, seq) > (?, ?) ORDER BY t_ns, seq LIMIT 100", mid.tNs, mid.seq)
		}),
		timeIt("jump by rank: OFFSET n/2", 5, func() string {
			return q("SELECT t_ns, seq FROM item ORDER BY t_ns, seq LIMIT 100 OFFSET ?", n/2)
		}),
		timeIt("jump by rank: OFFSET n-100", 5, func() string {
			return q("SELECT t_ns, seq FROM item ORDER BY t_ns, seq LIMIT 100 OFFSET ?", n-100)
		}),
		timeIt("count all", 5, func() string { return one("SELECT count(*) FROM item") }),
		timeIt("count device=7", 5, func() string { return one("SELECT count(*) FROM item WHERE device = 7") }),
		timeIt("count device=7 AND fc=3 AND status=0", 5, func() string {
			return one("SELECT count(*) FROM item WHERE device = 7 AND fc = 3 AND status = 0")
		}),
		timeIt("count class=write (no index)", 5, func() string { return one("SELECT count(*) FROM item WHERE class = 1") }),
		timeIt("filtered first page device=7", 20, func() string {
			return q("SELECT t_ns, seq FROM item WHERE device = 7 ORDER BY t_ns, seq LIMIT 100")
		}),
		timeIt("filtered jump device=7 OFFSET half", 5, func() string {
			return q("SELECT t_ns, seq FROM item WHERE device = 7 ORDER BY t_ns, seq LIMIT 100 OFFSET ?", n/80)
		}),
		timeIt("filtered page class=write (no index)", 5, func() string {
			return q("SELECT t_ns, seq FROM item WHERE class = 1 ORDER BY t_ns, seq LIMIT 100")
		}),
		timeIt("filtered jump class=write OFFSET half", 3, func() string {
			return q("SELECT t_ns, seq FROM item WHERE class = 1 ORDER BY t_ns, seq LIMIT 100 OFFSET ?", n/100)
		}),
		timeIt("latency order first page", 20, func() string { return q("SELECT lat_us, seq FROM item ORDER BY lat_us DESC, seq LIMIT 100") }),
		timeIt("latency jump OFFSET n/2", 5, func() string {
			return q("SELECT lat_us, seq FROM item ORDER BY lat_us, seq LIMIT 100 OFFSET ?", n/2)
		}),
		timeIt("rank sample: every 4096th key, time order", 3, func() string {
			return q("SELECT t_ns, seq FROM (SELECT t_ns, seq, row_number() OVER (ORDER BY t_ns, seq) AS r FROM item) WHERE r % 4096 = 1")
		}),
	)
	var version string
	_ = db.QueryRow("SELECT sqlite_version()").Scan(&version)
	t.Logf("SQLite %s (row values in keyset paging work: yes)", version)
	for _, m := range ms {
		t.Logf("  SQLite %-45s %10v  (%s)", m.name, m.d.Round(10*time.Microsecond), m.note)
	}
	t.Logf("  Go heap after SQLite: %.0f MiB", heapMiB())
}

// columnar is the alternative: one slice per column, a time-order
// permutation and a latency-order permutation built at import.
type columnar struct {
	tNs               []int64
	latUs             []uint32
	device            []uint16
	fc, status, class []uint8
	byTime, byLatency []uint32
}

func columnarSpike(t *testing.T, rows []spikeRow) {
	n := len(rows)
	before := heapMiB()
	t0 := time.Now()
	c := &columnar{tNs: make([]int64, n), latUs: make([]uint32, n), device: make([]uint16, n),
		fc: make([]uint8, n), status: make([]uint8, n), class: make([]uint8, n)}
	for i, r := range rows {
		c.tNs[i], c.latUs[i], c.device[i], c.fc[i], c.status[i], c.class[i] = r.tNs, r.latUs, r.device, r.fc, r.status, r.class
	}
	perm := func(less func(a, b uint32) bool) []uint32 {
		p := make([]uint32, n)
		for i := range p {
			p[i] = uint32(i)
		}
		sort.Slice(p, func(i, j int) bool { return less(p[i], p[j]) })
		return p
	}
	c.byTime = perm(func(a, b uint32) bool { return c.tNs[a] < c.tNs[b] || (c.tNs[a] == c.tNs[b] && a < b) })
	c.byLatency = perm(func(a, b uint32) bool { return c.latUs[a] < c.latUs[b] || (c.latUs[a] == c.latUs[b] && a < b) })
	build := time.Since(t0)
	held := heapMiB() - before
	t.Logf("Columnar build: %v, held %.0f MiB = %.0f bytes/row (the fields an index query reads; provenance stays on disk)",
		build.Round(time.Millisecond), held, held*(1<<20)/float64(n))

	filter := func(order []uint32, keep func(i uint32) bool) []uint32 {
		out := make([]uint32, 0, n/8)
		for _, i := range order {
			if keep(i) {
				out = append(out, i)
			}
		}
		return out
	}
	var sel []uint32
	ms := []measure{
		timeIt("page by rank anywhere (unfiltered)", 20, func() string {
			p := c.byTime[n/2 : n/2+100]
			return fmt.Sprint(len(p))
		}),
		timeIt("filter device=7 (selection vector)", 5, func() string {
			sel = filter(c.byTime, func(i uint32) bool { return c.device[i] == 7 })
			return fmt.Sprint(len(sel))
		}),
		timeIt("filter device=7 AND fc=3 AND status=0", 5, func() string {
			sel = filter(c.byTime, func(i uint32) bool { return c.device[i] == 7 && c.fc[i] == 3 && c.status[i] == 0 })
			return fmt.Sprint(len(sel))
		}),
		timeIt("filter class=write", 5, func() string {
			sel = filter(c.byTime, func(i uint32) bool { return c.class[i] == 1 })
			return fmt.Sprint(len(sel))
		}),
		timeIt("filtered page by rank (after filter)", 20, func() string { return fmt.Sprint(len(sel[len(sel)/2 : len(sel)/2+min(100, len(sel)/2)])) }),
		timeIt("filter class=write in latency order", 5, func() string {
			sel = filter(c.byLatency, func(i uint32) bool { return c.class[i] == 1 })
			return fmt.Sprint(len(sel))
		}),
	}
	for _, m := range ms {
		t.Logf("  Columnar %-43s %10v  (%s)", m.name, m.d.Round(10*time.Microsecond), m.note)
	}
}
