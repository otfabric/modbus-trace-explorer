// SPDX-License-Identifier: MIT

package gui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// MenuEvent carries a MenuAction from the native menu to the window.
const MenuEvent = "explorer:menu"

// DocsURL is the documentation the Help menu opens.
const DocsURL = "https://github.com/otfabric/modbus-trace-explorer#readme"

// MenuAction is what a menu item asks the window to do. The window owns
// the state (the open trace, dialogs, the theme), so the menu only names
// the action.
type MenuAction struct {
	Action string   `json:"action"`          // open, recent, close, export, settings, theme, time, detail, about
	Paths  []string `json:"paths,omitempty"` // recent: the trace to open
}

// buildMenu is the application menu (ARCHITECTURE.md section 12): File,
// Edit (for copy and paste in the text boxes), View, Help, and on macOS the
// application and Window menus. Keyboard shortcuts are handled by the
// window (UX.md section 9), so the items carry no accelerators that would
// fire the same action twice. The role menus (application, Edit, Window,
// Quit, full screen) need a running application; roles is false in tests.
func buildMenu(recent []service.RecentFile, act func(MenuAction), docs func(), roles bool) *application.Menu {
	m := application.NewMenu()
	if roles && runtime.GOOS == "darwin" {
		m.AddRole(application.AppMenu)
	}
	do := func(action string) func(*application.Context) {
		return func(*application.Context) { act(MenuAction{Action: action}) }
	}
	file := m.AddSubmenu("File")
	file.Add("Open…").OnClick(do("open"))
	rec := file.AddSubmenu("Open Recent")
	n := 0
	for _, r := range recent {
		if !r.Exists {
			continue
		}
		paths := r.Paths
		rec.Add(r.Name).OnClick(func(*application.Context) { act(MenuAction{Action: "recent", Paths: paths}) })
		n++
	}
	if n == 0 {
		rec.Add("No recent traces").SetEnabled(false)
	} else {
		rec.AddSeparator()
		rec.Add("Clear Recent").OnClick(do("clear-recent"))
	}
	file.Add("Close Trace").OnClick(do("close"))
	file.AddSeparator()
	file.Add("Export…").OnClick(do("export"))
	file.AddSeparator()
	file.Add("Settings…").OnClick(do("settings"))
	if roles && runtime.GOOS != "darwin" {
		file.AddSeparator()
		file.AddRole(application.Quit)
	}
	if roles {
		m.AddRole(application.EditMenu)
	}
	view := m.AddSubmenu("View")
	view.Add("Switch Theme").OnClick(do("theme"))
	view.Add("Next Time Display").OnClick(do("time"))
	view.Add("Show or Hide Detail").OnClick(do("detail"))
	if roles {
		view.AddSeparator()
		view.AddRole(application.ToggleFullscreen)
	}
	if roles && runtime.GOOS == "darwin" {
		m.AddRole(application.WindowMenu)
	}
	help := m.AddSubmenu("Help")
	help.Add("About Modbus Trace Explorer").OnClick(do("about"))
	help.Add("Documentation").OnClick(func(*application.Context) { docs() })
	return m
}

// emitMenu sends a menu action to the window.
func emitMenu(a MenuAction) {
	if app := application.Get(); app != nil {
		app.Event.Emit(MenuEvent, a)
	}
}

// openDocs opens the documentation in the browser.
func openDocs() {
	if app := application.Get(); app != nil {
		_ = app.Browser.OpenURL(DocsURL)
	}
}

// RefreshMenu builds the application menu with the current recent files
// and installs it.
func (a *App) RefreshMenu() {
	app := application.Get()
	if app == nil {
		return
	}
	recent, err := a.svc.Recent(context.Background())
	if err != nil {
		recent = nil
	}
	app.Menu.SetApplicationMenu(buildMenu(recent, emitMenu, openDocs, true))
}

// Geometry is the window size remembered between runs. The position is
// not kept: a window restored onto a screen that is gone would be lost.
type Geometry struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximised bool `json:"maximised"`
}

const geometryFile = "window.json"

// LoadGeometry returns the remembered size, or def when there is none or
// it is out of bounds.
func LoadGeometry(dir string, def, minimum Geometry) Geometry {
	b, err := os.ReadFile(filepath.Join(dir, geometryFile))
	if err != nil {
		return def
	}
	var g Geometry
	if json.Unmarshal(b, &g) != nil || g.Width < minimum.Width || g.Height < minimum.Height || g.Width > 16384 || g.Height > 16384 {
		return def
	}
	return g
}

// SaveGeometry remembers the window size.
func SaveGeometry(dir string, g Geometry) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, geometryFile), b, 0o600)
}
