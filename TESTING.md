# Testing

No devices and no network: Go tests build their traces with go-modbus-trace's `tracetest` or use the synthetic corpus in `testdata/corpus`. The tests that need a window run in CI on each desktop platform. `docs/ARCHITECTURE.md` section 16 describes the corpus.

## Tiers

| Tier | Command | What | In CI (`.github/workflows/build.yml`) |
|---|---|---|---|
| Go tests with race | `make test` | The `TEST_PKGS` in the Makefile | yes |
| Coverage gate | `make coverage-check` | `TEST_PKGS`, at least `COVERAGE_MIN` (85%) | yes |
| Frontend | `make frontend-check` | Typecheck and Vitest | yes, as `make frontend-coverage` |
| Frontend URLs | `make frontend-urls` | Fails on an external URL in the bundle (A12) | yes |
| Fuzz | `make fuzz` (`FUZZTIME`, default 5m) | `FuzzOpen` in `internal/engine`, on mutated corpus files | yes, `FUZZTIME=10s` |
| Smoke | `make smoke` | The built application on `testdata/smoke.pcapng` (A15) | yes, each desktop target |
| Corpus in the application | `make smoke-corpus` | Runs `scripts/smoke_corpus.py`: every corpus file in the built application, checked against its manifest | yes, Linux |
| Scale | `make scale` (`SCALE`, default 1,000,000 transactions) | Imports a generated trace and times it (R21, A4) | no |
| Store spike | `make spike` (`SPIKE_ROWS`, default 10,000,000) | The M0 measurement in `docs/ARCHITECTURE.md` section 5 | no |

`make check` runs fmt, fmt-check, mod-check, vet, lint, lint-ci, vuln, test, coverage-check and frontend-check. `make test` does not need a frontend build; the smoke targets build the application and need its prerequisites (`CONTRIBUTING.md`).

## Coverage policy

`TEST_PKGS` lists `internal/engine`, `service`, `store`, `buildinfo`, `applog` and `schemagen`. `internal/gui` and `cmd/` are outside the gate: the Wails shell needs a window (`codecov.yml`). `internal/policy` and `internal/corpus` have tests of their own but are not in `TEST_PKGS`. Run them with `go test ./internal/policy ./internal/corpus`. Codecov receives the Go profile and the frontend's lcov report under separate flags.

## Guards

`internal/policy` (`TestNoNetworkNoModbusNoCgo`) checks the dependency graph, and `.golangci.yml` bans network use in Go. `make mod-check` fails on a `replace` directive. `TestServiceContract` holds the service contract, and `TestSchemasUpToDate` the DTO schemas in `docs/schema/`.

## Environment variables

| Variable | Effect |
|---|---|
| `MTE_UPDATE` | Rewrites golden files. Use `make golden`, which regenerates the corpus, the DTO schemas, the engine fixtures and the smoke trace |
| `MTE_SCALE` | Enables the scale test (`make scale` sets it) |
| `MTE_TRACE` | Path of a trace for the scale test's import of a real file; skipped without it |
| `MTE_SPIKE_ROWS` | Enables the store spike (`make spike` sets it) |
| `MTE_FUZZ_SLOW` | Directory where `FuzzOpen` keeps inputs that took more than 2 seconds |

## Replaying a failure

A failing fuzz input lands in `internal/engine/testdata/fuzz/FuzzOpen/`. Commit it with the fix: `go test ./internal/engine` replays it. `scripts/smoke_corpus.py` prints the application's whole output on failure and takes `--only SUBSTRING` and `--repeat N`.
