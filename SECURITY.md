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

### Local only

The application makes no network connections: no updater, no telemetry, no remote fonts or scripts (a lint rule, a dependency test and the window's Content Security Policy enforce it). It never talks to a Modbus device.

### Traces contain operational data

The analysis cache holds metadata and references only (directory 0700, files 0600, safe to delete). Logs never contain trace content. Exports can contain addresses and register values; handle them like the trace.
