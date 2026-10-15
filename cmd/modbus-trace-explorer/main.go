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
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/otfabric/modbus-trace-explorer/frontend"
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
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	eng := engine.New(engine.Options{Logger: log})
	defer func() { _ = eng.Close() }()
	binding := gui.New(eng, flags.Args())

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
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     appTitle,
		Width:     1440,
		Height:    900,
		MinWidth:  1024,
		MinHeight: 640,
		URL:       "/",
		// Files dropped on an element marked data-file-drop-target arrive as
		// WindowFilesDropped with their paths.
		EnableFileDrop: true,
		// --ot-bg of the dark theme, so the window does not flash white.
		BackgroundColour: application.NewRGB(0x11, 0x18, 0x1f),
	})
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		binding.Handed(e.Context().DroppedFiles())
	})
	return app.Run()
}
