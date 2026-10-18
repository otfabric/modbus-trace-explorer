# Architecture

Status (2026-10-16): **M0 and M1 are implemented** (section 16). Statements about M1 and later are *proposed behavior* unless marked *(verified)* (checked in the local checkouts of sunspec-explorer v0.0.3, go-modbus-capture v0.0.2, modbusctl v3.2.0, go-modbus v1.7.1 and go-modbus-trace at main `119d699` on 2026-10-15/16) or *(unverified)* with the milestone that must check them.

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

*(M1 and M2: implemented for opening, importing, the timeline, the detail, the filter text and the views: `internal/service/service.go` is the authority, its DTOs have JSON Schemas in `docs/schema/` generated from the Go types and checked against the engine's real output, and `frontend/src/app/service/types.ts` mirrors them. Exports and settings (M3) follow.)* The original sketch, kept for the parts still to come:

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

Budgets (R21; the M0 numbers are above, the M1 numbers below): index at most 40% of the trace size (restated by the user on 2026-10-16 from the M1 measurements; proposed was 25%); at most about 110 bytes per timeline row plus its provenance references (8 to 24 bytes); 10 million rows paged, filtered and sorted in under 150 ms at the 95th percentile; page cache bounded (64 MiB).

Layout as built in M1 (`internal/store`), one directory per trace fingerprint, `os.UserCacheDir()/modbus-trace-explorer/ix-<fingerprint>/`, written as `ix-<fingerprint>.partial/` and renamed when complete:

| File | Content |
|---|---|
| `meta.json` | format version, library version, event-semantics version, fingerprint, paths at import, rows, first and last time, remembered choices (`service.AnalyzeConfig`), `complete` (written last) |
| `summary.json`, `conns.json` | the library's file summary; every connection as last seen (`model.Connection`) |
| `devices.json`, `clients.json`, `summaries.json`, `extra.json` | dictionaries: device entries (server endpoint and unit; an entry with the server only, marked `#`, stands for a whole server and carries its connection, gap and resync rows, so a server filter keeps them; entry 0 is "none" for notes), client addresses, the transactions' summary texts (deduplicated, at most 1,048,576), and the small JSON of every row that is not a transaction |
| `col/<name>.bin` | one fixed-width little-endian column per field, row `i` at offset `i * width`: `kind` (u8), `t_ns` (i64), `conn` (u32), `device`, `client`, `tid` (u16), `fc` (u8), `sub` (u16, 0xFFFF none), `class`, `status`, `exc` (u8), `flags` (u32 bitmask), `space` (u8), `start`, `count`, `wstart`, `wcount` (u16), `lat_us` (u32, 0xFFFFFFFF none), `mbcap` (u8: the `Classify` reason), `summary` (u32: a transaction's request summary, another row's extra data), `bytes` (u16: the request's and the response's ADU bytes), `resp_hash` (u16: a hash of the response PDU, for the patterns' changed answers), `refs` (u64: the offset of the row's provenance in `refs.bin`). 59 bytes a row (format version 2) |
| `refs.bin` | every row's provenance: the request's and the response's frame references (part, frame, file offset, interface, time, payload offset and length), varint-encoded as differences to the previous reference of the row and to the row's time, about 26 bytes a transaction |
| `perm/time.bin`, `perm/latency.bin` | u32 row numbers in time order (`t_ns`, then row) and in latency order (`lat_us`, then row), sorted at the end of the import with the columns mapped, so sorting takes 4 bytes a row of heap |

Raw ADU bytes and decoded text are **not** stored; the row summary string is built on demand for the rows being displayed. The columns are memory-mapped for queries (`syscall.Mmap` on macOS and Linux, `golang.org/x/sys/windows` file mappings on Windows); without a mapping they are read in chunks.

`mbcap_reason` is the library's `Classify` result for the baseline export (exceptions on, no change-only), stored at import. It makes the export preview a count over one column instead of a pass over the trace. Options that change the outcome (exceptions off, change-only) are applied to the preview arithmetically, and "up to" is shown where it cannot be exact.

Cache management (M1): the fingerprint is a SHA-256 over the store's format version, the library version and its event semantics, and per file the size, modification time and the first and last MiB; a change to any gives a new directory, so a stale index is never read. A small `index.json` in the cache root lists the entries (`ix-<fingerprint>` indexes and `gz-<fingerprint>` decompressed traces) with their last use; eviction removes the least recently used entries that are not held by an open trace until the cache is under its cap (default 2 GiB); "Clear cache" removes everything not in use; partial entries left by a crash are removed at start. Directories 0700, files 0600. The cache never contains packet bytes, only references (a decompressed gzip trace is a copy of the user's file, an entry of its own that lives only while the trace is open (A16, M3)).

