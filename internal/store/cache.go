// SPDX-License-Identifier: MIT

package store

import (
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultCap is the default size cap of the cache (R3).
const DefaultCap int64 = 2 << 30

// Cache is the cache directory: one index directory per trace
// fingerprint, decompressed copies of gzip traces, and index.json with the
// last use of each entry for eviction. It is safe for concurrent use.
type Cache struct {
	root string
	now  func() time.Time

	mu     sync.Mutex
	inUse  map[string]int // entries that must not be evicted
	capped int64
}

type cacheIndex struct {
	Entries map[string]cacheEntry `json:"entries"`
}

type cacheEntry struct {
	LastUsed time.Time `json:"last_used"`
}

// NewCache opens (or creates) the cache at root, 0700. now is the clock for
// the last-use times (time.Now outside tests).
func NewCache(root string, capBytes int64, now func() time.Time) (*Cache, error) {
	err := os.MkdirAll(root, DirMode)
	if err == nil {
		err = os.Chmod(root, DirMode) // an existing directory too
	}
	if err != nil {
		return nil, err
	}
	if capBytes <= 0 {
		capBytes = DefaultCap
	}
	c := &Cache{root: root, now: now, inUse: map[string]int{}, capped: capBytes}
	// Imports, decompressions and live snapshots that a crash or a kill
	// left behind.
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".partial") || strings.Contains(e.Name(), ".live-") {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
	return c, nil
}

// Root returns the cache directory.
func (c *Cache) Root() string { return c.root }

// Cap returns the size cap.
func (c *Cache) Cap() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.capped
}

// SetCap changes the size cap; it applies at the next eviction.
func (c *Cache) SetCap(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n > 0 {
		c.capped = n
	}
}

// Fingerprint identifies a trace (its files, in order) together with what
// interprets it: the size, modification time and the SHA-256 of the first
// and last MiB of each file, the library version and its event semantics,
// and this store's format. A change to any of them gives another
// fingerprint, so a stale index is never used.
func Fingerprint(paths []string, library string, semantics int) (string, error) {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "store %d\x00library %s\x00semantics %d\x00", FormatVersion, library, semantics) // a hash does not fail
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return "", err
		}
		fi, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return "", err
		}
		_, _ = fmt.Fprintf(h, "file %d %d\x00", fi.Size(), fi.ModTime().UnixNano())
		const mib = 1 << 20
		if _, err := io.Copy(h, io.LimitReader(f, mib)); err != nil {
			_ = f.Close()
			return "", err
		}
		if fi.Size() > mib {
			if _, err := f.Seek(-min(mib, fi.Size()-mib), io.SeekEnd); err != nil {
				_ = f.Close()
				return "", err
			}
			if _, err := io.Copy(h, f); err != nil {
				_ = f.Close()
				return "", err
			}
		}
		_ = f.Close()
	}
	return hex.EncodeToString(h.Sum(nil))[:32], nil
}

// Dir returns where the index of a fingerprint lives.
func (c *Cache) Dir(fp string) string { return filepath.Join(c.root, "ix-"+fp) }

// Lookup opens the complete index of a fingerprint, or returns ErrFormat
// (or a not-exist error) when there is none.
func (c *Cache) Lookup(fp string) (*Index, error) {
	ix, err := Open(c.Dir(fp))
	if err != nil {
		return nil, err
	}
	c.touch("ix-" + fp)
	return ix, nil
}

// Begin starts writing the index of a fingerprint into a partial
// directory; Commit moves it into place.
func (c *Cache) Begin(fp string) (*Writer, error) {
	partial := c.Dir(fp) + ".partial"
	_ = os.RemoveAll(partial)
	return Create(partial)
}

// Commit finishes w and moves the index into place, replacing an older one.
func (c *Cache) Commit(fp string, w *Writer, meta Meta, docs map[string]any) error {
	if err := w.Finish(meta, docs); err != nil {
		_ = w.Abort()
		return err
	}
	dst := c.Dir(fp)
	_ = os.RemoveAll(dst)
	if err := os.Rename(w.Dir(), dst); err != nil {
		_ = w.Abort()
		return err
	}
	c.touch("ix-" + fp)
	return c.Evict()
}

