// SPDX-License-Identifier: MIT

package gui

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package when a test leaves the event forwarding
// goroutine of App running.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
