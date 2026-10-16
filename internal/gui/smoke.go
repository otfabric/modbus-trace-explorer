// SPDX-License-Identifier: MIT

package gui

import (
	"errors"
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The smoke test (A15): started with -smoke and a trace, the window imports
// it without asking (the Flows step's suggestion), shows the timeline, and
// reports back; the application then quits with the outcome.

// SetSmoke switches the smoke test on; done receives its outcome.
func (a *App) SetSmoke(done func(error)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.smoke = done
}

// SmokeMode tells the window whether to run the smoke test.
func (a *App) SmokeMode() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.smoke != nil
}

// SmokeDone is the window's report: the rows the timeline shows, or what
// went wrong. The report function given to SetSmoke ends the process; the
// Quit after it is for a report function that returns (tests).
func (a *App) SmokeDone(rows int, problem string) {
	a.mu.Lock()
	done := a.smoke
	a.smoke = nil
	a.mu.Unlock()
	if done == nil {
		return
	}
	switch {
	case problem != "":
		done(errors.New(problem))
	case rows <= 0:
		done(errors.New("the timeline is empty"))
	default:
		done(nil)
	}
	if app := application.Get(); app != nil {
		app.Quit()
	}
}

// SmokeError is the outcome of a smoke test that did not see the timeline.
func SmokeError(err error) error { return fmt.Errorf("smoke test: %w", err) }