// LiveDir is the directory of snapshot k of the import of fp (an index
// readable while it is written); the engine removes it when the import
// ends, and a start removes any a crash left.
func (c *Cache) LiveDir(fp string, k int) string {
	return filepath.Join(c.root, fmt.Sprintf("ix-%s.live-%d", fp, k))
}

// Hold marks an entry (a fingerprint's index and its decompressed trace)
// as in use until the returned release is called: it is not evicted.
func (c *Cache) Hold(fp string) func() {
	c.mu.Lock()
	c.inUse[fp]++
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			if c.inUse[fp]--; c.inUse[fp] <= 0 {
				delete(c.inUse, fp)
			}
			c.mu.Unlock()
		})
	}
}

func (c *Cache) touch(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := c.readIndex()
	idx.Entries[name] = cacheEntry{LastUsed: c.now().UTC()}
	_ = writeJSON(filepath.Join(c.root, "index.json"), idx)
}

func (c *Cache) readIndex() cacheIndex {
	var idx cacheIndex
	if err := readJSON(filepath.Join(c.root, "index.json"), &idx); err != nil || idx.Entries == nil {
		idx.Entries = map[string]cacheEntry{}
	}
	return idx
}

// Info describes the cache.
type Info struct {
	Root    string `json:"root"`
	Bytes   int64  `json:"bytes"`
	Cap     int64  `json:"cap"`
	Entries int    `json:"entries"`
}

// Info returns the cache's location, size and entry count.
func (c *Cache) Info() Info {
	ents := c.entries()
	var n int64
	for _, e := range ents {
		n += e.size
	}
	return Info{Root: c.root, Bytes: n, Cap: c.Cap(), Entries: len(ents)}
}

type entry struct {
	name string
	fp   string
	size int64
	used time.Time
}

func (c *Cache) entries() []entry {
	c.mu.Lock()
	idx := c.readIndex()
	c.mu.Unlock()
	ents, _ := os.ReadDir(c.root)
	var out []entry
	for _, e := range ents {
		name := e.Name()
		if name == "index.json" || strings.HasSuffix(name, ".partial") || strings.Contains(name, ".live-") {
			continue
		}
		fp := strings.TrimPrefix(strings.TrimPrefix(name, "ix-"), "gz-")
		fp = strings.TrimSuffix(fp, filepath.Ext(fp))
		out = append(out, entry{name: name, fp: fp, size: diskSize(filepath.Join(c.root, name)), used: idx.Entries[name].LastUsed})
	}
	return out
}

