// SPDX-License-Identifier: MIT

package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fuzzSuffixes are the names a fuzzed file gets: each takes another path
// through Open (a trace, gzip, each capture encoding).
var fuzzSuffixes = []string{".pcap", ".pcapng", ".pcapng.gz", ".mbcap", ".mbcap.jsonl", ".mbcap.csv"}

// FuzzOpen feeds hostile bytes, seeded with the corpus, through everything
// the window does with a file (runCorpusFile): whatever the input, the
// engine answers with a result or an error, never a panic or a hang.
// The seeds run with go test; make fuzz runs it for longer.
func FuzzOpen(f *testing.F) {
	for _, c := range loadCorpus(f) {
		data, err := os.ReadFile(filepath.Join(corpusDir, c.File))
		if err != nil || len(data) > 64<<10 {
			continue // the large realistic files take too long per input
		}
		for k, s := range fuzzSuffixes {
			if strings.HasSuffix(c.File, s) {
				f.Add(data, uint8(k))
			}
		}
	}
	e := newEngine(f)
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, data []byte, suffix uint8) {
		path := filepath.Join(dir, "input"+fuzzSuffixes[int(suffix)%len(fuzzSuffixes)])
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		_ = runCorpusFile(t, e, path)
		if d := time.Since(start); d > 2*time.Second && os.Getenv("MTE_FUZZ_SLOW") != "" {
			_ = os.WriteFile(filepath.Join(os.Getenv("MTE_FUZZ_SLOW"), fmt.Sprintf("slow-%d%s", d.Milliseconds(), filepath.Ext(path))), data, 0o600)
		}
	})
}
