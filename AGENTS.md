# OT Fabric Modbus Trace Explorer: agent handoff

Status (2026-10-18): **v0.2.0 is released** (`RELEASE.md`): the status bar's jobs and cache, the resizable detail pane, frontend coverage, `.mbcap` captures with the views (R24), the test corpus (`testdata/corpus`, see `docs/ARCHITECTURE.md` section 16) and the timeline during the import. Since then: the README's demo video and screenshots, made by `docs/media/record-demo.mjs` against `make demo-server` (`docs/ARCHITECTURE.md` section 14). v0.1 was M0 to M4. M4: the native menu, the filter and capture-facts popovers, go to time and frame, the remaining keys, Copy diagnostics, the smoke test (A15), the contract tests between the mock and the engine (A17), and the Release workflow. Opening `.mbcap` files (R24) is deferred to v0.2. M3: the MBCAP, timeline and sub-trace exports with the live preview, the report and the replay hand-off, and Settings with cache management. M2: the Devices, Conversations, Patterns, Addresses (coverage and value history) and Quality views, computed from the index under the active filter and checked against the library's analyzers. M1: opening, gzip, the scan and the Flows step, the analysis into the columnar index, the cache lifecycle, the timeline, filters with the text grammar, the detail pane, the log file, the DTO schemas (`docs/ARCHITECTURE.md` section 16 lists what M1 left out). M0: repository scaffolding and CI from the SunSpec Explorer, the Wails shell (branding, file drop, "open with", the command line), the frontend shell with the mock Service and the scaled-scroll timeline table, and the store and table spikes with their decisions in `docs/ARCHITECTURE.md` sections 5, 11 and 16. After v0.1.0: R24 and the items marked SHOULD or later in `docs/REQUIREMENTS.md`. This file is the entry point for Claude Code and other agents. Read it fully, then the documents it points to, before writing code. The library this application depends on, `otfabric/go-modbus-trace`, is built in its own repository from its own handoff documents; the contract between the two is in `docs/ARCHITECTURE.md` section 3.

## What this is

A **desktop tool (Wails v3 + React/TypeScript) to understand Modbus/TCP traffic recorded by someone else.** The user opens a packet trace (pcap or pcapng from tcpdump, Wireshark, dumpcap or tshark), confirms which flows are Modbus, and gets a timeline of events in Modbus terms (connect, read 62 holding registers at 0x1000, write 1 register, exception, no response, disconnect) with breakdowns per device, unit, connection, poll pattern and address, a detail view down to the bytes and the packets, and exports: **MBCAP** (so a passive capture of a real installation can be replayed as a virtual device by `modbusctl server replay`), a timeline as CSV/JSON Lines, and a filtered sub-trace as pcapng.

