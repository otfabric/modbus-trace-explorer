// SPDX-License-Identifier: MIT

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// fixturePath is the engine's answers for the site trace, which the
// frontend's mock borrows its library shapes from and a UI test replays
// (A17), so the mock cannot drift from the engine. Synthetic data only.
const fixturePath = "../../frontend/src/test/fixtures/engine-site.json"

// engineFixture is what the fixture holds, keyed as the Service methods.
type engineFixture struct {
	FileInfo   service.FileInfo          `json:"file_info"`
	ScanResult service.ScanResult        `json:"scan_result"`
	Overview   service.Overview          `json:"overview"`
	Timeline   service.TimelinePage      `json:"timeline"`
	Details    map[string]service.Detail `json:"details"`
	Devices    []service.DeviceStats     `json:"devices"`
	Conns      []service.ConnStats       `json:"connections"`
	Patterns   []service.Pattern         `json:"patterns"`
	Quality    service.Quality           `json:"quality"`
	Preview    service.MBCAPPreview      `json:"mbcap_preview"`
	Export     service.ExportResult      `json:"mbcap_export"`
}

// TestEngineFixture: the fixture is the engine's current output;
// MTE_UPDATE=1 (make golden) rewrites it.
func TestEngineFixture(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	path := writeTrace(t, "site.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	id, res := importTrace(t, e, nil, path)
	var fx engineFixture
	fx.ScanResult = res
	tr, _ := e.trace(id)
	fx.FileInfo = tr.info
	fx.Overview, _ = e.Overview(ctx, id)
	fx.Timeline, _ = e.Timeline(ctx, id, service.TimelineQuery{Order: "time", Limit: 100})
	fx.Details = map[string]service.Detail{}
	for _, r := range fx.Timeline.Rows {
		d, err := e.Detail(ctx, id, r.Seq)
		if err != nil {
			t.Fatal(err)
		}
		fx.Details[jsonKey(r.Seq)] = d
	}
	fx.Devices, _ = e.Devices(ctx, id, service.Filter{})
	fx.Conns, _ = e.Connections(ctx, id, service.Filter{})
	fx.Patterns, _ = e.Patterns(ctx, id, service.Filter{})
	fx.Quality, _ = e.Quality(ctx, id)
	fx.Preview, _ = e.PreviewMBCAP(ctx, id, service.MBCAPExport{Exceptions: true, Format: "mbcap"})
	ref, err := e.ExportMBCAP(ctx, id, service.MBCAPExport{Exceptions: true, Format: "mbcap"}, filepath.Join(filepath.Dir(path), "site.mbcap"))
	fx.Export = runExport(t, e, ref, err)

	b, err := json.MarshalIndent(fx, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	// What differs between runs: the temporary path and the fingerprint
	// (it covers the file's modification time).
	b = bytes.ReplaceAll(b, []byte(filepath.Dir(path)+string(filepath.Separator)), []byte("/traces/"))
	b = bytes.ReplaceAll(b, []byte(id), []byte("site"))
	b = regexp.MustCompile(`"(analyze|scan|mbcap)-\d+"`).ReplaceAll(b, []byte(`"$1"`))
	b = append(b, '\n')
	if os.Getenv("MTE_UPDATE") != "" {
		if err := os.MkdirAll(filepath.Dir(fixturePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixturePath, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(fixturePath)
	if err != nil || !bytes.Equal(want, b) {
		t.Errorf("%s is not up to date: run make golden (%v)", fixturePath, err)
	}
}

func jsonKey(seq uint64) string { b, _ := json.Marshal(seq); return string(b) }

// smokeTrace is the trace CI's smoke test opens in the built application
// (A15): the site trace, synthetic. MTE_UPDATE=1 (make golden) writes it.
const smokeTrace = "../../testdata/smoke.pcapng"

func TestSmokeTrace(t *testing.T) {
	path := writeTrace(t, "smoke.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("MTE_UPDATE") != "" {
		if err := os.MkdirAll(filepath.Dir(smokeTrace), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(smokeTrace, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(smokeTrace)
	if err != nil || !bytes.Equal(want, b) {
		t.Errorf("%s is not up to date: run make golden (%v)", smokeTrace, err)
	}
	// The application must find Modbus in it without help.
	e := newEngine(t)
	id, res := importTrace(t, e, nil, smokeTrace)
	if len(res.Selected) == 0 {
		t.Fatal("no Modbus flow suggested")
	}
	if p, _ := e.Timeline(context.Background(), id, service.TimelineQuery{Order: "time", Limit: 5}); p.Total == 0 {
		t.Error("empty timeline")
	}
}
