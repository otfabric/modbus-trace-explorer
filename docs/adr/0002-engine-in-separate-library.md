# ADR 0002: The engine is a separate library

- Status: Accepted
- Date: 2026-10-15 (user decision)
- Specified in: `AGENTS.md`, settled decisions; `docs/ARCHITECTURE.md` sections 1 and 3; `docs/PROPOSAL-REVIEW.md`, change 4

## Context

The proposal placed the engine inside the application. A headless CLI and CI need the same trace understanding.

## Decision

All trace understanding lives in `go-modbus-trace`. The application contains no pcap parsing, reassembly, framing, decoding or pairing. It orchestrates, indexes, queries and presents.

## Consequences

If the library lacks something, it is recorded in `docs/ARCHITECTURE.md` section 3 and not reimplemented here. `go.mod` holds no `replace` directive for the library (`make mod-check`).
