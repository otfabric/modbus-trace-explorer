# Testing

No devices and no network: Go tests build their traces with go-modbus-trace's `tracetest` or use the synthetic corpus in `testdata/corpus`. The tests that need a window run in CI on each desktop platform. `docs/ARCHITECTURE.md` section 16 describes the corpus.

## Tiers

| Tier | Command | What | In CI (`.github/workflows/build.yml`) |
|---|---|---|---|
| Go tests with race | `make test` | The `TEST_PKGS` in the Makefile | yes |
| Coverage gate | `make coverage-pkg-check` | `TEST_PKGS`, at least `COVERAGE_MIN` (85%) in total and `COVER_PKG_MIN` per package | yes |
| End-to-end | `make e2e` | `e2e/`: `service.Service` and its events on trace files: import, views, exports, bad input | yes, in `make test` |
| Frontend | `make frontend-check` | Typecheck and Vitest | yes, as `make frontend-coverage` |
| Frontend URLs | `make frontend-urls` | Fails on an external URL in the bundle (A12) | yes |
| Fuzz | `make fuzz` (`FUZZ`, default `FuzzOpen`; `FUZZTIME`, default 5m) | `internal/engine`: `FuzzOpen` on mutated corpus files, `FuzzParseFilter` on the filter box's text (see Fuzzing below) | yes, `FuzzOpen` with `FUZZTIME=10s`; the seeds of both run with `make test` |
| Smoke | `make smoke` | The built application on `testdata/smoke.pcapng` (A15) | yes, each desktop target |
| Corpus in the application | `make smoke-corpus` | Runs `scripts/smoke_corpus.py`: every corpus file in the built application, checked against its manifest | yes, Linux |
| Scale | `make scale` (`SCALE`, default 1,000,000 transactions) | Imports a generated trace and times it (R21, A4) | nightly |
| Store spike | `make spike` (`SPIKE_ROWS`, default 10,000,000) | The M0 measurement in `docs/ARCHITECTURE.md` section 5 | nightly |

`make check` runs fmt, fmt-check, mod-check, vet, lint, lint-ci, vuln, test, coverage-pkg-check and frontend-check. `make test` does not need a frontend build; the smoke targets build the application and need its prerequisites (`CONTRIBUTING.md`).

## Coverage policy

`make coverage-pkg-check` also fails when one package is below `COVER_PKG_MIN` (85). `TEST_PKGS` lists `internal/engine`, `service`, `store`, `buildinfo`, `applog` and `schemagen`. `internal/gui` and `cmd/` are outside the gate: the Wails shell needs a window (`codecov.yml`). `internal/policy` and `internal/corpus` have tests of their own but are not in `TEST_PKGS`. Run them with `go test ./internal/policy ./internal/corpus`. Codecov receives the Go profile and the frontend's lcov report under separate flags.

## Guards

`internal/policy` (`TestNoNetworkNoModbusNoCgo`) checks the dependency graph, and `.golangci.yml` bans network use in Go. `make mod-check` fails on a `replace` directive. `TestServiceContract` holds the service contract, and `TestSchemasUpToDate` the DTO schemas in `docs/schema/`.

## Fuzzing

`FuzzOpen` takes each input through everything the window does with a file (`runCorpusFile`) and then checks what holds for the index of any input (`invariants` in `internal/engine/corpus_test.go`): the overview, the index and the timeline agree on the row count; the time and the latency order are each a permutation of the rows, sorted by their key and then by row; the timeline's totals for each status, in time and in latency order and from either end, are the numbers of its rows and of those with a latency; a split at a time parts the rows; and every frame reference of a trace decodes and names a frame of one of its files. `TestCorpus` checks the same on every corpus file, so every corpus file is a seed of the invariants. `FuzzParseFilter` parses any text, writes what it took back as filter text and requires it to parse again, without an error, to the same filter.

## Lifecycle contract matrix

The long-lived types are `engine.Engine` (open traces, jobs, `Subscribe`), `store.Index` (`Acquire`, `Release`, `Close`, `Retire`), the live snapshots of an import (`internal/engine/live.go`), `store.Cache` (`Hold`, `Evict`, `Clear`) and `applog.File`. The application has no network peer, so the rows about peers do not apply. The tests below run with `-race` in `make test`, and the engine's `TestMain` checks for leaked goroutines.

