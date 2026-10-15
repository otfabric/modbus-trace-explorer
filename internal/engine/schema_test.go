// SPDX-License-Identifier: MIT

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/otfabric/go-modbus-trace/trace/tracetest"

	"github.com/otfabric/modbus-trace-explorer/internal/schemagen"
	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// The DTOs with a published schema (R22): docs/schema/<name>-1.schema.json.
var schemas = []struct {
	name, title string
	v           any
}{
	{"about", "About", service.About{}},
	{"file-info", "Open trace", service.FileInfo{}},
	{"recent-files", "Recent files", []service.RecentFile{}},
	{"event", "Job event", service.Event{}},
	{"scan-result", "Flows step", service.ScanResult{}},
	{"analyze-config", "Analysis choices", service.AnalyzeConfig{}},
	{"overview", "Overview", service.Overview{}},
	{"timeline-query", "Timeline query", service.TimelineQuery{}},
	{"timeline-page", "Timeline page", service.TimelinePage{}},
	{"detail", "Row detail", service.Detail{}},
	{"filter-parse", "Parsed filter", service.FilterParse{}},
	{"error", "Error", service.Error{}},
}

const schemaDir = "../../docs/schema"

// TestSchemasUpToDate: every DTO's schema in docs/schema matches the Go
// type; MTE_UPDATE=1 (make golden) rewrites them.
func TestSchemasUpToDate(t *testing.T) {
	for _, s := range schemas {
		b, err := schemagen.Generate("https://github.com/otfabric/modbus-trace-explorer/docs/schema/"+s.name+"-1.schema.json",
			s.title, "Generated from internal/service by internal/engine's TestSchemasUpToDate.", s.v)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(schemaDir, s.name+"-1.schema.json")
		if os.Getenv("MTE_UPDATE") != "" {
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		old, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(bytes.ReplaceAll(old, []byte("\r"), nil), b) {
			t.Errorf("%s is not up to date: run make golden (%v)", path, err)
		}
	}
}

func validator(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	c := jsonschema.NewCompiler()
	path, _ := filepath.Abs(filepath.Join(schemaDir, name+"-1.schema.json"))
	s, err := c.Compile(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func validate(t *testing.T, name string, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if err := validator(t, name).Validate(doc); err != nil {
		t.Errorf("%s does not validate: %v\n%s", name, err, b)
	}
}

// TestOutputValidates (A18): the engine's real output validates against
// the schemas.
func TestOutputValidates(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	path := writeTrace(t, "site.pcapng", tracetest.Options{Format: tracetest.PcapNG}, site)
	sctx, stop := context.WithCancel(ctx)
	events, _ := e.Subscribe(sctx)
	var seen []service.Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range events {
			seen = append(seen, ev)
		}
	}()
	id, res := importTrace(t, e, nil, path)
	stop()
	<-done
	if len(seen) < 2 {
		t.Errorf("%d events", len(seen))
	}
	for _, ev := range seen {
		validate(t, "event", ev)
	}
	validate(t, "scan-result", res)
	info, _ := e.Open(ctx, []string{path})
	validate(t, "file-info", info)
	ab, _ := e.About(ctx)
	validate(t, "about", ab)
	rec, _ := e.Recent(ctx)
	validate(t, "recent-files", rec)
	ov, _ := e.Overview(ctx, id)
	validate(t, "overview", ov)
	q := service.TimelineQuery{Order: "time", Limit: 100}
	validate(t, "timeline-query", q)
	p, _ := e.Timeline(ctx, id, q)
	validate(t, "timeline-page", p)
	for _, r := range p.Rows {
		d, err := e.Detail(ctx, id, r.Seq)
		if err != nil {
			t.Fatal(err)
		}
		validate(t, "detail", d)
	}
	fp, _ := e.ParseFilter(ctx, id, "unit:1 color:red")
	validate(t, "filter-parse", fp)
	validate(t, "analyze-config", service.AnalyzeConfig{Flows: []uint32{1}, ServerPorts: []uint16{502}, Servers: []string{}})
	validate(t, "error", service.Errorf(service.CodeNotFound, "x"))
}
