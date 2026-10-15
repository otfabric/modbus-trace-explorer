// SPDX-License-Identifier: MIT

// Package buildinfo holds the version of the application and of the
// libraries it was built with.
package buildinfo

import "runtime/debug"

// Version is the application version. Release builds set it with
// -ldflags "-X github.com/otfabric/modbus-trace-explorer/internal/buildinfo.Version=...".
var Version = "dev"

// Module paths whose versions the application reports (About, diagnostics,
// the MBCAP producer and the cache's validity).
const (
	GoModbusTracePath = "github.com/otfabric/go-modbus-trace"
	WailsPath         = "github.com/wailsapp/wails/v3"
)

// ModuleVersion returns the version of a dependency in this binary, or
// "unknown" when the binary carries no module information.
func ModuleVersion(path string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return depVersion(info, path)
}

func depVersion(info *debug.BuildInfo, path string) string {
	for _, dep := range info.Deps {
		if dep.Path == path {
			if dep.Replace != nil {
				return dep.Replace.Version
			}
			return dep.Version
		}
	}
	return "unknown"
}
