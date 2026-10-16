# OT Fabric Modbus Trace Explorer

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI Status](https://github.com/otfabric/modbus-trace-explorer/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/modbus-trace-explorer/actions/workflows/ci.yml)
[![Code Coverage](https://codecov.io/gh/otfabric/modbus-trace-explorer/graph/badge.svg)](https://codecov.io/gh/otfabric/modbus-trace-explorer)
[![Latest Release](https://img.shields.io/github/v/release/otfabric/modbus-trace-explorer?label=release)](https://github.com/otfabric/modbus-trace-explorer/releases)

A desktop tool to understand Modbus/TCP traffic recorded by someone else. Open a packet trace (pcap or pcapng from tcpdump, Wireshark, dumpcap or tshark), confirm which flows are Modbus, and get a timeline in Modbus terms (connect, read 62 holding registers at 0x1000, write 1 register, exception, no response, disconnect), with breakdowns per device, unit, connection, poll pattern and address, a detail view down to the bytes and packets, and exports:

- **MBCAP**, so a passive capture of a real installation can be replayed as a virtual device with [`modbusctl server replay`](https://github.com/otfabric/modbusctl);
- the timeline as CSV or JSON Lines;
- a filtered sub-trace as pcapng.

It does not capture live (use tcpdump or Wireshark), never talks to devices and never writes to Modbus. All trace understanding is in the library [go-modbus-trace](https://github.com/otfabric/go-modbus-trace), whose CLI `mbtracectl` is the headless way to do the same on a gateway or in CI.

**Status: version 0.1 (milestones M0 to M4).** Opening `.mbcap` files for inspection (R24) is planned for v0.2.

## Install

Every release has the files below, a checksum file (`modbus-trace-explorer-<version>-checksums.txt`) and build provenance. The files are **not code-signed yet**, so macOS and Windows warn on the first start; the steps for that are under each platform.

| Platform | File |
|---|---|
| macOS 13 or later, Apple silicon | `modbus-trace-explorer-<version>-darwin-arm64.zip` |
| macOS 13 or later, Intel | `modbus-trace-explorer-<version>-darwin-amd64.zip` |
| Windows 10 or 11, x64 | `modbus-trace-explorer-<version>-windows-amd64.zip` |
| Windows 11 on ARM | `modbus-trace-explorer-<version>-windows-arm64.zip` (best effort) or the x64 build |
| Linux, x86-64 / arm64 (GTK 4, WebKitGTK 6.0) | `modbus-trace-explorer-<version>-linux-amd64.tar.gz` / `-linux-arm64.tar.gz` |

All downloads are on the [releases page](https://github.com/otfabric/modbus-trace-explorer/releases/latest). The commands below fetch the latest release and check its checksum. You can also check the build provenance: `gh attestation verify <file> --repo otfabric/modbus-trace-explorer`.

### macOS

```sh
VERSION=$(curl -fsSL https://api.github.com/repos/otfabric/modbus-trace-explorer/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
ARCH=$(uname -m | sed 's/x86_64/amd64/')            # arm64 or amd64
FILE="modbus-trace-explorer-${VERSION}-darwin-${ARCH}.zip"
BASE="https://github.com/otfabric/modbus-trace-explorer/releases/download/v${VERSION}"
curl -fsSLO "${BASE}/${FILE}" && curl -fsSLO "${BASE}/modbus-trace-explorer-${VERSION}-checksums.txt"
grep " ${FILE}\$" "modbus-trace-explorer-${VERSION}-checksums.txt" | shasum -a 256 -c -
ditto -x -k "${FILE}" /Applications
open "/Applications/Modbus Trace Explorer.app"
```

The app is ad-hoc signed, not notarized. If macOS refuses to open it, go to System Settings → Privacy & Security and choose "Open Anyway" once, or remove the quarantine flag: `xattr -dr com.apple.quarantine "/Applications/Modbus Trace Explorer.app"` (a file downloaded with `curl` usually has none). Traces open by dropping them on the window, with File → Open, or with "Open With" in the Finder.

### Windows

In PowerShell:

```powershell
$version = (Invoke-RestMethod https://api.github.com/repos/otfabric/modbus-trace-explorer/releases/latest).tag_name.TrimStart('v')
$file    = "modbus-trace-explorer-$version-windows-amd64.zip"
$base    = "https://github.com/otfabric/modbus-trace-explorer/releases/download/v$version"
Invoke-WebRequest "$base/$file" -OutFile $file
Invoke-WebRequest "$base/modbus-trace-explorer-$version-checksums.txt" -OutFile checksums.txt
$expected = ((Select-String -Path checksums.txt -Pattern " $file$").Line -split '\s+')[0]
if ((Get-FileHash $file -Algorithm SHA256).Hash -ne $expected.ToUpper()) { throw "checksum mismatch" }
Expand-Archive $file -DestinationPath "$env:LOCALAPPDATA\Programs\Modbus Trace Explorer" -Force
& "$env:LOCALAPPDATA\Programs\Modbus Trace Explorer\modbus-trace-explorer-$version-windows-amd64\modbus-trace-explorer.exe"
```

SmartScreen shows "Windows protected your PC" the first time: choose "More info", then "Run anyway". Windows 10 needs the [WebView2 runtime](https://developer.microsoft.com/microsoft-edge/webview2/); Windows 11 has it.

### Linux

The application needs GTK 4 and WebKitGTK 6.0: Debian 13, Ubuntu 24.04 or later.

```sh
sudo apt install libgtk-4-1 libwebkitgtk-6.0-4
VERSION=$(curl -fsSL https://api.github.com/repos/otfabric/modbus-trace-explorer/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
ARCH=$(dpkg --print-architecture)                    # amd64 or arm64
FILE="modbus-trace-explorer-${VERSION}-linux-${ARCH}.tar.gz"
BASE="https://github.com/otfabric/modbus-trace-explorer/releases/download/v${VERSION}"
curl -fsSLO "${BASE}/${FILE}" && curl -fsSLO "${BASE}/modbus-trace-explorer-${VERSION}-checksums.txt"
grep " ${FILE}\$" "modbus-trace-explorer-${VERSION}-checksums.txt" | sha256sum -c -
tar xzf "${FILE}" && sudo install -m 0755 "${FILE%.tar.gz}/modbus-trace-explorer" /usr/local/bin/
modbus-trace-explorer site.pcapng
```

## A short tour

1. **Open** a trace: drop it on the window, File → Open (`Ctrl/Cmd+O`), Open Recent, or `modbus-trace-explorer site.pcapng.gz`. Several files are one trace in the order of their first packets; gzip is decompressed into the cache while the trace is open.
2. **Flows**: the scan lists the TCP flows; the Modbus ones are ticked and their server ports filled in. A device on another port: type the port and scan again. TLS (Modbus/TCP Security) is named and cannot be decoded. Analyse.
3. **Timeline**: one row per transaction (request and response together), connection events, gaps and resyncs. Filter with the controls (Filters) or the text box, for example:

   ```text
   unit:1 fc:3,4 status:exception
   server:10.0.3.21:502 addr:holding/4096-4160 from:14:30 to:+15m
   class:write -flag:retransmitted
   ```

   `Ctrl/Cmd+G` goes to a time, `Ctrl/Cmd+Shift+G` to a Wireshark frame number, `N`/`P` to the next or previous row of the same connection. The detail pane (`Enter`) shows the request and response in words and by field, the bytes (select and copy them as hex or as a C or Python byte string) and the packets with their Wireshark filter.
4. **Views** (`1`–`6`): Devices (latency quantiles), Conversations, Patterns (poll plans: interval, jitter, how often the answer changes), Addresses (what was read and written, and the values of one address over time), Quality (every condition the library counted, with what it means).
5. **Export** (`Ctrl/Cmd+E`): an MBCAP capture with a live preview of what goes in and why the rest stays out; the timeline as CSV or JSON Lines; a pcapng sub-trace. After an MBCAP export the dialog gives the commands to check and replay it:

   ```sh
   mbcapctl verify site.mbcap
   modbusctl server replay --input site.mbcap --listen 127.0.0.1 --port 1502 --before-first absent --missing exception
   ```

Settings (`Ctrl/Cmd+,`) hold the time display, extra server ports for the Flows step, 1-based address labels and the analysis cache (location, size limit, "Clear cache"). Help → About has "Copy diagnostics" for a bug report: versions and the trace's facts, never paths or addresses.

## Development

Start with [AGENTS.md](AGENTS.md), then [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/UX.md](docs/UX.md), [docs/BRANDING.md](docs/BRANDING.md) and [docs/PROPOSAL-REVIEW.md](docs/PROPOSAL-REVIEW.md). `modbus-capture-claude-proposal.md` is the superseded original proposal. Releases are described in [RELEASE.md](RELEASE.md).

```sh
make frontend-install    # once: npm ci
make check               # Go and frontend checks
make build-gui           # ./bin/modbus-trace-explorer (cgo; the platform's webview)
make smoke               # start it on testdata/smoke.pcapng and wait for the timeline (A15)
make golden              # rewrite the schemas, the engine fixture and the smoke trace
cd frontend && npm run dev   # the user interface over the mock engine, in a browser (?rows=10000000, ?empty)
make spike               # the M0 index measurements (SPIKE_ROWS, default 10,000,000)
```

## License

MIT
