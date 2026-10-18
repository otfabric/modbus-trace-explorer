# Contributing to modbus-trace-explorer

Thank you for your interest in contributing. Read [`AGENTS.md`](AGENTS.md) first: it lists the settled decisions and the hard rules, and points to the requirements, the architecture, the UX and the branding in [`docs/`](docs/).

## Development Setup

### Prerequisites

- Go 1.25+ (see [go.mod](go.mod)); Wails v3 needs it. CI and releases build with the current stable Go
- Node.js 22+ and npm for the frontend
- [staticcheck](https://staticcheck.dev/), [golangci-lint](https://golangci-lint.run/) v2 and [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) for the checks
- For the desktop application: a C compiler and the platform's webview (macOS: Xcode command line tools; Windows: WebView2 runtime; Linux: GTK4 and WebKitGTK 6.0)
- No devices and no network: tests build their traces with go-modbus-trace's `tracetest`

### Build & Test

```sh
make frontend-install       # once: npm ci
make check                  # format, vet, lint, vulnerability scan, race tests, coverage gate, frontend checks
make build-gui               # ./bin/modbus-trace-explorer
cd frontend && npm run dev  # the user interface over the mock Service, in a browser
```

The README's screenshots and demo video are made by `docs/media/record-demo.mjs` (usage in its header) against `make demo-server`: rerun it when the user interface changes visibly.

Run `make` (or `make help`) for the full list of targets. A fresh checkout compiles and tests without a frontend build (the embed holds a placeholder); a binary needs `make frontend` first.

## Rules

- **Local only.** No network connections (lint bans `net/http` and dialing; `internal/policy` checks the dependency graph), no remote resources in the frontend.
- **No Modbus I/O.** The application reads traces; it never talks to a device.
- **The library understands traces.** No pcap parsing, reassembly, framing, decoding or pairing here; if go-modbus-trace lacks something, record it in `docs/ARCHITECTURE.md` section 3.
- **Tokens only** for colors (`frontend/src/styles/tokens.css`); both themes; no state by color alone.
- **DTOs change in three places at once:** `internal/service`, `frontend/src/app/service/types.ts` and `docs/schema/`.
- **No `replace` in `go.mod`**; develop against a local library with an uncommitted `go.work`.

## Pull Requests

- Branch from `main`; one open pull request at a time, targeting `main`.
- `make check` passes locally.
