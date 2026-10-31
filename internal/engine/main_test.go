// SPDX-License-Identifier: MIT

package engine

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package when a test leaves a job or event goroutine
// running after its engine has shut down.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