func diskSize(path string) int64 {
	var n int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// Evict removes the least recently used entries that are not in use until
// the cache is within its cap.
func (c *Cache) Evict() error {
	ents := c.entries()
	var total int64
	for _, e := range ents {
		total += e.size
	}
	slices.SortFunc(ents, func(a, b entry) int { return cmp.Or(a.used.Compare(b.used), strings.Compare(a.name, b.name)) })
	var err error
	for _, e := range ents {
		if total <= c.Cap() {
			break
		}
		c.mu.Lock()
		busy := c.inUse[e.fp] > 0
		c.mu.Unlock()
		if busy {
			continue
		}
		if rerr := os.RemoveAll(filepath.Join(c.root, e.name)); rerr != nil {
			err = errors.Join(err, rerr)
			continue
		}
		total -= e.size
		c.forget(e.name)
	}
	return err
}

func (c *Cache) forget(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := c.readIndex()
	delete(idx.Entries, name)
	_ = writeJSON(filepath.Join(c.root, "index.json"), idx)
}

// Clear removes every entry that is not in use.
func (c *Cache) Clear() error {
	var err error
	for _, e := range c.entries() {
		c.mu.Lock()
		busy := c.inUse[e.fp] > 0
		c.mu.Unlock()
		if busy {
			continue
		}
		err = errors.Join(err, os.RemoveAll(filepath.Join(c.root, e.name)))
		c.forget(e.name)
	}
	return err
}

// ErrGzipCut is returned with the decompressed copy's path when the gzip
// file ends early or is damaged after its start: the copy holds what
// decompressed, and the trace can be read up to there.
var ErrGzipCut = errors.New("the gzip file ends early or is damaged; what decompressed is kept")

// ErrNoSpace is returned when decompressing a trace would not leave
// enough free space.
var ErrNoSpace = errors.New("not enough free space to decompress the trace")

// freeSpaceOf is freeSpace, replaceable in tests.
var freeSpaceOf = freeSpace

// FreeMargin is the space that must stay free on the cache's disk.
const FreeMargin int64 = 512 << 20

// IsGzip reports whether a file starts with the gzip magic.
func IsGzip(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	var b [2]byte
	if _, err := io.ReadFull(f, b[:]); err != nil {
		return false, nil
	}
	return b[0] == 0x1f && b[1] == 0x8b, nil
}

// RemoveCopy deletes a decompressed copy made by Decompress (A16: the copy
// lives only while its trace is open). Other paths are left alone.
func (c *Cache) RemoveCopy(path string) error {
	name := filepath.Base(path)
	if filepath.Dir(path) != c.root || !strings.HasPrefix(name, "gz-") {
		return nil
	}
	c.forget(name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Decompress writes the decompressed copy of a gzip trace into the cache
// (an entry of its own, named by fp) and returns its path; an existing
// copy is reused. Before it starts it checks that the expected size (from
// the gzip trailer, at least four times the compressed size) leaves
// FreeMargin free; while it writes it stops as soon as the disk's free
// space would fall below FreeMargin (a gzip bomb ends there).
// progress is called with bytes read of the compressed file.
func (c *Cache) Decompress(ctx context.Context, path, fp string, progress func(read, total int64)) (string, error) {
	if dst, ok := c.Decompressed(path, fp); ok {
		return dst, nil
	}
	dst := c.gzName(path, fp)
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = in.Close() }()
	fi, err := in.Stat()
	if err != nil {
		return "", err
	}
	free, ferr := freeSpaceOf(c.root)
	if ferr == nil && free-expected(in, fi.Size()) < FreeMargin {
		return "", fmt.Errorf("%w: about %s needed, %s free", ErrNoSpace, iec(expected(in, fi.Size())), iec(free))
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	counted := &countReader{r: in}
	zr, err := gzip.NewReader(counted)
	if err != nil {
		return "", err
	}
	tmp := dst + ".partial"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, FileMode)
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		_ = out.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	buf := make([]byte, 1<<20)
	var written, sinceCheck int64
	var cut error // the gzip stream's error after some data: the copy is kept
	for {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		n, rerr := zr.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return fail(err)
			}
			written += int64(n)
			sinceCheck += int64(n)
			if sinceCheck >= 64<<20 {
				sinceCheck = 0
				if free, err := freeSpaceOf(c.root); err == nil && free < FreeMargin {
					return fail(fmt.Errorf("%w: stopped after %s", ErrNoSpace, iec(written)))
				}
			}
			if progress != nil {
				progress(counted.n, fi.Size())
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			if written == 0 {
				return fail(rerr)
			}
			cut = rerr
			break
		}
	}
	if err := out.Close(); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fail(err)
	}
	c.touch(filepath.Base(dst))
	if cut != nil {
		return dst, errors.Join(fmt.Errorf("%w: %v", ErrGzipCut, cut), c.Evict())
	}
	return dst, c.Evict()
}

// gzName is the decompressed copy's path: trace.pcapng.gz ->
// gz-<fp>.pcapng; anything else -> .pcap (the readers detect the format by
// its magic, not by the name).
func (c *Cache) gzName(path, fp string) string {
	ext := filepath.Ext(strings.TrimSuffix(filepath.Base(path), ".gz"))
	if ext != ".pcapng" && ext != ".cap" {
		ext = ".pcap"
	}
	return filepath.Join(c.root, "gz-"+fp+ext)
}

// Decompressed returns the decompressed copy of path made earlier under
// fp, if the cache still has it.
func (c *Cache) Decompressed(path, fp string) (string, bool) {
	dst := c.gzName(path, fp)
	if _, err := os.Stat(dst); err != nil {
		return "", false
	}
	c.touch(filepath.Base(dst))
	return dst, true
}

// expected estimates the decompressed size: the gzip trailer's ISIZE (the
// size modulo 4 GiB), and at least four times the compressed size.
func expected(f *os.File, size int64) int64 {
	est := 4 * size
	if size >= 4 {
		var b [4]byte
		if _, err := f.ReadAt(b[:], size-4); err == nil {
			est = max(est, int64(binary.LittleEndian.Uint32(b[:])))
		}
	}
	return est
}

type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func iec(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	v, u := float64(n), 0
	for v >= 1024 && u < len(units)-1 {
		v /= 1024
		u++
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + " " + units[u]
}
