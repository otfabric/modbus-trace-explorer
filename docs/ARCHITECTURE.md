# Architecture

Status (2026-10-16): **M0 is implemented** (shell, mock, spikes; section 16). Statements about M1 and later are *proposed behavior* unless marked *(verified)* (checked in the local checkouts of sunspec-explorer v0.0.3, go-modbus-capture v0.0.2, modbusctl v3.2.0, go-modbus v1.7.1 and go-modbus-trace at main `119d699` on 2026-10-15/16) or *(unverified)* with the milestone that must check them.

## 1. Artifact and layering

One artifact, `modbus-trace-explorer`: the Wails v3 desktop GUI with the engine in-process. There is no daemon, no HTTP API and no web build in v0.1. The frontend is written against a TypeScript `Service` and `Platform` interface (as the SunSpec Explorer's is *(verified)*) so a web build remains possible later; nothing in v0.1 builds one.

```
React UI  --(TS Service, Platform)-->  Wails bindings  -->  internal/gui  -->  internal/service (Go interface)
                                                                                     |
                                                            internal/engine  (jobs: import, history, export)
                                                              |                    |
                                                  internal/store (columnar cache) go-modbus-trace (library)  --> trace file
```

- `internal/service`: the Go `Service` interface and its DTOs (section 2). No Wails import.
- `internal/gui`: the only package that imports Wails. Wraps the service, forwards job events to the window, owns native dialogs and the file-open entry points.
- `internal/engine`: orchestration. Calls the library, writes the index, runs queries, runs jobs with cancellation.
- `internal/store`: the columnar analysis cache (section 5).
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

What this application needs from `go-modbus-trace`. Each item has a requirement in the library's `docs/REQUIREMENTS.md`. If the library cannot provide one, stop and ask the user; do not reimplement it here. *(verified 2026-10-16 against go-modbus-trace main `119d699`, milestones L0–L4: every item is provided; the column "In the library" says where.)*

| # | Capability | Library requirement | In the library (v0.1.0 candidate) |
|---|---|---|---|
| C1 | `Open`, `Scan` (file summary + flow table with class and Modbus score), `Analyze` with progress callback, cancellation, server-port and per-flow role overrides | L1, L10, L6, L17 | `trace.Open`/`OpenFiles`, `Scan`, `Analyze` with `ScanOptions.Progress`, `ctx` cancellation, `ScanOptions.ServerPorts`, `Servers`/`Clients` overrides. **Per-flow** overrides are not built (library D13): the Flows step's "this endpoint is the server" sets an endpoint override, which has the same effect for that flow's server |
| C2 | Event stream with, per transaction: connection, server endpoint, unit, TID, function, sub-function, **range** (space, start, count) and write range, class (read, write, other), status, exception code, flags, request/response times, latency, provenance (`FrameRef`s). Enough to fill every index column without decoding twice. | L9 | `model.Event`, `model.Transaction` (all listed fields; `FrameRef` provenance; `schema_version`) |
| C3 | `File.Frames(refs)`: random access to the packets of a transaction (uncompressed files) | L16 | `File.Frames(refs)`, `ErrNotSeekable` for gzip and streams |
| C4 | `mbtcp.Decode` returning named fields **with byte ranges** (for highlighting in the Raw tab), warnings, and a one-line summary helper | L7 (to be extended: byte ranges per field) | `mbtcp.Decode` (fields with byte ranges in the ADU), `mbtcp.Summary`, warnings |
| C5 | Value extraction for address history: given a decoded read response and a request range, the raw word or bit at an address | L7 (to be extended) | `mbtcp.ValueAt(req, resp, addr)` |
| C6 | Analyzer results: overview, devices (with latency percentiles), patterns (period stats, volatility), coverage intervals, quality counters | L11 | `analyze.NewOverview`, `NewDevices`, `NewPatterns`, `NewCoverage` with `Tee`; quality counters in `trace.FileSummary.Quality` |
| C7 | `selection.Selection` covering everything the UI filter does (including quality flags and address range) so exports can follow the UI filters exactly | L12 (to be extended: flags) | `selection.Selection` with `Require`/`Exclude` flags and `Space`+ranges |
| C8 | `mbcapexport.Classify(txn) Reason` (stable reason codes), `Export` with `Options`, `Report`; the exporter uses the very same `Classify` | L13, `MBCAP-EXPORT.md` | `mbcapexport.Classify`, `Export`, `Options`, `Report`, `Warn*` codes |
| C9 | Timeline and sub-trace exporters | L14, L15 | `export/timeline.Write`, `export/subtrace.Extract` |
| C10 | A version constant for event semantics so the cache invalidates when the library's output meaning changes | to be added to L9 | `model.EventSemanticsVersion` |
| C11 | `trace/tracetest` for application tests (synthetic traces with ground truth) | `TESTING.md` | `trace/tracetest` (and `testdata/plant.pcapng`) |

The app records the library version it was built against in its cache and in exports (`Header.Producer`). Depend on a tag (v0.1.0 once released) or a pseudo-version of the library's main; develop locally with an uncommitted `go.work`; CI rejects a `replace` in `go.mod` (`make mod-check`). M0 imports nothing from the library, so `go.mod` names it from M1 on.

## 4. Import pipeline

```
Open(path)  --> fingerprint (size, mtime, SHA-256 of first and last MiB) --> cache hit? ----yes----> ready
   | no
Scan job --> ScanResult (summary, flows)  ==> Flows step in the UI: user confirms flows, server ports, overrides
   |
Analyze job --> library events --> column writers --> index (column files) --> permutations, analyzer results --> ready
```

- A `.gz` input is first decompressed into the cache directory (free-space check before, progress during, removed when the file is closed). The decompressed file is what the library reads and what `Frames` seeks in.
- Scan and Analyze are single forward passes. Analyze appends rows to the column files through buffered writers; the permutations and analyzer results are written at the end, then `meta.json` with `complete`. An interrupted import (no `complete`) is detected on the next open and redone; the cache is disposable, so nothing is fsynced.
- Progress events carry bytes read, total, rows written and rate, at most 10 per second. Cancel is a context cancellation; the partial index directory is deleted.
- Remembered choices (selected flows, server ports, overrides) are stored in the cache file and reapplied on reopen.

## 5. The analysis cache (index)

**Decided (user, 2026-10-16, from the M0 spike below): a columnar index of memory-mapped column files, without SQLite.** The original design proposed SQLite through the pure-Go driver `modernc.org/sqlite`; the spike measured both, and SQLite missed the size and latency budgets for unindexed filters and rank jumps.

### M0 spike: measured (2026-10-16)

`make spike` (`internal/store/spike_test.go`) writes the same 10,000,000 synthetic timeline rows (40 devices, 6 clients, 80% FC03, writes, exceptions, flags, latencies, 16 bytes of provenance per row) into both candidates and times what the user interface asks of the index. Apple M1, 8 cores, Go 1.27.2, `modernc.org/sqlite` v1.53.0 (SQLite 3.53.2); p95 of 5 to 20 runs.

| Operation (10,000,000 rows) | SQLite (WAL, `synchronous=OFF`, 5 indexes) | Columnar (one slice per column, time and latency permutations) |
|---|---|---|
| Import | 33.6 s rows + 63.6 s indexes = 97 s (about 100,000 rows/s) | 3.3 s build |
| Size | 1,386 MiB file = **145 bytes/row** | **25 bytes/row** for the queried columns (238 MiB); provenance (16 bytes/row) stays on disk |
| First page / keyset next page (time order) | 0.6 / 0.5 ms | constant (a slice) |
| Jump by rank to the middle / the end (`OFFSET`) | 90 / 182 ms | constant |
| Count all | 83 ms | constant |
| Filter on an indexed column (device), count / first page / jump | 16 ms / 0.1 ms / 7.7 ms | 24 ms (scan to a selection vector), then constant |
| Filter on an unindexed column (class = write), count / first page / jump | 765 ms / 5.7 ms / **4,756 ms** | 24 ms, then constant |
| Three conditions (device, function, status), count | **10,974 ms** (the planner picks the status index) | 25 ms |
| Latency order first page / jump to the middle | 0.3 / 74 ms | constant (latency permutation) |
| Unindexed filter in latency order | not measured (worse than the above) | 43 ms |
| Rank sample (every 4,096th key, to map a scrollbar) | 10,776 ms | not needed |

Reading the numbers against the budgets (R21): rows of 145 bytes put a SQLite index at 25 to 35% of a typical trace (about 400 to 600 trace bytes per transaction with its ACKs), over the 25% budget; columnar rows of 41 bytes with provenance are 7 to 10%. SQLite meets the latency budget only for filters with a matching index, and the UI's filter has a dozen independent terms (device, unit, client, connection, function, status, class, flags, time, space and address range) whose combinations cannot all be indexed: an unindexed or badly planned filter costs 0.8 to 11 s. The scaled scrollbar needs jumps by rank, which SQLite answers with `OFFSET` in time proportional to the rank (180 ms at the end of 10 million rows, 4.8 s in a filtered set). The columnar index answers every one of these in a single scan of the filtered columns, 25 to 45 ms at 10 million rows, and every page afterwards in constant time. Import is 30 times faster.

**Decision (confirmed by the user on 2026-10-16):** a columnar index in the cache directory, one file per column of fixed-width values (time as `int64`; device, client, connection, unit, function, status, class, flags, space, start, count, latency as the narrowest integers; the two provenance references), plus the time-order and latency-order permutations, written at import. Queries scan the filtered columns into a selection vector (cached per filter) and page by rank. The files are memory-mapped where the platform allows (`syscall.Mmap` on macOS and Linux, `CreateFileMapping` from `golang.org/x/sys/windows`, already a dependency through Wails), so the resident set is page cache the operating system can reclaim and R21's 500 MB holds for any trace size; without a mapping the scan reads the files in chunks. The small tables (meta, file summary, flows, devices, connections, analyzer results) are JSON files in the same directory. SQLite would then not be a dependency. The trade-off is machinery: a column file format (versioned, with a `complete` marker) and a selection-vector cache are code this repository owns, about the size of the SQLite schema plus the query builder they replace, and simpler to reason about for the budgets.

Not measured yet (M1, on the real index): memory-mapped scans under memory pressure, Windows file mapping, the selection cache's memory for very selective and very broad filters, and import from the library's events end to end (the library's `Analyze` runs at about 200 MB/s on one core, so the index build is not the bottleneck).

Budgets (R21; the M0 numbers are above): index at most 25% of the trace size; at most about 110 bytes per timeline row plus its provenance references (8 to 24 bytes); 10 million rows paged, filtered and sorted in under 150 ms at the 95th percentile; page cache bounded (64 MiB).

Layout (names indicative), one directory per trace: `os.UserCacheDir()/modbus-trace-explorer/<fingerprint>/`.

| File | Content |
|---|---|
| `meta.json` | format version, library version, event-semantics version, quick fingerprint, full SHA-256, path at import, import status (`complete`, written last), remembered choices (flows, server ports, overrides) |
| `summary.json` | the library's file summary (interfaces, link types, drops, counters) |
| `flows.json` | the scan's flow table with the user's selection and overrides |
| `devices.json`, `clients.json`, `conns.json` | dictionaries: id to server address, port and unit, label; id to client address; connections with endpoints, epoch, open/close, role source, first/last time, transactions, bytes, flags |
| `col/<name>.bin` | the timeline rows, one fixed-width little-endian column per field, row `i` at offset `i * width`: `seq` (u32), `kind` (u8: txn, conn_open, conn_close, gap, resync, note), `t_ns` (i64: request first packet, or the event time), `t_end_ns` (i64), `conn` (u32), `device` (u16), `client` (u16), `unit` (u8), `tid` (u16), `fc` (u8), `sub` (u16), `class` (u8), `status` (u8), `exc` (u8), `flags` (u32 bitmask), `space` (u8), `start`, `count`, `wstart`, `wcount` (u16), `lat_us` (u32), `mbcap_reason` (u8), `req_ref`/`resp_ref` (u64 each: the offset of the first packet's record; the rest of the provenance is re-read from the trace), `extra` (u32 offset into `extra.jsonl` for non-transaction kinds) |
| `perm/time.bin`, `perm/latency.bin` | u32 row numbers in time order (`t_ns`, then `seq`) and in latency order (`lat_us`, then `seq`), written at the end of the import |
| `device_stats.json`, `patterns.json`, `coverage.json`, `quality.json` | analyzer results from the library |

Raw ADU bytes and decoded text are **not** stored; the row summary string is built on demand for the rows being displayed. The columns are memory-mapped for queries (`syscall.Mmap` on macOS and Linux, `golang.org/x/sys/windows` file mappings on Windows); without a mapping they are read in chunks.

`mbcap_reason` is the library's `Classify` result for the baseline export (exceptions on, no change-only), stored at import. It makes the export preview a count over one column instead of a pass over the trace. Options that change the outcome (exceptions off, change-only) are applied to the preview arithmetically, and "up to" is shown where it cannot be exact.

Cache management: a small `index.json` in the cache root lists the trace directories with their last-used time for LRU eviction against the size cap; a library, format or semantics version mismatch invalidates a directory; "Clear cache" removes everything. Directories 0700, files 0600. The cache never contains packet bytes, only references.

## 6. Queries and paging

- **A filter is a selection vector.** The engine compiles the structured `Filter` to a predicate over the columns it names and scans them once, in the requested order's permutation, into a list of matching row numbers (u32). The list is cached per (filter, order) with a small LRU bounded in bytes; every page and every count for that filter is then a slice of it. Measured: 25 to 45 ms for 10 million rows, whatever the filter (section 5). Very broad filters are cheap to represent as "all rows" plus the permutation, which needs no list.
- **Paging by rank.** `TimelineQuery{offset, limit}` returns rows `offset..offset+limit` of the filtered, ordered set: one slice of the selection vector, then one read of each displayed column. The scaled scrollbar (section 11) needs exactly this. "Go to time" is a binary search in the time permutation (or the selection vector); "go to frame" maps through `req_ref`.
- **Descending order** reads the same vector from the end.
- **Filters** are structured values, never strings concatenated into a query. The text box is parsed in Go into the same structured filter (`UX.md` grammar); anything that does not parse is a visible error, not a silent full-text search.
- **Counts** for the status line and chips are the lengths of selection vectors and are returned with each page.
- All scans are cancellable by context between chunks; the frontend aborts stale requests when the filter changes.

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
- **The big table.** A timeline can have 10 million rows. Browsers cap the height of a scrollable element (on the order of 10^7 to 3x10^7 px depending on the engine), so a plain virtualized list with a spacer of `rows x rowHeight` breaks beyond roughly a million rows. Therefore the table is implemented as **windowing over the index with a scaled scrollbar**: the scroll position maps to a row position by proportion, wheel and keyboard move by rows relative to the current key, and only the visible window (plus a prefetch margin) is fetched. Because of this a generic virtualization library does not help; write the small component. *(M0: built as `frontend/src/app/table/{scroll.ts,TimelineTable.tsx}`.)* The spacer is capped at 4,000,000 px, well under every engine's limit, so the cap itself needs no per-engine measurement; above it the thumb maps to ranks by proportion and the wheel, arrows, Page Up/Down and Home/End move by rows. Checked with the mock's 10,000,000 rows in Chromium (the development browser pane): both ends reachable, a jump renders in one fetch, key repeat and wheel move exactly. *(unverified, M1)* The same in WKWebView, WebView2 and WebKitGTK, in the running application.
- **Charts** (address history, interval distribution): hand-written SVG step charts with the brand tokens; no charting dependency.
- **Hex view**: a monospaced grid (IBM Plex Mono) with offset, hex and ASCII columns, field highlighting from byte ranges, selection by click and shift-click, copy.
- **Platform** (TS): `openFile`, `saveFile` (native dialog; returns the path, the Go side writes), `copyText`, `openExternal`, `persistTheme`/`loadTheme`, `recentFiles`. In the Wails build these call native dialogs; in the dev/mock build, simple stand-ins.
- **Keyboard**: `UX.md` has the map; every action is reachable without a pointer.

## 12. Wails shell

Copied and adapted from the Explorer (section 15). Window title "Modbus Trace Explorer", default size 1440x900, minimum 1024x640 (remembered size and position: M1). Native menu (M1): File (Open..., Open Recent, Close, Export), View (theme, time display), Help (About, Documentation). File-open entry points, all funnelled into `gui.App.Handed`, which keeps only regular files (absolute, symlinks resolved) and announces them to the window with the event `explorer:open`; paths that arrive before the window is ready wait in `PendingFiles`:

| Entry point | Wails v3 beta.29 mechanism *(verified in its source, M0)* |
|---|---|
| Open dialog | `app.Dialog.OpenFile()...PromptForMultipleSelection()` (`gui.App.ChooseTraces`) |
| Drop onto the window | window option `EnableFileDrop: true`; files dropped on an element with the attribute `data-file-drop-target` arrive as the window event `events.Common.WindowFilesDropped` with `DroppedFiles()` (the empty state is such an element) |
| "Open with" | `events.Common.ApplicationOpenedWithFile` with `Filename()`. macOS: the bundle's `CFBundleDocumentTypes` (`packaging/macos/Info.plist`, `.pcap`, `.pcapng`, `.cap`, role Viewer); Windows and Linux: `application.Options.FileAssociations`, matched against the command line |
| Command line | `modbus-trace-explorer trace.pcapng ...`, the arguments after the flags |

Seen working at run time on macOS *(M0: not yet; the application was built but not started in a window during M0)*; the A15 smoke test (M4) starts it on every platform. If file drop proves unreliable in a WebView, the dialog and the recent files remain the guaranteed paths.

## 13. Security and privacy

- **No network.** No Go network client (lint + dependency test), CSP `default-src 'self'` with no remote origins in the WebView, nothing fetched at run time.
- **Untrusted input.** Traces come from third parties. The library is fuzzed and bounded; the application adds: a free-space check before decompression, a size cap on the cache, no execution or interpretation of trace contents, and no rendering of trace-supplied strings as HTML (all text is rendered as text; device identification strings from FC43 are untrusted text).
- **Paths.** The application opens only paths the user chose (dialog, drop, argument, recent list). It resolves symlinks, requires a regular file, and reads it read-only. Exports go through a temporary file next to the destination and a rename; an output equal to an input is refused.
- **Sensitive data.** Traces contain operational data (addresses, register values, writes). The cache holds only metadata and references, 0700/0600. Logs never contain trace content or, at info level, addresses and paths. Exports can contain addresses and register data; the export dialogs say so once.
- **Supply chain.** Dependencies pinned and recorded; `govulncheck` and the lockfile audit in CI; provenance attestations on releases.

## 14. Build, packaging, dependencies

| Item | Decision |
|---|---|
| Go | `go 1.25.0` as the Explorer (Wails v3 needs it); native runners. Dependencies are pinned at versions that keep 1.25: `golang.org/x/sys` v0.46.0 and `modernc.org/sqlite` v1.53.0 in the spike's test (v1.60 needs Go 1.26) |
| Wails | `github.com/wailsapp/wails/v3 v3.0.0-beta.29` pinned (latest v3 tag on 2026-10-15 *(verified in the Explorer's go.mod and the proposal's check)*); `@wailsio/runtime` aligned |
| Library | `github.com/otfabric/go-modbus-trace` at a tag or pseudo-version; `go-modbus-capture` arrives through it (direct import only if the `.mbcap` viewer, R24, is built) |
| Index | own column files (section 5); `modernc.org/sqlite` v1.53.0 only in the spike's test, to keep the measurement repeatable |
| Schemas in tests | `github.com/santhosh-tekuri/jsonschema/v6` as the Explorer |
| Frontend | section 11; no charting, no virtualization, no icon library. The dependency set is the SunSpec Explorer's lockfile (React 19.3, Vite 8, Tailwind 4.3, TypeScript 7, Vitest 5, `@wailsio/runtime` 3.0.0-beta.29), installed from it unchanged *(M0)* |
| Targets | `make build-gui` per platform; macOS bundle (ad-hoc signed) via `packaging/macos/bundle.sh`; Windows resources (icon, manifest); Linux binary, deb/rpm/AppImage later |
| CI | `build.yml` reusable, `ci.yml`, `release.yml` as the Explorer's; matrix on native runners; checksums and provenance attestations; manual Release workflow with `bump` |
| Icon | `website/public/icons/go-modbus.svg` (teal tile) with the `<metadata>` element stripped; rendered by `packaging/icons/render_appicon.py` |

## 15. Reuse from the SunSpec Explorer

Copied at commit 915d2c3 (tag v0.0.3), adapted, and listed here so a later extraction of a shared module is cheap. The Explorer is not imported and no shared module exists.

| Area | Files (here) | From the Explorer | Changes |
|---|---|---|---|
| Repo hygiene | `Makefile` | `Makefile` | GUI only; `mod-check`, `frontend-urls`, `spike`; coverage packages |
| | `.golangci.yml` | `.golangci.yml` | read-only bans replaced by the network and Modbus bans (`depguard`, `forbidigo`) |
| | `codecov.yml`, `LICENSE`, `.gitignore` | same | coverage packages; dist and `go.work` ignored |
| | `.github/workflows/build.yml`, `ci.yml` | same | no daemon or image jobs; the release workflow follows in M4 |
| Wails shell | `cmd/modbus-trace-explorer/main.go` | `cmd/sunspec-explorer/main.go` | file drop and "open with" |
| | `internal/gui/app.go` | `internal/gui/app.go` | trace dialogs and handed-over paths instead of sessions and TLS |
| | `internal/buildinfo/buildinfo.go` | same | any module's version |
| | `frontend/embed.go` | same | one build (no web build) |
| | `packaging/macos/bundle.sh`, `Info.plist` | same | names; document types |
| | `packaging/icons/render_appicon.py` | same | the Modbus glyph (a rounded grid) |
| Frontend shell | `frontend/vite.config.ts`, `tsconfig.json`, `package.json`, `package-lock.json` | same | Wails build plus a `mock` dev mode; no web build |
| | `src/styles/{tokens,fonts,index}.css` | same | none |
| | `src/app/components/{Dialog,ConfirmDialog,ui}.tsx` | same | none |
| | `src/app/components/Header.tsx` | same | Modbus icon and name, no mode badge |
| | `src/app/icons/index.tsx` | same | trace-viewer icons added |
| | `src/app/platform/{platform,themeStorage}.ts`, `src/app/state/useTheme.ts` | same | trace dialogs and "open" events instead of TLS and saving |
| | `src/adapters/wails/wails.ts` | same | the new binding |
| | `src/assets/logo-*.svg`, `src/test/setup.ts` | same | `<metadata>` and the C2PA namespace removed |
| Not copied | everything SunSpec-, polling-, session-, daemon-, web-adapter-, TLS- or recording-specific | | |

## 16. Milestones

| M | Scope | Needs from the library | Exit criteria |
|---|---|---|---|
| M0 | Repo scaffolding and CI from the Explorer; Wails shell opens an empty window with branding; mock Service with a fixture dataset; **store spike** (SQLite vs columnar, measured on a synthetic 10M-row dataset); **table spike** (scaled scroll in the three WebViews); Wails file-drop/open-with check | nothing | decisions and measured budgets recorded in this file; A14 skeleton passes |
| M1 | Open, Scan, Flows step, Analyze into the cache, timeline, detail pane, filters, overview strip, cache lifecycle | L1-L2 events, `Frames`, `Decode` (C1-C4, C10) | A1-A3, A5, A6, A11, A12, A13 |
| M2 | Devices, Conversations, Patterns, Addresses, Quality; schemas | C5, C6 (library L4 analyzers) | A7, A8, A18, A4 on the real index |
| M3 | MBCAP/timeline/sub-trace exports, preview, replay hand-off, settings | C7-C9 (library L3, L4) | A9, A10, A16 |
| M4 | Packaging (unsigned), documentation, polish, optional `.mbcap` viewer (R24) | - | A15, A17; R25 delivered; first release |

**M0 outcome** (2026-10-16): scaffolding and CI copied from the SunSpec Explorer (section 15); the Wails shell with branding, the Modbus app icon, file drop, "open with" and command-line paths (section 12); the frontend shell (header, empty state, file strip, view rail, detail pane, status bar, keyboard map for views and time modes) over a mock Service with a generated 10-million-row dataset; the scaled-scroll timeline table (section 11); the store spike with a recommendation (section 5); the A12 dependency test and the bundle URL check; the A14 skeleton (no color outside `tokens.css`, contrast of every token pair the application uses). Decided with the user: the columnar index (section 5). Open: the WebView checks in the running application (M1).

The UI work in M1-M3 can start before the library is ready by developing against the mock Service; integration happens when the library publishes the corresponding milestone (L2 for M1, L4 for M2/M3).
