// SPDX-License-Identifier: MIT

package gui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/otfabric/modbus-trace-explorer/internal/engine"
	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

func TestUsable(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.pcap")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.pcap")
	if err := os.Symlink(f, link); err != nil {
		t.Skip(err)
	}
	got := Usable([]string{f, link, dir, filepath.Join(dir, "missing.pcap")})
	real, _ := filepath.EvalSymlinks(f)
	if len(got) != 2 || got[0] != real || got[1] != real {
		t.Errorf("%v", got)
	}
}

func TestPendingAndErrors(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.pcapng")
	_ = os.WriteFile(f, []byte("x"), 0o600)
	a := New(engine.New(engine.Options{}), []string{f})
	a.Handed([]string{f}) // no application running: kept
	if p := a.PendingFiles(); len(p) != 2 {
		t.Errorf("pending %v", p)
	}
	if p := a.PendingFiles(); len(p) != 0 {
		t.Errorf("pending twice: %v", p)
	}
	if _, err := a.About(context.Background()); err != nil {
		t.Error(err)
	}
	if err := a.OpenExternal("http://example.com"); err == nil {
		t.Error("http link opened")
	}
	var e service.Error
	_ = json.Unmarshal(MarshalError(context.Canceled), &e)
	if e.Code != service.CodeCanceled {
		t.Errorf("%+v", e)
	}
	_ = json.Unmarshal(MarshalError(service.Errorf(service.CodeNotFound, "x")), &e)
	if e.Code != service.CodeNotFound {
		t.Errorf("%+v", e)
	}
	_ = json.Unmarshal(MarshalError(errors.New("boom")), &e)
	if e.Code != service.CodeInternal {
		t.Errorf("%+v", e)
	}
}
