# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in the Modbus Trace Explorer, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email **security@otfabric.io**, or open a private advisory on GitHub (the [Security](https://github.com/otfabric/modbus-trace-explorer/security) tab → **Advisories** → **Report a vulnerability**), with:

- A description of the vulnerability
- Steps to reproduce the issue
- The affected version(s) (`modbus-trace-explorer --version` output)
- Any potential impact you've identified

We will acknowledge your report within 48 hours and aim to provide a fix or mitigation within 7 days for critical issues.

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | Best effort |


## Security Considerations

### Traces are untrusted input

Packet traces come from third parties. All parsing is done by go-modbus-trace, which bounds its memory and is fuzzed; the application adds a free-space check before decompressing, a size cap on its cache, and renders every string from a trace as plain text, never as HTML.

### Limits

What one trace or capture can hold, what the application keeps for it, and what happens at each limit. The parsing limits are the libraries': go-modbus-trace's defaults (tracked connections 100,000, flows returned by a scan 1,000,000, packets 256 KiB, 64 MiB of analysis state, and the others in its `docs/ARCHITECTURE.md` section 2) and go-modbus-capture's (65,536 targets and 65,536 streams in a capture, strings of 64 KiB).

| What | Limit | At the limit |
|---|---|---|
| Servers and units of one trace (the index's device dictionary, one entry for each server and each server and unit pair) | 65,535 | The analysis is refused with the error code `limit` and a message that names the limit; no index is kept. A capture always fits (one entry a target). |
| Client addresses of one trace | 65,535 | As above. A capture always fits (one entry a stream). |
| Rows of one index (transactions, connection events, gaps, resyncs, notes, capture samples) | 4,294,967,295 | As above. |
| Request summaries of one index (distinct texts) | 1,048,576 | A later row's summary is rebuilt from its columns when it is shown (without the values of writes). |
| Times of a capture's samples | up to 2262-04-11 23:47:16.854775807 UTC, the last nanosecond since 1970 in 64 bits | A later sample is left out, counted (`out_of_range` in the capture's facts) and named in the import's warning. |
| Analysis cache | 2 GiB by default; settable from 256 MiB to 1 TiB | The least recently used entries are removed until the cache is under its cap. The index and the decompressed copy of an open trace are kept while it is open, also when they are larger than the cap. |
| Free space on the cache's disk | 512 MiB stays free | A gzip trace is not decompressed, or its decompression stops, when the copy would leave less; the scan (or the preparation of a trace opened again) fails with the error code `unavailable`. |
| Timeline page | 2,000 rows a request | A larger request gets 2,000 rows. |
| Cached filter results (selections) | the six most recent, 64 MiB together (4 bytes a selected row); the most recent is kept whatever its size | An older filter is scanned again when it is asked for again. |
| Address history | 200,000 points a request unless the request asks for another number | The history stops there and says where to continue. |
| Patterns of one view | 10,000 | Further request patterns are left out of the view. |
| Server ports in the settings | 32 | The settings are refused with the error code `invalid_argument`. |
| Recent files | 10 | The oldest drops off the list. |
| Log file | 1 MiB, and 3 older files | The file is rotated and the oldest removed. |

### Local only

The application makes no network connections: no updater, no telemetry, no remote fonts or scripts (a lint rule, a dependency test and the window's Content Security Policy enforce it). It never talks to a Modbus device.

### Traces contain operational data

The analysis cache holds metadata and references only (directory 0700, files 0600, safe to delete). Logs never contain trace content. Exports can contain addresses and register values; handle them like the trace.
