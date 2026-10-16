# modbus-trace-explorer Releases

## v0.1.0

**Date:** 2026-10-16
**Previous release:** none (first release)

## Summary

The first release of the Modbus Trace Explorer: open a pcap or pcapng trace, confirm its Modbus/TCP flows, and read the traffic as a timeline in Modbus terms, with views per device, connection, poll pattern, address and capture quality, a detail view down to the bytes and the packets, and exports to MBCAP (for `modbusctl server replay`), CSV, JSON Lines and pcapng. Read-only and offline: it never captures, never talks to a device and opens no network connection.

## Changes

### Added

- **Opening** (R1–R3): File → Open, Open Recent, drop on the window, "Open With", the command line; pcap, pcapng and gzip; several files as one trace in the order of their first packets. A scan proposes the Modbus flows and server ports (Flows step); the analysis goes into a columnar index in the cache directory, reused when the trace is opened again. A gzip trace's decompressed copy lives only while the trace is open.
- **Timeline** (R5–R7): request and response as one row, connection events, gaps and resyncs; local, UTC or relative time; time or latency order; filters as chips, a text grammar (`unit:1 fc:3 status:exception addr:holding/4096-4160 from:+15m`) and filter controls; go to time or frame; next or previous row of a connection. The detail pane: the transaction in words, the fields with their byte ranges, the hex dump (select bytes, copy as hex or as a C or Python byte string), the packets with Wireshark filters, notes and the MBCAP decision.
- **Views** (R8–R12): Devices (exact latency quantiles), Conversations, Patterns (interval, jitter, changed answers, static/slow/noisy), Addresses (coverage of what was read and written, the value history of one address with writes marked), Quality (every library counter with its meaning) and the capture facts.
- **Exports** (R13–R16): MBCAP (binary, JSON Lines, CSV) with a live preview of what is written and why the rest is not, the library's report, and the replay commands; the timeline as CSV or JSON Lines; a pcapng sub-trace of chosen connections. Every export runs through the library, can be cancelled, and is written atomically.
- **Settings** (R20): time display, extra server ports, 1-based address labels, the cache's size limit and "Clear cache", "Clear recent files". Help → About with "Copy diagnostics" (no paths, no addresses).
- **Native menu** (File, Edit, View, Help) and the window size kept between runs.
- JSON Schemas for every Service DTO in `docs/schema/`, checked against the engine's output and against the mock the UI is tested with.

### Measured

At 1,000,000 transactions on an Apple M1: import at about 80 MB/s end to end, the index about a third of the trace, timeline pages under 1 ms and new filters in milliseconds, the views in 0.1–55 ms. An address history reads about 12,500 points a second.

### Built with

go-modbus-trace (pseudo-version of 2026-10-15), go-modbus-capture v0.0.2, Wails v3.0.0-beta.29.

### Not in this release

- Code signing, notarization, installers and Linux packages: the builds are unsigned archives (see the README for Gatekeeper and SmartScreen).
- Opening `.mbcap` files for inspection (R24): planned for v0.2.
- The WebView checks on Windows and Linux were run by CI's smoke test, not by hand.

---