**M1 measurements** (Apple M1, `make scale`, Go 1.27.2): a generated trace of 1,000,000 transactions (272 MiB, 40 connections, no ACK-only packets) opens, scans, analyses and indexes in 3.5 s, **82 MB/s end to end** (R21: 50); the index is 85 MiB, **89 bytes a row, 31% of that trace**; the Go heap after the import is 1 MiB. Queries: first page 0.19 ms, last page 0.15 ms, a new filter (a scan of the columns) 2.7 ms, a filter on unit and function at a rank in the middle 10 ms, latency order 6 ms. A real capture of small polls (`MTE_TRACE`, 330 KB, 1,327 transactions, about 250 bytes a transaction) gives 91 bytes a row, **37% of the trace**. The proposed 25% was therefore not met for traces of small polls: the provenance needed to re-read a row's bytes (two packets) and the row's own fields take about 80 bytes whatever the payload size, and the ratio falls with larger payloads and ACK packets (estimated, not measured: about 15% for a pcapng capture with ACK packets and 125-register reads). The user restated the budget as 40% on 2026-10-16; narrower columns stay an option if a real trace needs them.

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

*(M2, as built: the candidates are found by one scan of the device, status, space, start, count, wstart and wcount columns in time order (there is no separate address index); the batch is read in time order without sorting by file offset, and points stream every 2,000 points or 200 ms. The coverage map is computed by a scan of the same columns under the active filter, 5 ms at 1,000,000 rows. Devices, Conversations, Patterns and Quality are computed from the columns at query time too, not stored at import with the library's analyzers (C6): that way every view follows the filter (R7), and a test (A7) checks the numbers equal the library's analyzers on the same trace. Patterns group by client, device, function and the request summary (which names the address range), and count a response as changed when its 16-bit PDU hash differs from the previous one.)*

## 9. Exports

All three exports follow the same shape: the dialog builds a `Filter`, the engine maps it to a library `Selection`, then **runs the library exporter over the trace** with progress and cancellation, writing to `dest` through a temporary file and rename. The index is used only for previews and counts.

