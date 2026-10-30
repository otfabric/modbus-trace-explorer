# ADR 0003: A columnar index, not SQLite

- Status: Accepted
- Date: 2026-10-16 (user decision, from the M0 spike)
- Specified in: `docs/ARCHITECTURE.md` section 5

## Context

The original design used SQLite through the pure-Go driver `modernc.org/sqlite`. The M0 spike (`make spike`) measured both options, and SQLite missed the size and latency budgets for unindexed filters and rank jumps.

## Decision

The analysis cache is a set of memory-mapped column files of fixed-width values, with permutations for time and latency order. Queries scan the filtered columns into a selection vector and page by rank.

## Consequences

The application owns the file format in `internal/store`. The index stays a disposable cache: the trace file is the source of truth, and exports re-stream the trace through the library.
