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

## Capturing a trace

The Explorer reads traces; it does not capture them. Any tool that writes pcap or pcapng will do. What makes a trace useful:

- **Where.** Capture where both directions of the Modbus traffic pass: on the client (SCADA host, gateway, PLC programming PC), on a switch port that mirrors the device's port (SPAN), or with a network tap. A capture on an ordinary switch port sees only that port's own traffic.
- **Whole packets.** Do not cut packets short (`-s 0`, the default in current tools). A cut ADU cannot be decoded and is reported as "cut by the snap length".
- **From before the connections open.** Start capturing, then start (or restart) the poller. The connection's handshake shows which side is the server; without it the Explorer infers it from the traffic and says so ("guessed roles").
- **Keep the TCP packets.** Filter by port when capturing, not by protocol afterwards: saving only the packets Wireshark decodes as `modbus` drops the handshakes, retransmissions and acknowledgements the reassembly needs.
- **Modbus/TCP Security** (TLS, usually port 802) is encrypted and cannot be decoded; the Flows step names it.

The examples capture port 502. For devices on other ports, extend the filter (`tcp port 502 or tcp port 1502`) and enter the port in the Flows step. Capturing needs privileges: `sudo` (or membership of the `wireshark` group on Linux), the ChmodBPF helper that Wireshark installs on macOS, Npcap on Windows. Interface names differ per system: `tcpdump -D`, `dumpcap -D` or `tshark -D` list them.

### tcpdump (Linux, macOS)

```sh
# Until Ctrl+C; full packets, no name lookups.
sudo tcpdump -i eth0 -s 0 -n -w site.pcap 'tcp port 502'

# A long capture in files of 100 MB, keeping the last 50 (5 GB at most).
sudo tcpdump -i eth0 -s 0 -n -C 100 -W 50 -w site.pcap 'tcp port 502'

# One file per hour, named by its start time.
sudo tcpdump -i eth0 -s 0 -n -G 3600 -w 'site-%Y%m%d-%H%M.pcap' 'tcp port 502'

# On a remote gateway, written on your machine.
ssh admin@gateway "sudo tcpdump -i eth0 -s 0 -n -U -w - 'tcp port 502'" > site.pcap
```

When tcpdump stops it prints how many packets the kernel dropped; more than zero means reads may be missing from the trace.

### dumpcap (comes with Wireshark; Linux, macOS, Windows)

dumpcap is Wireshark's capture engine: no decoding, so it keeps up on long captures, and its pcapng records the drops (the Explorer shows them in the Quality view).

```sh
dumpcap -D                                            # list the interfaces
dumpcap -i eth0 -f "tcp port 502" -w site.pcapng

# A day in files of 100 MB (100000 kB), the last 50 kept; stops after 24 hours.
dumpcap -i eth0 -f "tcp port 502" -b filesize:100000 -b files:50 -a duration:86400 -w site.pcapng
```

On Windows the interface is a number from `dumpcap -D` or its full name:

```powershell
& "C:\Program Files\Wireshark\dumpcap.exe" -D
& "C:\Program Files\Wireshark\dumpcap.exe" -i 3 -f "tcp port 502" -w "$env:USERPROFILE\Desktop\site.pcapng"
```

### tshark (comes with Wireshark)

```sh
# One hour, then stop. -f is a capture filter; do not use a display filter (-Y) when writing a file.
tshark -i eth0 -f "tcp port 502" -a duration:3600 -w site.pcapng
```

### Wireshark

1. Capture → Options: pick the interface and type `tcp port 502` as the capture filter (the field on the Input tab, not the display filter bar).
2. For a long capture, Output tab: a file name, pcapng, and "Create a new file automatically" every 100 MB with a ring buffer.
3. Start, let the poller run, stop.
4. File → Save As, pcapng, with all packets ("Export Specified Packets" with a `modbus` display filter would leave out the TCP packets the Explorer needs).

### Opening what you captured

Open the file, or select all the files of a ring buffer together: they are read as one trace, in the order of their first packets. Compressed files (`site.pcapng.gz`) open as they are. The Quality view then says what the capture missed: drops, cut packets, gaps, guessed roles.

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
make frontend-coverage   # the frontend tests with coverage (frontend/coverage/lcov.info)
cd frontend && npm run dev   # the user interface over the mock engine, in a browser (?rows=10000000, ?empty)
make spike               # the M0 index measurements (SPIKE_ROWS, default 10,000,000)
```

## License

MIT