- **MBCAP**: `mbcapexport.Export` with `Options` from the dialog, `Producer = "modbus-trace-explorer <ver>"`, labels from the dialog, tick resolution, encoding (the binary writer with the CLI's defaults: deflate, 256 samples a block). The report is shown and can be saved as JSON. GUI and CLI call the same function with the same selection and analysis options, so the samples, targets, streams and report are the same; the files differ only in the header's producer and provenance step, which name the tool that wrote them (A9 is tested as byte-identity with the library's `Export` given the application's options). *(M3 as built.)*
- **Timeline**: the library's CSV/JSONL writers.
- **Sub-trace**: the library's pcapng extractor.

Replay hand-off (R16) is text built from the destination path and the report's targets.

*(M3, as built.)* The filter maps to `selection.Selection` term by term; the two terms the library's selection does not have, exception codes and a pattern's request summary, are refused with a message rather than dropped, and an address range needs a data space. Exports use the trace's remembered analysis choices (`trace.Options`), so they see the transactions the index holds. The **preview** counts from the index: the stored `Classify` reason of every transaction, then the exporter's own order (exceptions off, not selected, outside the window), and the targets and streams in the order the exporter creates them; it equals the report except for change-only (an upper bound, said as "up to") and `invalid_time` (a response without a timestamp, known only to the export, said in a note). Every export runs as a job with progress and cancel, writes through a temporary file beside the destination that is renamed when complete (removed on error or cancel), refuses a relative path, a missing folder and the trace's own files, and keeps its result (`ExportResult`) for the dialog. Settings (R20) are a JSON file in the configuration directory; the cache cap applies at once. A gzip trace's decompressed copy lives only while the trace is open (A16, confirmed by the user on 2026-10-16): closing the trace or quitting removes it; reopening an analysed gzip trace reuses the analysis and runs a `Prepare` job that decompresses again (with the free-space check, progress and cancel) before detail, address history and exports read the bytes.

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

Copied and adapted from the Explorer (section 15). Window title "Modbus Trace Explorer", default size 1440x900, minimum 1024x640 (remembered size and position: M1). Native menu (M4, as built): File (Open…, Open Recent, Close Trace, Export…, Settings…), Edit, View (switch theme, next time display, detail pane, full screen), Help (About, Documentation), and the macOS application and Window menus. The items send an action to the window (`explorer:menu`), which owns the state; they carry no accelerators, because the window handles the keys (UX.md section 9) and an accelerator would fire the same action twice. Open Recent is rebuilt whenever the list changes. The window size and maximised state are kept in `window.json` in the configuration directory; the position is not, so the window never opens on a screen that is gone. File-open entry points, all funnelled into `gui.App.Handed`, which keeps only regular files (absolute, symlinks resolved) and announces them to the window with the event `explorer:open`; paths that arrive before the window is ready wait in `PendingFiles`:

| Entry point | Wails v3 beta.29 mechanism *(verified in its source, M0)* |
|---|---|
| Open dialog | `app.Dialog.OpenFile()...PromptForMultipleSelection()` (`gui.App.ChooseTraces`) |
| Drop onto the window | window option `EnableFileDrop: true`; files dropped on an element with the attribute `data-file-drop-target` arrive as the window event `events.Common.WindowFilesDropped` with `DroppedFiles()` (the empty state is such an element) |
| "Open with" | `events.Common.ApplicationOpenedWithFile` with `Filename()`. macOS: the bundle's `CFBundleDocumentTypes` (`packaging/macos/Info.plist`, `.pcap`, `.pcapng`, `.cap`, role Viewer); Windows and Linux: `application.Options.FileAssociations`, matched against the command line |
| Command line | `modbus-trace-explorer trace.pcapng ...`, the arguments after the flags |

Seen working at run time on macOS (WKWebView) by the user on 2026-10-16: a `.pcapng` dropped on the empty state reached `Handed` and the window. Windows and Linux are checked by hand in M1 and by the A15 smoke test (M4). If file drop proves unreliable in a WebView, the dialog and the recent files remain the guaranteed paths.

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
| CI | `build.yml` reusable, `ci.yml`, `release.yml` as the Explorer's; matrix on native runners; checksums and provenance attestations; manual Release workflow with `bump` *(M4: built; every desktop build runs the A15 smoke test, `-smoke testdata/smoke.pcapng`)* |
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
| Library code | `internal/schemagen/` | go-modbus-trace `internal/schemagen` at main 5fc5583 | none (copied: it is internal there) |
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

**v0.2, the test corpus** (2026-10-18): `testdata/corpus` holds about 60 synthetic files written by `internal/corpus` (`make golden`), each described in its `manifest.json` (and `README.md`) with how the application must take it: **realistic** (a 15-minute plant with three devices, two clients, writes, exceptions, an outage, a reset; the same gzipped, as a three-file ring buffer, and exported to MBCAP in three encodings and change-only; an RTU gateway; a function-code tour), **edge** (midstream, a non-standard port, losses and retransmissions, IP fragments, a snap length, pairing trouble, a clock stepping back, 300 connections, IPv6, TLS, two interfaces with drops, every link type and file variant, simple packets, unusual captures) and **broken** (empty, garbage, text, an unknown link type, cut files and gzip, a 2-GiB record, corrupt and misnamed captures, four of go-modbus-capture's own corrupt vectors). Held three ways: `TestCorpus` takes every file through everything the window does (open, scan and analyse or import, timeline in both orders and directions, detail, every view, coverage and histories, quality, diagnostics) and checks the manifest's outcome, transactions, warnings and quality counters; `FuzzOpen` mutates the smaller files through the same path (seeds in every `go test`, `make fuzz` for longer); `make smoke-corpus` (and CI on Linux) runs the built application on every file. The engine's answers for three of them are replayed through the UI (`frontend/src/app/corpus.test.tsx`): every row's detail on every tab and every view. **Changed on the way:** a capture damaged after its first samples, and a gzip trace that ends early, now show what could be read with a warning instead of failing (as a cut trace always did); the smoke mode reports an empty timeline instead of waiting for rows.

**v0.2, captures (R24)** (2026-10-18): a file whose name ends in `.mbcap`, `.mbcap.jsonl`, `.jsonl`, `.mbcap.csv` or `.csv`, or that starts with the MBCAP magic, opens as a capture (`FileInfo.kind`), on its own. `ImportCapture` reads it with `go-modbus-capture`'s readers into the same columnar index: one transaction row per sample (time from the header's start and tick; the target as the device, `host:port` for a network target; the stream as the connection, its client from the label `client <address>` the exporter writes; function, class, status, ranges and request summary from `mbtcp.Decode`; no latency). **Deviation from section 5:** a capture's entry keeps the samples' PDUs (`pdus.bin`, with a u64 offset per row in `pdus.idx`), because JSON Lines and CSV cannot be read at a sample's position and the detail and the address history need the bytes; it is the content of the user's own file, as a decompressed gzip trace is. The detail rebuilds the MBAP header (transaction ID 0) and says so. The views run over the index as for a trace; Quality shows the capture's facts (encoding, producer, tick, start, provenance, change-only, a missing end, a cut file). Scan, Analyze, the exports, and go to frame refuse a capture with a message. `.mbcap` is a document type of the macOS bundle and a file association.

**v0.2, first part** (2026-10-18): the status bar lists the jobs that run beside the open trace (exports, address histories) with progress and Cancel, the cache's size (read again when a job ends) and the time zone offset of local time; an export can continue in the background. The detail pane is resizable (a `separator` with pointer and keyboard control, 280 to 800 px, kept in the window's storage like the theme). The frontend's coverage (Vitest, v8) goes to Codecov with the flag `frontend`; `codecov.yml` maps its paths, which are relative to `frontend/`.

