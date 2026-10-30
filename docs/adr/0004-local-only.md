# ADR 0004: Local only

- Status: Accepted
- Date: not recorded; a settled decision in `AGENTS.md`
- Specified in: `AGENTS.md`, settled decisions and hard rules; `docs/ARCHITECTURE.md` section 13

## Context

Traces contain operational data: addresses, register values, writes (`docs/ARCHITECTURE.md` section 13).

## Decision

The application makes no network connections: no updater, no telemetry, no remote resources. Fonts and everything else are bundled.

## Consequences

Lint bans `net/http` and dialing in Go outside tests, `internal/policy` checks the dependency graph, the frontend CSP is `default-src 'self'`, and `make frontend-urls` fails on an external URL in the bundle.
