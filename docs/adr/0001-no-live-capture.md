# ADR 0001: No live capture

- Status: Accepted
- Date: 2026-10-15 (user decision)
- Specified in: `AGENTS.md`, settled decisions; `docs/PROPOSAL-REVIEW.md`, change 1

## Context

The original proposal was a passive live capture tool, with capture backends for each platform, a helper, an admin policy and retention. tcpdump and Wireshark already capture live.

## Decision

The input is a trace file. There is no capture backend, no privilege model and no recording.

## Consequences

The application reads traces recorded elsewhere. It never talks to devices and never modifies a trace. On a gateway, the `mbtracectl` CLI of go-modbus-trace works on the trace where it lives.