**M4 outcome** (2026-10-16): the native menu and the remembered window size (section 12); the filter popover (its controls are written as grammar text and parsed by the engine, so the two paths cannot disagree); the capture-facts popover; go to time and go to frame (`Locate`, which also gives N and P: the next or previous row of the same connection); Enter to the detail pane, Ctrl/Cmd+C and Ctrl/Cmd+Shift+C; byte selection and C/Python byte strings in Raw; Help → About with "Copy diagnostics" (versions and facts, no paths or addresses, R23); several files ordered by their first packet. A15: a `-smoke` mode and a CI step after every desktop build (section 14). A17: the Service contract suite, the mock and a fixture of the engine's own answers validated against the schemas in the frontend's tests (a small validator for the subset `schemagen` writes, no dependency), and the fixture replayed through the UI. Release: the Release workflow, `RELEASE.md` with the v0.1.0 notes, install notes in the README. **Fixed on the way:** unit IDs, function codes and exception codes in a `Filter` travelled from Go as base64 (Go writes a `[]uint8` as a string), so a filter the engine sent back (the parsed text box, a Quality condition's "show in timeline") was unreadable in the window; `service.Uint8s` writes and reads them as numbers and declares its schema to `schemagen`. A scan or analysis that ended before the window knew the job's ID (a broken file fails in milliseconds) left the window waiting for ever; the window now keeps such final events until the job is known. Deferred by the user: opening `.mbcap` files (R24) to v0.2.

**M3 outcome** (2026-10-16): the Export dialog (Ctrl/Cmd+E or the file strip) with the MBCAP capture (scope: everything, the current filter or one device; a window in seconds after the first packet; exceptions, change-only, tick, encoding; editable target labels; the live preview with the reason and meaning of every exclusion), the timeline (CSV, JSON Lines) and the sub-trace (pcapng, with the note that writes and vendor functions are included verbatim), each a cancellable job through the library into a native Save dialog's path; the result shows the report (counts, targets, warnings), saves it as JSON, and gives the replay hand-off (`mbcapctl verify`, one `modbusctl server replay` per target, the caveats, the walkthrough link). Settings (Ctrl/Cmd+, or the header): theme, time display, server ports added to the Flows step, 1-based address labels, the cache's location, size cap and "Clear cache", and "Clear recent files". Tests: A9 (preview equals report for four option sets; the file equals the library's export byte for byte; the binary file reads back through the capture library's checker), A10 (timeline equal to the library's writer for CSV and JSON Lines; a sub-trace reopened shows the same transactions), refusals and atomic writes, settings across a restart, the gzip copy removed on close and made again on reopening (A16), schemas for every new DTO (A18), and the dialogs in the UI against the mock. Not covered here: running the pinned `mbcapctl verify` and `modbusctl server replay` binaries (the library's LA10 does that for the same exporter).

**M2 outcome** (2026-10-16): the Devices view (per server and unit: functions, statuses, reads and writes, exceptions, unanswered, clients, rate, bytes, exact latency quantiles with a histogram, "show in timeline"), the Conversations view (connections with role source and confidence, open and close, duration, gaps, flags, and the selected connection's own timeline), the Patterns view (recurring requests per client and device with the interval distribution, jitter as the median absolute deviation, the share of changed answers and the static/slow/noisy class, "show in timeline" by request summary), the Addresses view (device and space, a coverage strip with sparse ranges compressed, reads solid and writes hatched copper, the value history of one address as a cancellable streaming job with a cap and "load more", writes marked, hex/decimal/binary, 1-based labels, the "observed, not a complete register map" note), and the Quality view (every library counter with its meaning, "none" for zero, "show in timeline" where a filter exists, and the capture facts). Every chart has a table alternative. "Show in timeline" narrows the shared filter, which applies to every view (R8's "selecting a row filters the other views"). Tests: A7 against the library's analyzers on a scripted trace, A8 (a scripted value sequence, a write, the cap and its continuation), A18 for the new DTOs, the views in the UI against the mock. **M2 measurements** (`make scale`, 1,000,000 transactions): Devices 55 ms, Conversations 13 ms, Patterns 24 ms, coverage 5 ms, Quality under 0.1 ms; an address history reads about 12,500 points a second (25,000 points in 2 s), so the default cap of 200,000 points takes about 16 s, progressively and cancellable. That rate is bound by the library's `trace.File.Frames`, which opens the trace for each frame; a handle kept across one call would raise it. The index grew by the two new columns to 32.6% of that trace (R21: 40%).

**M1 outcome** (2026-10-16): opening (dialog, drop, "open with", command line, recent files), gzip decompression into the cache with a free-space check before and while writing, the scan job and the Flows step (Modbus flows preselected, server ports prefilled, a role override per flow as an endpoint override, TLS and "no Modbus" explained), the analysis job into the columnar index with progress and cancel, the cache lifecycle (fingerprint, reuse, LRU, clear, partial cleanup, permissions), the overview strip with the quality badge, the timeline over the real index (paging by rank, time and latency order, both directions), the filter text grammar of UX.md section 6 parsed in Go with chips and error chips, the detail pane (summary in words, request and response fields with byte ranges, the hex dump with field highlighting, the packets with Wireshark filters, notes with meanings and the MBCAP classification), the rotating log file (R23), JSON Schemas for every DTO validated against real engine output (R22, A18), and tests for A1–A3, A5, A6, A11, A13 with the library's `tracetest`. Not in M1 (see REQUIREMENTS.md statuses): the native menu and remembered window geometry, the filter popover, "go to time/frame", the capture-facts popover of the file strip, "Copy diagnostics"; the Quality view and the other views were M2.

**M0 outcome** (2026-10-16): scaffolding and CI copied from the SunSpec Explorer (section 15); the Wails shell with branding, the Modbus app icon, file drop, "open with" and command-line paths (section 12); the frontend shell (header, empty state, file strip, view rail, detail pane, status bar, keyboard map for views and time modes) over a mock Service with a generated 10-million-row dataset; the scaled-scroll timeline table (section 11); the store spike with a recommendation (section 5); the A12 dependency test and the bundle URL check; the A14 skeleton (no color outside `tokens.css`, contrast of every token pair the application uses). Decided with the user: the columnar index (section 5). Open: the WebView checks in the running application (M1).

The UI work in M1-M3 can start before the library is ready by developing against the mock Service; integration happens when the library publishes the corresponding milestone (L2 for M1, L4 for M2/M3).
