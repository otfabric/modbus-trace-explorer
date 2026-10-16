// SPDX-License-Identifier: MIT

package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { c.t = c.t.Add(time.Second); return c.t }

func newCache(t *testing.T, capBytes int64) *Cache {
	t.Helper()
	c, err := NewCache(filepath.Join(t.TempDir(), "cache"), capBytes, (&clock{t: time.Unix(1_760_000_000, 0)}).now)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func commit(t *testing.T, c *Cache, fp string, rows int) {
	t.Helper()
	w, err := c.Begin(fp)
	if err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		_ = w.Append(Row{Kind: KindTxn, TimeNS: int64(i)}, nil, nil)
	}
	if err := c.Commit(fp, w, Meta{Fingerprint: fp}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestFingerprint(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.pcap"), filepath.Join(dir, "b.pcap")
	_ = os.WriteFile(a, bytes.Repeat([]byte("a"), 3<<20), 0o600)
	_ = os.WriteFile(b, []byte("b"), 0o600)
	f1, err := Fingerprint([]string{a, b}, "v1", 1)
	if err != nil {
		t.Fatal(err)
	}
	same, _ := Fingerprint([]string{a, b}, "v1", 1)
	other := map[string][]any{
		"order":     {[]string{b, a}, "v1", 1},
		"library":   {[]string{a, b}, "v2", 1},
		"semantics": {[]string{a, b}, "v1", 2},
	}
	if f1 != same || len(f1) != 32 {
		t.Fatalf("%q %q", f1, same)
	}
	for name, args := range other {
		if f, _ := Fingerprint(args[0].([]string), args[1].(string), args[2].(int)); f == f1 {
			t.Errorf("%s does not change the fingerprint", name)
		}
	}
	// The tail of a large file counts.
	_ = os.WriteFile(a, append(bytes.Repeat([]byte("a"), 3<<20-1), 'z'), 0o600)
	if f, _ := Fingerprint([]string{a, b}, "v1", 1); f == f1 {
		t.Error("a changed tail kept the fingerprint")
	}
	if _, err := Fingerprint([]string{filepath.Join(dir, "missing")}, "v1", 1); err == nil {
		t.Error("missing file")
	}
}

func TestCacheLifecycle(t *testing.T) {
	c := newCache(t, 0)
	if fi, _ := os.Stat(c.Root()); fi.Mode().Perm() != DirMode || c.Cap() != DefaultCap {
		t.Fatalf("root %v, cap %d", fi.Mode().Perm(), c.Cap())
	}
	if _, err := c.Lookup("none"); err == nil {
		t.Error("lookup of nothing")
	}
	commit(t, c, "aaa", 10)
	ix, err := c.Lookup("aaa")
	if err != nil || ix.Rows() != 10 {
		t.Fatalf("%v", err)
	}
	_ = ix.Close()
	// A left-over partial import is removed when the cache opens.
	_ = os.MkdirAll(filepath.Join(c.Root(), "ix-bbb.partial", "col"), DirMode)
	c2, err := NewCache(c.Root(), 0, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Root(), "ix-bbb.partial")); !os.IsNotExist(err) {
		t.Error("partial kept")
	}
	info := c2.Info()
	if info.Entries != 1 || info.Bytes == 0 || info.Root != c.Root() {
		t.Errorf("%+v", info)
	}
	if err := c2.Clear(); err != nil {
		t.Fatal(err)
	}
	if c2.Info().Entries != 0 {
		t.Error("not cleared")
	}
}

func TestEviction(t *testing.T) {
	c := newCache(t, 1)
	commit(t, c, "one", 1000) // over the cap at once: evicted at commit
	if _, err := c.Lookup("one"); err == nil {
		t.Error("an index over the cap was kept")
	}
	release := c.Hold("two")
	commit(t, c, "two", 1000)
	if _, err := c.Lookup("two"); err != nil {
		t.Errorf("a held index was evicted: %v", err)
	}
	c.SetCap(1 << 30)
	commit(t, c, "three", 10)
	c.SetCap(1)
	release()
	if err := c.Evict(); err != nil {
		t.Fatal(err)
	}
	if c.Info().Entries != 0 {
		t.Errorf("%+v", c.Info())
	}
	// Least recently used first.
	c.SetCap(1 << 30)
	commit(t, c, "old", 1000)
	commit(t, c, "new", 1000)
	ix, _ := c.Lookup("old") // now the most recent
	_ = ix.Close()
	sizes := c.Info().Bytes
	c.SetCap(sizes - 1)
	_ = c.Evict()
	if _, err := c.Lookup("new"); err == nil {
		t.Error("the least recently used survived")
	}
	if _, err := c.Lookup("old"); err != nil {
		t.Error("the most recently used was evicted")
	}
}

func gz(t *testing.T, data []byte) string {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	_, _ = zw.Write(data)
	_ = zw.Close()
	p := filepath.Join(t.TempDir(), "trace.pcapng.gz")
	if err := os.WriteFile(p, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDecompress(t *testing.T) {
	c := newCache(t, 0)
	data := bytes.Repeat([]byte("pcap"), 100_000)
	p := gz(t, data)
	if ok, _ := IsGzip(p); !ok {
		t.Fatal("not gzip")
	}
	var last int64
	out, err := c.Decompress(context.Background(), p, "fp1", func(read, _ int64) { last = read })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, data) || !strings.HasSuffix(out, "gz-fp1.pcapng") || last == 0 {
		t.Errorf("%s, %d bytes, progress %d", out, len(got), last)
	}
	if fi, _ := os.Stat(out); fi.Mode().Perm() != FileMode {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	again, err := c.Decompress(context.Background(), p, "fp1", nil)
	if err != nil || again != out {
		t.Error("not reused")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Decompress(ctx, p, "fp2", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Root(), "gz-fp2.pcapng.partial")); !os.IsNotExist(err) {
		t.Error("partial left")
	}
	bad := filepath.Join(t.TempDir(), "x.gz")
	_ = os.WriteFile(bad, []byte{0x1f, 0x8b, 1, 2, 3}, 0o600)
	if _, err := c.Decompress(context.Background(), bad, "fp3", nil); err == nil {
		t.Error("garbage decompressed")
	}
	if ok, _ := IsGzip(filepath.Join(t.TempDir(), "missing")); ok {
		t.Error("missing file is gzip")
	}
	if iec(3<<30) != "3.0 GiB" || iec(5) != "5.0 B" {
		t.Error(iec(3 << 30))
	}
}

// TestNoSpace: decompression refuses to start without room for the
// expected size, and stops when the disk fills up while it writes.
func TestNoSpace(t *testing.T) {
	c := newCache(t, 0)
	p := gz(t, bytes.Repeat([]byte("x"), 65<<20)) // past the first check, every 64 MiB
	saved := freeSpaceOf
	defer func() { freeSpaceOf = saved }()
	freeSpaceOf = func(string) (int64, error) { return FreeMargin + 1, nil }
	if _, err := c.Decompress(context.Background(), p, "small", nil); !errors.Is(err, ErrNoSpace) {
		t.Errorf("start: %v", err)
	}
	calls := 0
	freeSpaceOf = func(string) (int64, error) {
		calls++
		if calls == 1 {
			return 1 << 40, nil
		}
		return 0, nil
	}
	if _, err := c.Decompress(context.Background(), p, "fills", nil); !errors.Is(err, ErrNoSpace) {
		t.Errorf("while writing: %v", err)
	}
	if ents, _ := os.ReadDir(c.Root()); len(ents) > 1 {
		t.Errorf("left behind: %v", ents)
	}
}

// A16: RemoveCopy deletes a decompressed copy and nothing else.
func TestRemoveCopy(t *testing.T) {
	c, err := NewCache(t.TempDir(), 0, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	cp := filepath.Join(c.Root(), "gz-abc-0.pcap")
	other := filepath.Join(c.Root(), "ix-abc")
	outside := filepath.Join(t.TempDir(), "gz-x.pcap")
	for _, p := range []string{cp, outside} {
		if err := os.WriteFile(p, []byte("x"), FileMode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(other, DirMode); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{cp, cp, other, outside} {
		if err := c.RemoveCopy(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(cp); !os.IsNotExist(err) {
		t.Error("the copy is still there")
	}
	for _, p := range []string{other, outside} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s removed: %v", p, err)
		}
	}
}
