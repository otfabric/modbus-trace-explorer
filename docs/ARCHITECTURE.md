# Architecture

Status: design to implement. Statements are *proposed behavior* unless marked *(verified)* (checked in the local checkouts of sunspec-explorer v0.0.3, go-modbus-capture v0.0.2, modbusctl v3.2.0 and go-modbus v1.7.1 on 2026-10-15) or *(unverified)* with the milestone that must check them.

## 1. Artifact and layering

One artifact, `modbus-trace-explorer`: the Wails v3 desktop GUI with the engine in-process. There is no daemon, no HTTP API and no web build in v0.1. The frontend is written against a TypeScript `Service` and `Platform` interface (as the SunSpec Explorer's is *(verified)*) so a web build remains possible later; nothing in v0.1 builds one.

```
React UI  --(TS Service, Platform)-->  Wails bindings  -->  internal/gui  -->  internal/service (Go interface)
                                                                                     |
                                                            internal/engine  (jobs: import, history, export)
                                                              |                    |
                                                  internal/store (SQLite cache)   go-modbus-trace (library)  --> trace file
```

- `internal/service`: the Go `Service` interface and its DTOs (section 2). No Wails import.
- `internal/gui`: the only package that imports Wails. Wraps the service, forwards job events to the window, owns native dialogs and the file-open entry points.
- `internal/engine`: orchestration. Calls the library, writes the index, runs queries, runs jobs with cancellation.
- `internal/store`: the SQLite analysis cache (section 5).
- The library does all trace understanding. The application imports `go-modbus-trace` (`trace`, `model`, `selection`, `mbtcp`, `analyze`, `export/*`) and nothing else that touches Modbus.

## 2. The Service interface

Illustrative; the exact DTOs are defined in M0/M1 and published as JSON Schemas (R22).

```go
type Service interface {
    // files
    Open(ctx context.Context, path string) (FileInfo, error)            // detect format, quick fingerprint, cache lookup
    Scan(ctx context.Context, fileID string) (JobRef, error)            // job; result via ScanResult
    ScanResult(ctx context.Context, fileID string) (ScanResult, error) // file summary + flow table
    Analyze(ctx context.Context, fileID string, cfg AnalyzeConfig) (JobRef, error)
    Close(ctx context.Context, fileID string) error
    Recent(ctx context.Context) ([]RecentFile, error)

    // queries (all take the active Filter; all are read-only and paged)
    Overview(ctx context.Context, fileID string) (Overview, error)
    Timeline(ctx context.Context, fileID string, q TimelineQuery) (TimelinePage, error)
    Count(ctx context.Context, fileID string, f Filter) (Counts, error)
    Detail(ctx context.Context, fileID string, rowID uint64) (Detail, error)
    Devices(ctx context.Context, fileID string, f Filter) ([]DeviceStats, error)
    Connections(ctx context.Context, fileID string, q ConnQuery) (ConnPage, error)
    Patterns(ctx context.Context, fileID string, f Filter) ([]Pattern, error)
    Coverage(ctx context.Context, fileID string, d DeviceKey, space Space) (CoverageMap, error)
    AddressHistory(ctx context.Context, fileID string, q HistoryQuery) (JobRef, error) // streams chunks as events
    Quality(ctx context.Context, fileID string) (Quality, error)

    // exports (destination paths come from native Save dialogs)
    ExportPreview(ctx context.Context, fileID string, cfg MBCAPConfig) (ExportPreview, error)
    ExportMBCAP(ctx context.Context, fileID string, cfg MBCAPConfig, dest string) (JobRef, error)
    ExportTimeline(ctx context.Context, fileID string, cfg TimelineExportConfig, dest string) (JobRef, error)
    ExportSubtrace(ctx context.Context, fileID string, cfg SubtraceConfig, dest string) (JobRef, error)

    // jobs, settings, cache
    CancelJob(ctx context.Context, jobID string) error
    Subscribe(ctx context.Context) (<-chan Event, error) // job progress and completion, rate-limited
    Settings(ctx context.Context) (Settings, error)
    SetSettings(ctx context.Context, s Settings) error
    CacheInfo(ctx context.Context) (CacheInfo, error)
    ClearCache(ctx context.Context) error
}
```

A `Filter` mirrors the library's `Selection` plus index-only terms (quality flags, text): servers, units, clients, connections, functions, statuses, class, flags, time range, space + address range. The mapping Filter -> Selection is one function, tested, and is what the export dialogs use.

## 3. The library contract

What this application needs from `go-modbus-trace`. Each item has a requirement in the library's `docs/REQUIREMENTS.md`. If the library cannot provide one, stop and ask the user; do not reimplement it here.

| # | Capability | Library requirement |
|---|---|---|
| C1 | `Open`, `Scan` (file summary + flow table with class and Modbus score), `Analyze` with progress callback, cancellation, server-port and per-flow role overrides | L1, L10, L6, L17 |
| C2 | Event stream with, per transaction: connection, server endpoint, unit, TID, function, sub-function, **range** (space, start, count) and write range, class (read, write, other), status, exception code, flags, request/response times, latency, provenance (`FrameRef`s). Enough to fill every index column without decoding twice. | L9 |
| C3 | `File.Frames(refs)`: random access to the packets of a transaction (uncompressed files) | L16 |
| C4 | `mbtcp.Decode` returning named fields **with byte ranges** (for highlighting in the Raw tab), warnings, and a one-line summary helper | L7 (to be extended: byte ranges per field) |
| C5 | Value extraction for address history: given a decoded read response and a request range, the raw word or bit at an address | L7 (to be extended) |
| C6 | Analyzer results: overview, devices (with latency percentiles), patterns (period stats, volatility), coverage intervals, quality counters | L11 |
| C7 | `selection.Selection` covering everything the UI filter does (including quality flags and address range) so exports can follow the UI filters exactly | L12 (to be extended: flags) |
| C8 | `mbcapexport.Classify(txn) Reason` (stable reason codes), `Export` with `Options`, `Report`; the exporter uses the very same `Classify` | L13, `MBCAP-EXPORT.md` |
| C9 | Timeline and sub-trace exporters | L14, L15 |
| C10 | A version constant for event semantics so the cache invalidates when the library's output meaning changes | to be added to L9 |
| C11 | `trace/tracetest` for application tests (synthetic traces with ground truth) | `TESTING.md` |

The app records the library version it was built against in its cache and in exports (`Header.Producer`). While the library is pre-0.1.0, depend on a pseudo-version or tag; develop locally with an uncommitted `go.work`; CI rejects a `replace` in `go.mod`.

## 4. Import pipeline

```
Open(path)  --> fingerprint (size, mtime, SHA-256 of first and last MiB) --> cache hit? ----yes----> ready
   | no
Scan job --> ScanResult (summary, flows)  ==> Flows step in the UI: user confirms flows, server ports, overrides
   |
Analyze job --> library events --> batched writer --> index (SQLite) --> analyzer results stored --> ready
```

- A `.gz` input is first decompressed into the cache directory (free-space check before, progress during, removed when the file is closed). The decompressed file is what the library reads and what `Frames` seeks in.
- Scan and Analyze are single forward passes. Analyze writes rows in batches of about 10,000 inside one transaction; the database is in WAL mode with `synchronous=OFF` during import (the cache is disposable) and a `complete` flag set at the end; an interrupted import is detected on the next open and redone.
- Progress events carry bytes read, total, rows written and rate, at most 10 per second. Cancel is a context cancellation; the partial index is deleted.
- Remembered choices (selected flows, server ports, overrides) are stored in the cache file and reapplied on reopen.

## 5. The analysis cache (index)

*Technology: SQLite through the pure-Go driver `modernc.org/sqlite` (no cgo). The M0 spike measures it against a compact in-memory columnar index and records the decision and numbers here; the alternative wins only if it meets the budgets with less machinery.*

Budgets (proposed; M0 replaces them with measured values): index at most 25% of the trace size; at most about 110 bytes per timeline row plus its provenance references (8 to 24 bytes); 10 million rows paged, filtered and sorted in under 150 ms at the 95th percentile; page cache bounded (64 MiB).

Outline (names indicative):

| Table | Content |
|---|---|
| `meta` | schema version, library version, event-semantics version, quick fingerprint, full SHA-256, path at import, import status (`complete`), remembered choices |
| `summary` | the library's file summary as JSON (interfaces, link types, drops, counters) |
| `flow` | scan flow table with the user's selection and overrides |
| `device` | id, server address, port, unit, label |
| `client` | id, address |
| `conn` | id, device, client address and port, epoch, open/close type, role source, first/last time, transactions, bytes, flags |
| `item` | the timeline rows: `seq` (primary key), `kind` (txn, conn_open, conn_close, gap, resync, note), `t_ns` (request first packet, or the event time), `t_end_ns`, `conn`, `device`, `client`, `tid`, `fc`, `sub`, `class`, `status`, `exc`, `flags` (bitmask), `space`, `start`, `count`, `wstart`, `wcount`, `lat_us`, `mbcap_reason`, `req_ref`/`resp_ref` (varint-packed provenance), `extra` (small JSON for non-transaction kinds) |
| `device_stats`, `pattern`, `coverage`, `quality` | analyzer results from the library, stored as rows or JSON |

Indexes on `item`: `(t_ns, seq)`, `(device, t_ns)`, `(conn, seq)`, `(client, t_ns)`, `(fc, t_ns)`, `(status, t_ns)`, `(device, space, start)`, `(lat_us)`. Raw ADU bytes and decoded text are **not** stored; the row summary string is built on demand for the rows being displayed.

`mbcap_reason` is the library's `Classify` result for the baseline export (exceptions on, no change-only), stored at import. It makes the export preview a `GROUP BY` instead of a pass over the trace. Options that change the outcome (exceptions off, change-only) are applied to the preview arithmetically, and "up to" is shown where it cannot be exact.

Cache management: `os.UserCacheDir()/modbus-trace-explorer/<fingerprint>.db`; a small `index.json` lists files with last-used time for LRU eviction against the size cap; a library, schema or semantics version mismatch invalidates a file; "Clear cache" removes everything. Directory 0700, files 0600. The cache never contains packet bytes, only references.

## 6. Queries and paging

- **Keyset paging** on `(t_ns, seq)`: `WHERE (t_ns, seq) > (?, ?) ORDER BY t_ns, seq LIMIT n` (row values; SQLite >= 3.15 *(verified in the driver's SQLite version at M0)*), backwards with the reverse comparison. "Go to time" is a lookup on the index; "go to row N of the filtered set" uses an offset only within a bounded distance of a known key. Sorting by latency pages on `(lat_us, seq)`.
- **Filters** compile to one parameterized `WHERE` (never string-concatenated user input). The text box is parsed in Go into the same structured filter (`UX.md` grammar); anything that does not parse is a visible error, not a silent full-text search.
- **Counts** for the status line and chips use `COUNT(*)` with the same `WHERE` and are requested separately so the table never waits for them.
- All queries are cancellable by context; the frontend aborts stale requests when the filter changes.

## 7. Detail and provenance

`Detail(rowID)`: read the row, take `req_ref` and `resp_ref`, call `File.Frames`, rebuild the ADU bytes from the payload ranges (the library gives a helper), decode with `mbtcp.Decode`, and return the DTO: named fields with byte ranges, warnings, the raw ADUs (MBAP header + PDU), the packet list (frame number, time, interface, file offset, payload range), the connection and role information, and the `Classify` reason for MBCAP ("exported" or why not). It touches at most a few packets, so it is fast, and it works only while the trace is at the recorded path.

## 8. Address history

`AddressHistory(device, unit, space, address)` selects the candidate rows with the `(device, space, start)` index (reads whose range covers the address, and writes whose write range does), ordered by time, then processes them in batches: sort the batch's references by file offset, read the packets, extract the value (C5), restore time order, drop repeats unless "show every poll" is on, and stream the chunk to the UI as an event. A cap (default 200,000 points) ends the job with a "load more" continuation token; cancel is immediate between batches. The coverage map (R11) comes from the stored `coverage` rows and is instant.

## 9. Exports

All three exports follow the same shape: the dialog builds a `Filter`, the engine maps it to a library `Selection`, then **runs the library exporter over the trace** with progress and cancellation, writing to `dest` through a temporary file and rename. The index is used only for previews and counts.

- **MBCAP**: `mbcapexport.Export` with `Options` from the dialog, `Producer = "modbus-trace-explorer <ver> (go-modbus-trace <ver>)"`, labels from the dialog, tick resolution, encoding. The report is shown and can be saved as JSON. Because GUI and CLI call the same function with the same selection, the files are byte-identical (A9).
- **Timeline**: the library's CSV/JSONL writers.
- **Sub-trace**: the library's pcapng extractor.

Replay hand-off (R16) is text built from the destination path and the report's targets.

## 10. Jobs and events

A job is an engine goroutine with an ID, a kind (scan, analyze, history, export), a cancel function and a progress record. Only one analyze job per file; several export/history jobs may run. Events go to the window through `internal/gui` (Wails events), rate-limited, with the job's final state always delivered. Closing a file cancels its jobs. Quitting cancels all jobs and removes partial outputs and partial indexes.

## 11. Frontend

React 19, TypeScript, Tailwind CSS 4, Vite, Vitest + Testing Library, fonts via fontsource; versions resolved at the start of the work from current upstream releases, pinned in the lockfile, and recorded (the SunSpec Explorer's resolved set on 2026-10-14 is a known-good baseline *(verified: its `package.json`)*, not a requirement). One build, `dist/wails`.

- **Structure** as in the Explorer: `src/app/{components,state,service,platform,icons}`, `src/adapters/wails`, `src/styles`. `service/types.ts` mirrors the Go DTOs (checked against the schemas, R22); `service/mock.ts` is an in-memory Service with a fixture dataset so the UI can be built and tested **before the library exists** and in a plain browser with `vite` during development.
- **State**: a small store (as the Explorer's) with per-view query state, a page cache keyed by filter and window, request abort on change, and selection shared across views.
- **The big table.** A timeline can have 10 million rows. Browsers cap the height of a scrollable element (on the order of 10^7 to 3x10^7 px depending on the engine), so a plain virtualized list with a spacer of `rows x rowHeight` breaks beyond roughly a million rows. Therefore the table is implemented as **windowing over the index with a scaled scrollbar**: the scroll position maps to a row position by proportion, wheel and keyboard move by rows relative to the current key, and only the visible window (plus a prefetch margin) is fetched. Because of this a generic virtualization library does not help; write the small component. M0 verifies the practical limits in the three WebViews (WKWebView, WebView2, WebKitGTK) *(unverified)*.
- **Charts** (address history, interval distribution): hand-written SVG step charts with the brand tokens; no charting dependency.
- **Hex view**: a monospaced grid (IBM Plex Mono) with offset, hex and ASCII columns, field highlighting from byte ranges, selection by click and shift-click, copy.
- **Platform** (TS): `openFile`, `saveFile` (native dialog; returns the path, the Go side writes), `copyText`, `openExternal`, `persistTheme`/`loadTheme`, `recentFiles`. In the Wails build these call native dialogs; in the dev/mock build, simple stand-ins.
- **Keyboard**: `UX.md` has the map; every action is reachable without a pointer.

## 12. Wails shell

Copied and adapted from the Explorer (section 15). Window title "Modbus Trace Explorer", default size about 1440x900, minimum 1024x640, remembered size and position. Native menu: File (Open..., Open Recent, Close, Export), View (theme, time display), Help (About, Documentation). File-open entry points: dialog, drag and drop onto the window, the command-line argument, and the OS "open with" (macOS `Info.plist` document types for `.pcap`, `.pcapng`, `.cap`). *(unverified, M0)* The exact Wails v3 beta.29 APIs for file drop and open-with events must be checked before relying on them; if file drop is not reliable in a WebView, the dialog and recent files remain the guaranteed paths.

## 13. Security and privacy

- **No network.** No Go network client (lint + dependency test), CSP `default-src 'self'` with no remote origins in the WebView, nothing fetched at run time.
- **Untrusted input.** Traces come from third parties. The library is fuzzed and bounded; the application adds: a free-space check before decompression, a size cap on the cache, no execution or interpretation of trace contents, and no rendering of trace-supplied strings as HTML (all text is rendered as text; device identification strings from FC43 are untrusted text).
- **Paths.** The application opens only paths the user chose (dialog, drop, argument, recent list). It resolves symlinks, requires a regular file, and reads it read-only. Exports go through a temporary file next to the destination and a rename; an output equal to an input is refused.
- **Sensitive data.** Traces contain operational data (addresses, register values, writes). The cache holds only metadata and references, 0700/0600. Logs never contain trace content or, at info level, addresses and paths. Exports can contain addresses and register data; the export dialogs say so once.
- **Supply chain.** Dependencies pinned and recorded; `govulncheck` and the lockfile audit in CI; provenance attestations on releases.

## 14. Build, packaging, dependencies

| Item | Decision |
|---|---|
| Go | `go 1.25.0` as the Explorer (Wails v3 needs it); native runners |
| Wails | `github.com/wailsapp/wails/v3 v3.0.0-beta.29` pinned (latest v3 tag on 2026-10-15 *(verified in the Explorer's go.mod and the proposal's check)*); `@wailsio/runtime` aligned |
| Library | `github.com/otfabric/go-modbus-trace` at a tag or pseudo-version; `go-modbus-capture` arrives through it (direct import only if the `.mbcap` viewer, R24, is built) |
| Index | `modernc.org/sqlite` (pure Go) unless M0 shows otherwise |
| Schemas in tests | `github.com/santhosh-tekuri/jsonschema/v6` as the Explorer |
| Frontend | section 11; no charting, no virtualization, no icon library |
| Targets | `make build-gui` per platform; macOS bundle (ad-hoc signed) via `packaging/macos/bundle.sh`; Windows resources (icon, manifest); Linux binary, deb/rpm/AppImage later |
| CI | `build.yml` reusable, `ci.yml`, `release.yml` as the Explorer's; matrix on native runners; checksums and provenance attestations; manual Release workflow with `bump` |
| Icon | `website/public/icons/go-modbus.svg` (teal tile) with the `<metadata>` element stripped; rendered by `packaging/icons/render_appicon.py` |

## 15. Reuse from the SunSpec Explorer

Copy at commit 915d2c3 (tag v0.0.3), adapt, and keep this list current (name each file and the commit it came from) so a later extraction of a shared module is cheap. Do not import the Explorer and do not create a shared module now.

| Area | Files |
|---|---|
| Repo hygiene | `Makefile`, `.golangci.yml` (bans changed), `codecov.yml`, `CONTRIBUTING.md`, `SECURITY.md`, `RELEASE.md`, `.github/workflows/*`, `.gitignore`, `LICENSE` |
| Wails shell | `cmd/sunspec-explorer/main.go`, `internal/gui/app.go`, Windows `.syso` approach, `packaging/macos/*`, `packaging/icons/render_appicon.py` |
| Frontend shell | `vite.config.ts` (Wails mode only), `tsconfig.json`, `src/styles/*`, `src/app/components/{Header,Dialog,ConfirmDialog,ui}.tsx`, `src/app/icons/` (extend), `src/app/platform/*`, `src/app/state/{store,useTheme}.ts`, `src/assets/logo-*.svg`, test setup |
| Not copied | everything SunSpec-, polling-, session-, daemon-, web-adapter-, TLS- or recording-specific |

## 16. Milestones

| M | Scope | Needs from the library | Exit criteria |
|---|---|---|---|
| M0 | Repo scaffolding and CI from the Explorer; Wails shell opens an empty window with branding; mock Service with a fixture dataset; **store spike** (SQLite vs columnar, measured on a synthetic 10M-row dataset); **table spike** (scaled scroll in the three WebViews); Wails file-drop/open-with check | nothing | decisions and measured budgets recorded in this file; A14 skeleton passes |
| M1 | Open, Scan, Flows step, Analyze into the cache, timeline, detail pane, filters, overview strip, cache lifecycle | L1-L2 events, `Frames`, `Decode` (C1-C4, C10) | A1-A3, A5, A6, A11, A12, A13 |
| M2 | Devices, Conversations, Patterns, Addresses, Quality; schemas | C5, C6 (library L4 analyzers) | A7, A8, A18, A4 on the real index |
| M3 | MBCAP/timeline/sub-trace exports, preview, replay hand-off, settings | C7-C9 (library L3, L4) | A9, A10, A16 |
| M4 | Packaging (unsigned), documentation, polish, optional `.mbcap` viewer (R24) | - | A15, A17; R25 delivered; first release |

The UI work in M1-M3 can start before the library is ready by developing against the mock Service; integration happens when the library publishes the corresponding milestone (L2 for M1, L4 for M2/M3).
