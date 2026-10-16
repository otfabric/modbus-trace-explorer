// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// The Service contract (A17), held against the engine through the
// interface the window uses: every method that takes a trace refuses an
// unknown one with not_found, and every method answers on a trace that is
// analysed. The DTOs' shapes are held by TestOutputValidates.
func TestServiceContract(t *testing.T) {
	e := newEngine(t)
	var s service.Service = e
	ctx := context.Background()
	path := writeTrace(t, "site.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	id, _ := importTrace(t, e, nil, path)
	out := filepath.Join(t.TempDir(), "x")
	seq := uint64(1)
	f := service.Filter{}
	calls := map[string]func(id string) error{
		"Close": func(id string) error { return nil }, // closes; checked last
		"Scan": func(id string) error {
			ref, err := s.Scan(ctx, id)
			e.wait(ref.ID)
			return err
		},
		"ScanResult": func(id string) error { _, err := s.ScanResult(ctx, id); return err },
		"Prepare": func(id string) error {
			ref, err := s.Prepare(ctx, id)
			e.wait(ref.ID)
			return err
		},
		"Overview": func(id string) error { _, err := s.Overview(ctx, id); return err },
		"Timeline": func(id string) error {
			_, err := s.Timeline(ctx, id, service.TimelineQuery{Order: "time", Limit: 5})
			return err
		},
		"Detail":      func(id string) error { _, err := s.Detail(ctx, id, 1); return err },
		"Devices":     func(id string) error { _, err := s.Devices(ctx, id, f); return err },
		"Connections": func(id string) error { _, err := s.Connections(ctx, id, f); return err },
		"Patterns":    func(id string) error { _, err := s.Patterns(ctx, id, f); return err },
		"Coverage": func(id string) error {
			_, err := s.Coverage(ctx, id, service.CoverageQuery{Server: srv1.String(), Unit: 1, Space: "holding"})
			return err
		},
		"AddressHistory": func(id string) error {
			ref, err := s.AddressHistory(ctx, id, service.HistoryQuery{Server: srv1.String(), Unit: 1, Space: "holding", Address: 100})
			e.wait(ref.ID)
			return err
		},
		"Quality":     func(id string) error { _, err := s.Quality(ctx, id); return err },
		"ParseFilter": func(id string) error { _, err := s.ParseFilter(ctx, id, "unit:1"); return err },
		"Locate": func(id string) error {
			_, err := s.Locate(ctx, id, service.LocateQuery{Seq: &seq, Step: 1})
			return err
		},
		"Diagnostics":  func(id string) error { _, err := s.Diagnostics(ctx, id); return err },
		"PreviewMBCAP": func(id string) error { _, err := s.PreviewMBCAP(ctx, id, service.MBCAPExport{}); return err },
		"ExportMBCAP": func(id string) error {
			ref, err := s.ExportMBCAP(ctx, id, service.MBCAPExport{}, out+".mbcap")
			e.wait(ref.ID)
			return err
		},
		"ExportTimeline": func(id string) error {
			ref, err := s.ExportTimeline(ctx, id, service.TimelineExport{Format: "csv"}, out+".csv")
			e.wait(ref.ID)
			return err
		},
		"ExportSubtrace": func(id string) error {
			ref, err := s.ExportSubtrace(ctx, id, service.SubtraceExport{}, out+".pcapng")
			e.wait(ref.ID)
			return err
		},
		"Analyze": func(id string) error {
			ref, err := s.Analyze(ctx, id, service.AnalyzeConfig{ServerPorts: []uint16{502, 1502}})
			e.wait(ref.ID)
			return err
		},
	}
	for name, call := range calls {
		if name == "Close" {
			continue
		}
		if err := call("no-such-trace"); !isCode(err, service.CodeNotFound) {
			t.Errorf("%s of an unknown trace: %v", name, err)
		}
		if err := call(id); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, err := range map[string]error{
		"About":       func() error { _, err := s.About(ctx); return err }(),
		"Recent":      func() error { _, err := s.Recent(ctx); return err }(),
		"Settings":    func() error { _, err := s.Settings(ctx); return err }(),
		"CacheInfo":   func() error { _, err := s.CacheInfo(ctx); return err }(),
		"ClearRecent": s.ClearRecent(ctx),
		"CancelJob": func() error {
			err := s.CancelJob(ctx, "no-such-job")
			return map[bool]error{true: nil, false: err}[isCode(err, service.CodeNotFound)]
		}(),
		"ExportResult": func() error {
			_, err := s.ExportResult(ctx, "no-such-job")
			return map[bool]error{true: nil, false: err}[isCode(err, service.CodeNotFound)]
		}(),
	} {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := s.Close(ctx, id); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := s.Close(ctx, id); !isCode(err, service.CodeNotFound) {
		t.Errorf("Close twice: %v", err)
	}
	if _, err := s.Open(ctx, nil); !isCode(err, service.CodeInvalidArgument) {
		t.Errorf("Open of nothing: %v", err)
	}
}