It does **not** capture live (tcpdump and Wireshark do that better), does not talk to devices, and never writes to Modbus. All trace understanding lives in the library [otfabric/go-modbus-trace](https://github.com/otfabric/go-modbus-trace), which also provides the headless CLI `mbtracectl` for gateways and CI. This application is the interactive front end on that engine.

## Read these, in this order

1. `docs/REQUIREMENTS.md`: what to build, numbered requirements (R1..), acceptance tests (A1..).
2. `docs/ARCHITECTURE.md`: layering, the library contract, import pipeline, the analysis cache, queries, exports, frontend, security, milestones.
3. `docs/UX.md`: screens, behaviors, summary-line grammar, keyboard map, empty and error states.
4. `docs/BRANDING.md`: OT Fabric visual identity, tokens, icon rules, accessibility numbers.
5. `docs/PROPOSAL-REVIEW.md`: why the original "Modbus Capture" proposal was replaced, what was verified, what is still unverified.
6. `modbus-capture-claude-proposal.md` is the **superseded** original proposal (live capture, daemon, browser mode). Do not follow it; it is kept for history.
7. Sibling checkouts to read (read-only): `sunspec-explorer/` (the reference application: frontend shell, Wails shell, CI, packaging; commit 915d2c3 / tag v0.0.3), `go-modbus-trace/` (the library: L0–L4 done, v0.1.0 pending), `go-modbus-capture/docs/` and `modbusctl/` (what the MBCAP export feeds), `website/` (branding source).

## Decisions that are settled (do not relitigate without asking the user)

- **No live capture** (user decision 2026-10-15). Input is a trace file. There is no capture backend, no privilege model, no Npcap/BPF, no recording.
- **One artifact**: `modbus-trace-explorer`, the Wails desktop GUI (cgo). **No daemon and no browser mode in v0.1.** Headless and remote use is the CLI `mbtracectl` from the library (on a gateway: `mbtracectl export mbcap` or `summary` on the trace there). The frontend still talks to a TypeScript `Service` and `Platform` interface so a web build could be added later; do not build one now.
- **The engine is a separate library** (`go-modbus-trace`). The application contains no pcap parsing, reassembly, framing, decoding or pairing code. It orchestrates, indexes, queries and presents. If the library lacks something the application needs, record it in `docs/ARCHITECTURE.md` section 3 and ask the user; do not reimplement it here.
- **The trace file is the source of truth; the index is a disposable cache.** The application stores compact rows with provenance references in a columnar cache of memory-mapped column files (decided 2026-10-16 from the M0 spike). Raw bytes, ADU details and address values are re-read from the trace on demand through the library. **Exports always re-stream the trace through the library**, never from the index, so the GUI and `mbtracectl` produce identical files.
- **Read-only on inputs**: the application never modifies, moves or deletes a trace. Outputs are written to a temporary file next to the destination and renamed on success.
- **MBCAP is the only replay format** and is produced only by the library through go-modbus-capture. Captures hold reads only; writes and vendor functions stay visible in the UI but are never exported (the export dialog shows what is excluded and why).
- **Raw data is authoritative.** The UI shows raw words, bits and hex. Zero-based PDU addresses are primary; a 1-based/reference label is a display toggle. No type decoding (float, scaling, strings) in v0.1; it is a later display profile.
- **Local only**: the application makes no network connections of any kind. No updater, no telemetry, no remote resources (fonts and everything else are bundled).
- **Two repositories, one convention set**: copy conventions and shell code from `sunspec-explorer` (listed below). Do not extract a shared module now; keep the list of copied files in `docs/ARCHITECTURE.md` section 15 so a later extraction is possible.
- **Releases are unsigned for now** (no macOS or Windows signing identity): SHA-256 checksums, CI build provenance attestations, documented Gatekeeper/SmartScreen steps.
- **Branding**: dark theme by default, tokens from `docs/BRANDING.md`, fonts bundled via fontsource. The application's own icon is the **Modbus protocol icon** (`website/public/icons/go-modbus.svg`), per the rule that each OT Fabric GUI uses its protocol's icon.
- **Name** (user decision 2026-10-15): **Modbus Trace Explorer**; repository and GUI binary `modbus-trace-explorer`; library `go-modbus-trace`; CLI `mbtracectl`.

## Hard rules

- No network I/O in Go (`net/http`, `net.Dial*` are banned by lint outside tests) and none in the frontend (CSP `default-src 'self'`, no remote fonts, no analytics).
- No Modbus I/O and no import of `go-modbus` in non-test code. (The library uses it in tests only.)
- Never log trace content (payload bytes, register values) and, at info level, never IP addresses or file paths; the log file lives in the application's data directory.
- Never hold all rows in memory: the UI shows a virtualized window, the Go side pages from the index. Never send a whole trace to the frontend.
- The cache directory is created 0700, files 0600; the cache is safe to delete at any time.
- `go.mod` must never contain a `replace` directive or a local path for `go-modbus-trace`; use an uncommitted `go.work` for local library development. CI fails on `replace`.
- No cgo in `internal/` except what the Wails GUI strictly needs under `cmd/modbus-trace-explorer` and `internal/gui`. The index is the application's own column files (no SQLite; `docs/ARCHITECTURE.md` section 5).
- Pin the Wails v3 beta version in `go.mod` (beta.29 on 2026-10-15); it is a prerelease and APIs may still change. Align `@wailsio/runtime` with it.
- Do not commit unless the user asks. When committing, follow the user's commit conventions.

## Conventions to copy from `sunspec-explorer` (v0.0.3 / 915d2c3)

Copy, then adapt (do not import): `Makefile` (`make check`, `frontend`, `build-gui`, `bundle-macos`, `appicon`, `winres`), `.golangci.yml` (replace the read-only bans with the network bans above), `codecov.yml`, `CONTRIBUTING.md`, `SECURITY.md`, `RELEASE.md`, `.github/workflows/{build,ci,release}.yml` (native-architecture runners, never QEMU; manual Release workflow with `bump` input; notes from `RELEASE.md`), `packaging/macos/`, `packaging/icons/render_appicon.py`, `cmd/sunspec-explorer/main.go` and `internal/gui/app.go` (Wails shell), the Windows resources approach, `frontend/` shell: `src/styles/{tokens,fonts,index}.css`, `src/app/components/{Header,Dialog,ConfirmDialog,ui}.tsx`, `src/app/icons/`, `src/app/platform/`, `useTheme`, the logo assets, `vite.config.ts`, `tsconfig.json`, test setup. Its `AGENTS.md` and `docs/` are the model for these documents.

## Target repo layout

```
cmd/modbus-trace-explorer/   Wails GUI main (cgo); app icon; Windows resources
internal/gui/                Wails service binding, event forwarding, native dialogs (Wails only here)
internal/service/            Service interface, DTOs, errors (no Wails import)
internal/engine/             jobs (import, address history, export), orchestration over go-modbus-trace
internal/store/              columnar cache: column files, writers, selection vectors, cache manager
internal/buildinfo/
frontend/                    React + TypeScript + Tailwind v4; one build (dist/wails); src/app/{components,state,service,platform}, src/adapters/wails, a mock Service with fixture data
packaging/                   macOS bundle, icons, later installers
tests/                       soak and scale tests, fixtures (small, synthetic or from OT Fabric rigs)
docs/                        the documents above, schema/ (JSON Schema of the application's DTOs)
```

## Answered by the user

- **No live capture; import traces** (2026-10-15). tcpdump/Wireshark do live capture better; the product is the Modbus-level timeline, the introspection and the exports.
- **MBCAP export is required** (replay of a state based on a passive capture).
- **Name**: Modbus Trace Explorer (2026-10-15). **Headless**: CLI only, no daemon or browser build. **Engine**: a separate go- library, started now.
- GUI platforms as in Explorer: macOS arm64 and amd64, Windows amd64 (arm64 best effort, never blocking), Linux amd64 and arm64. No armv7 GUI (Wails v3 does not support it). Gateways (Debian 11, 12, 13; amd64, arm64, armv7) only need the library's CLI.

## Open questions

None blocking. Decided by the user on 2026-10-16 from the M0 spike: **a columnar index** (memory-mapped column files, no SQLite; `docs/ARCHITECTURE.md` section 5). Settled by M0: the scaled-scroll table (section 11) and the Wails file-drop and open-with APIs (section 12); their run-time checks in the three WebViews follow in M1.

## Milestones (details in docs/ARCHITECTURE.md section 16)

M0 shell, mock Service, store spike and budgets (needs nothing from the library) -> M1 open, import, flows step, timeline, detail pane, filters (needs library L2) -> M2 overview, devices, conversations, patterns, addresses, quality (needs L2, analyzers from L4) -> M3 exports, replay hand-off, settings, cache management (needs L3, L4) -> M4 packaging (unsigned), documentation, polish, optional `.mbcap` viewer.

## Definition of done for any change

`make check` passes; a change to a DTO in `internal/service` is made in `docs/schema/` too and in the TypeScript types; new behavior has a test against traces built with the library's `tracetest` package; UI changes use tokens, not hard-coded colors, and are checked in both themes; no new network path; no new dependency without a recorded reason.