| Row | What holds | Tests | Gap |
|---|---|---|---|
| L1 Close twice | `Index.Close` after `Close` or `Retire` returns at once; the retire callback runs once | `TestRetire` | `Engine.Shutdown` twice is exercised by test cleanups, not asserted. A second `applog.File.Close` returns the `os` error. |
| L2 Two goroutines call Close | Two `Index.Close` calls both return once the index is unmapped | `TestRetire` | `Engine.Shutdown` has no closed state: a job started after it is not cancelled. No test. |
| L3 Close during a blocked read | Closing a trace, the swaps of a live import and the preparation of a gzip trace while queries run | `TestQueriesDuringClose`, `TestQueriesDuringLiveImport`, `TestQueriesDuringPrepare` | — |
| L4 Close during a write | Open of a trace that is already open cancels its jobs and waits for them, as Close does; the new analysis ends `done` | `TestReopenDuringAnalysis` | Close during an analysis takes the same path and has no test of its own. |
| L5 Close during handshake | n/a: no handshake | — | — |
| L6 Cancel a pending request | `CancelJob` at a fixed point of the analysis (a test gate) ends it `canceled`, with no index and no `.partial` or `.live-` directory left; a live import cancelled while an address history reads one of its snapshots ends without waiting for the history | `TestCancelAnalysis`, `TestHistoryDuringLiveImport` | — |
| L7 / L8 Late response, response after reconnect | n/a: no peer | — | — |
| L9 / L10 Callback calls a public method, or Close | n/a: the library's progress callbacks only update the job; events go out over channels | — | — |
| L11 Slow callback or consumer | A query or history still reading a retired snapshot does not hold up the import or its Cancel: the snapshot is unmapped when its last reader releases it | `TestRetire`, `TestHistoryDuringLiveImport` (not on Windows, where the end of the import waits for the snapshots) | A must-deliver event waits up to 5 s for each subscriber, then is dropped without a count. No test. |
| L12 Queue at capacity | A subscriber channel holds 64 events; progress events are dropped when it is full | — | No counter, and no test that a must-deliver event still arrives when the channel is full. |
| L13 Concurrent sessions | Two traces open at once; the same trace opened twice | `TestQueriesDuringClose`, `TestReopenDuringAnalysis` | — |
| L14 Failure during construction | A cache that cannot be created, an incomplete index, a snapshot or a writer that fails | `TestNewCacheRefused`, `TestOpenRefusesIncomplete`, `TestLiveSnapshotFails`, `TestWriterErrors` | — |
| L15 Shutdown | `Shutdown` cancels every job and waits for it | — | No bound on the wait, and no test. |
| L16 Close racing reconnect | n/a | — | — |
| Cache `Hold` against `Evict` and `Clear` | An open trace's index and the decompressed copies of its gzip files stay, also when they are larger than the cap, and go once the trace is closed | `TestCacheReuse`, `TestGzip`, `TestHoldKeepsDecompressedCopies`, `TestGzipCopyHeldWhileOpen` | — |

## Environment variables

| Variable | Effect |
|---|---|
| `MTE_UPDATE` | Rewrites golden files. Use `make golden`, which regenerates the corpus, the DTO schemas, the engine fixtures and the smoke trace |
| `MTE_SCALE` | Enables the scale test (`make scale` sets it, so the nightly job cannot skip it) |
| `MTE_TRACE` | Path of a trace for the scale test's import of a real file; skipped without it |
| `MTE_SPIKE_ROWS` | Enables the store spike (`make spike` sets it, so the nightly job cannot skip it) |
| `MTE_FUZZ_SLOW` | Directory where `FuzzOpen` keeps inputs that took more than 2 seconds |

## Replaying a failure

A failing fuzz input lands in `internal/engine/testdata/fuzz/<target>/` (`FuzzOpen` or `FuzzParseFilter`). Commit it with the fix: `go test ./internal/engine` replays it. `scripts/smoke_corpus.py` prints the application's whole output on failure and takes `--only SUBSTRING` and `--repeat N`.

## When CI runs

| Event | Workflow | What runs |
|---|---|---|
| Pull request | `pr.yml` | `policy.yml` (otfabric-policy) always; `ci.yml` unless every changed file is documentation; then `OT Fabric Required Checks` |
| Push to a branch | none | nothing; dispatch `ci.yml` to test a branch without a pull request |
| Push to `main` | `policy.yml` | the light checks; the merged commit was tested as the pull request's merge commit |
| Schedule | `nightly.yml` | the scheduled runs, on `main` |
| Release | `release.yml` | the release workflow tests the release commit again |

Documentation means an added or modified `*.md` file outside `.github/`, `testdata/` and `vendor/`, other than `AGENTS.md` and `CLAUDE.md`. A deleted or renamed document, or any other file, runs everything. `OT Fabric Required Checks` fails unless every job that applies succeeded; its summary lists them.
