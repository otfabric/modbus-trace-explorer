# modbus-trace-explorer Releases

## v0.5.0

**Date:** 2026-10-30
**Previous release:** v0.4.0

### Summary

Patterns states the request rate, Addresses can show coverage as pies, and a button shows or hides the detail pane. Retransmitted requests now appear in the timeline, and invalid responses are stated as such.

### Added

- Patterns: the request rate of the selected pattern, per second and per minute.
- Timeline and Conversations: a button at the right edge shows or hides the detail pane, which until now only `D` and the View menu could do.
- Addresses: a bars/pie switch for coverage; the pies show the share of requests per address block.

### Fixed

- Addresses: "Show values" read the bytes of invalid responses as register values. It now leaves those rows out; they stay in the timeline under `flag:invalid_pdu`.
- Timeline: a read whose response breaks a rule or does not fit its request said "4 words", the count asked for. It now says `Invalid PDU`, and the Summary tab states the sizes.
- Quality: "Retransmissions" led to an empty timeline. A whole message sent again now marks its transaction (go-modbus-trace v0.4.0), unless the copy arrives after the transaction is complete; those are only counted. "Show in timeline" is offered only when rows carry the condition.

### Dependencies

- go-modbus-trace v0.3.0 → v0.4.0.

---

## v0.4.0

**Date:** 2026-10-30
**Previous release:** v0.3.1

### Dependencies

- `go-modbus-trace` v0.1.1 → v0.2.0.

---

## v0.3.1

**Date:** 2026-10-29
**Previous release:** v0.3.0

## Summary

Dependency update for serial Modbus devices: go-serial v0.1.8 fixes two POSIX serial I/O bugs. No command or flag changes.

### Dependencies

- go-modbus-trace v0.1.0 → v0.1.1 (go-modbus v1.7.2, go-serial v0.1.8).

### Fixes (from go-serial v0.1.8)

- `Write` no longer loses data when the serial port's output buffer is
  full. It used to return a short count with a nil error (or `n == -1`
  with `EAGAIN`); it now blocks until every byte is written.
- `Read` works when the port's file descriptor is 1024 or higher. It used
  to return a spurious `io.EOF` at once.

### Other

- Security contact is security@otfabric.io.

---

## v0.3.0

**Date:** 2026-10-18
**Previous release:** v0.2.0

## Summary

A README that shows what the Explorer is for, with a two-minute demo and screenshots made from the application itself, and go-modbus-trace at its release v0.1.0.

## Changes

### Added

- **A README about what the Explorer is for**, with a two-minute demo video and screenshots: finding what goes wrong between a Modbus client and its devices, and exporting real traffic to replay as a simulated device.
- **A demo trace** in the test corpus, `realistic/site-diagnosis.pcapng`: a site with the usual problems (slow meters behind an RTU gateway, an offline unit, duplicate polling, a 4x-offset register, a refused write, a connection reset).
- `make demo-server` and `docs/media/record-demo.mjs`, which make the screenshots and the video from the application itself.

### Changed

- go-modbus-trace is required as its release v0.1.0 instead of a pseudo-version of the same commit: the code is unchanged, but the version recorded in the cache and in exports is now `v0.1.0`, so traces opened before are analysed again once.

---

## v0.2.0

**Date:** 2026-10-18
**Previous release:** v0.1.0

## Summary

Opening `.mbcap` captures with the views (R24), the timeline while the analysis runs, a test corpus, and smaller improvements.

## Changes

### Added

- **Open MBCAP captures** (R24): `.mbcap`, `.mbcap.jsonl` and `.mbcap.csv` open read-only, by themselves, without a Flows step. The samples are the timeline (the target as the device, the stream as the connection), each decoded in the detail pane; Devices, Conversations (streams), Patterns, Addresses with the value history of an address, and Quality with the capture's own facts (producer, tick, start, provenance, change-only, a missing end or a cut file) work as for a trace. Captures have no exports and no frame numbers. On macOS, `.mbcap` files open with the application.
- **The status bar shows running exports and address histories** with their progress and a Cancel button, the cache's size, and the time zone offset of local time. An export can continue in the background ("Continue in the background" in the Export dialog); the status bar says when it finishes.
- **The detail pane can be resized**: drag its left edge, or focus the edge and use the arrow keys (Shift for larger steps, Home and End for the limits); double-click restores the default. The width is remembered.
- **The timeline appears while the analysis runs**: the rows analysed so far can be scrolled, filtered, sorted and opened in the detail pane, and the views show them, with a banner giving the progress and Cancel; the timeline grows in place and is complete at the end. Exports wait for the end. The analysis stays above R21's 50 MB/s (66 MB/s at a million transactions).
- **A test corpus** in `testdata/corpus`: realistic traces and captures (a plant, a ring buffer, a gateway, every function code; also for screenshots and demos), hard but valid input, and broken files, each with how the application must take it, checked by the engine tests, a fuzz test and the built application in CI.

### Fixed

- **A rare crash when a trace was closed, or a capture imported again, while the window was still reading it** (for example opening another file during a page fetch): the index was unmapped under the query. Found by running the test corpus through the built application in CI.

### Changed

- **A capture damaged after its first samples, and a gzip trace that ends early, open with what could be read** and a warning, as a cut trace already did; before, both were refused.
- CI uploads the frontend's test coverage to Codecov as well (flag `frontend`), next to the Go coverage (flag `unittests`).

---

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
