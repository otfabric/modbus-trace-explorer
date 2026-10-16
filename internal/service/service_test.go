// SPDX-License-Identifier: MIT

package service

import (
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func TestErrors(t *testing.T) {
	e := Errorf(CodeNotFound, "no trace %d", 3)
	if e.Error() != "no trace 3" || e.Code != CodeNotFound || e.Unwrap() != nil {
		t.Errorf("%+v", e)
	}
	w := Wrap(CodeInternal, io.ErrUnexpectedEOF, "reading %s", "x")
	if w.Error() != "reading x: unexpected EOF" || !errors.Is(w, io.ErrUnexpectedEOF) {
		t.Errorf("%+v", w)
	}
}

// Unit IDs and codes travel as numbers, not as base64 (a []uint8 would).
func TestUint8sJSON(t *testing.T) {
	b, err := json.Marshal(Filter{Units: Uint8s{1, 2}, Functions: Uint8s{3}, Exceptions: Uint8s{}})
	if err != nil || string(b) != `{"units":[1,2],"functions":[3]}` {
		t.Fatalf("%s %v", b, err)
	}
	var f Filter
	if err := json.Unmarshal([]byte(`{"units":[1,255],"functions":null}`), &f); err != nil || len(f.Units) != 2 || f.Units[1] != 255 || f.Functions != nil {
		t.Fatalf("%+v %v", f, err)
	}
	for _, bad := range []string{`{"units":[256]}`, `{"units":[-1]}`, `{"units":"AQ=="}`} {
		if err := json.Unmarshal([]byte(bad), &f); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if b, _ := json.Marshal(Uint8s(nil)); string(b) != "null" {
		t.Errorf("nil: %s", b)
	}
}
