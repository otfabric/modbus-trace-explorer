// SPDX-License-Identifier: MIT

package service

import (
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
