// SPDX-License-Identifier: MIT

// Command modbus-trace-explorer is the OT Fabric Modbus Trace Explorer: a
// desktop application to understand Modbus/TCP traffic recorded in a packet
// trace (pcap or pcapng). It reads traces, never captures, and never talks
// to a device.
//
//	modbus-trace-explorer [--debug] [trace.pcapng ...]
//
// Trace paths on the command line are opened as one trace.
package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/otfabric/modbus-trace-explorer/frontend"
	"github.com/otfabric/modbus-trace-explorer/internal/applog"
	"github.com/otfabric/modbus-trace-explorer/internal/buildinfo"
	"github.com/otfabric/modbus-trace-explorer/internal/engine"
	"github.com/otfabric/modbus-trace-explorer/internal/gui"
)

const appTitle = "Modbus Trace Explorer"

// appIcon is the Modbus icon of the OT Fabric website (docs/BRANDING.md,
// section 4): each OT Fabric GUI carries the icon of its protocol. It is the
// window and dock icon; the macOS bundle derives its .icns from the same
// file.
//
//go:embed appicon.png
var appIcon []byte

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "modbus-trace-explorer:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("modbus-trace-explorer", flag.ContinueOnError)
	version := flags.Bool("version", false, "print the version and exit")
	debug := flags.Bool("debug", false, "log at debug level")
	smoke := flags.Bool("smoke", false, "open the trace given, show its timeline and quit: 0 when it works (CI, A15)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *version {
		fmt.Printf("modbus-trace-explorer %s (go-modbus-trace %s)\n", buildinfo.Version,
			buildinfo.ModuleVersion(buildinfo.GoModbusTracePath))
		return nil
	}

	assets := frontend.Wails()
	if !frontend.Built(assets) {
		return fmt.Errorf("this binary was built without the user interface: run \"make frontend\" and build again")
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	// The log file (R23) lives with the settings; without it, stderr.
	var out io.Writer = os.Stderr
	if lf, err := applog.Open(filepath.Join(engine.DefaultConfigDir(), "logs", "modbus-trace-explorer.log")); err == nil {
		defer func() { _ = lf.Close() }()
		out = lf
	}
	log := applog.New(out, level)

	opts := engine.Options{Logger: log}
	config := engine.DefaultConfigDir()
	scratch := ""
	if *smoke {
		// A smoke test leaves the user's cache, recent files and window
		// size alone.
		dir, err := os.MkdirTemp("", "modbus-trace-explorer-smoke-")
		if err != nil {
			return err
		}
		scratch = dir
		defer func() { _ = os.RemoveAll(scratch) }()
		opts.CacheDir, opts.ConfigDir = filepath.Join(scratch, "cache"), filepath.Join(scratch, "config")
		config = opts.ConfigDir
	}
	eng, err := engine.New(opts)
	if err != nil {
		return fmt.Errorf("the analysis cache: %w", err)
	}
	defer func() { _ = eng.Shutdown() }()
	binding := gui.New(eng, flags.Args())
	if *smoke {
		if len(flags.Args()) == 0 {
			return fmt.Errorf("-smoke needs a trace")
		}
		// The window's report ends the process with its outcome: quitting
		// a Wails application may not return from Run (macOS ends the
		// process itself), so the exit code is set here.
		binding.SetSmoke(func(err error) {
			_ = eng.Shutdown()
			_ = os.RemoveAll(scratch)
			if err != nil {
				fmt.Fprintln(os.Stderr, "modbus-trace-explorer:", gui.SmokeError(err))
				os.Exit(1)
			}
			fmt.Println("smoke test: the timeline is shown")
			os.Exit(0)
		})
		// A window that never reports fails the test instead of hanging CI.
		timer := time.AfterFunc(3*time.Minute, func() {
			fmt.Fprintln(os.Stderr, "modbus-trace-explorer: smoke test: no timeline after 3 minutes")
			_ = os.RemoveAll(scratch)
			os.Exit(2)
		})
		defer timer.Stop()
	}

	app := application.New(application.Options{
		Name:        appTitle,
		Description: "Understand Modbus/TCP traffic recorded in a packet trace",
		Icon:        appIcon,
		Logger:      log,
		Services: []application.Service{
			application.NewServiceWithOptions(binding, application.ServiceOptions{MarshalError: gui.MarshalError}),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		// Windows and Linux: a trace opened with this application arrives as
		// ApplicationOpenedWithFile; macOS uses the bundle's document types.
		FileAssociations: gui.TraceExtensions,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationOpenedWithFile, func(e *application.ApplicationEvent) {
		binding.Handed([]string{e.Context().Filename()})
	})
	geo := gui.LoadGeometry(config, gui.Geometry{Width: 1440, Height: 900}, gui.Geometry{Width: 1024, Height: 640})
	binding.RefreshMenu()
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     appTitle,
		Width:     geo.Width,
		Height:    geo.Height,
		MinWidth:  1024,
		MinHeight: 640,
		URL:       "/",
		// Files dropped on an element marked data-file-drop-target arrive as
		// WindowFilesDropped with their paths.
		UseApplicationMenu: true, // Windows and Linux: the menu bar in the window
		EnableFileDrop:     true,
		// --ot-bg of the dark theme, so the window does not flash white.
		BackgroundColour: application.NewRGB(0x11, 0x18, 0x1f),
	})
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		binding.Handed(e.Context().DroppedFiles())
	})
	if geo.Maximised {
		win.Maximise()
	}
	// The size is remembered when the window closes (not the position).
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		g := gui.Geometry{Maximised: win.IsMaximised()}
		g.Width, g.Height = win.Size()
		if g.Maximised {
			g.Width, g.Height = geo.Width, geo.Height // keep the size it restores to
		}
		if err := gui.SaveGeometry(config, g); err != nil {
			log.Warn("cannot remember the window size", "err", err)
		}
	})
	if err := app.Run(); err != nil {
		return err
	}
	if *smoke {
		return gui.SmokeError(errors.New("the window closed without a report"))
	}
	return nil
}
