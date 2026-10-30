# UX specification

The reader is an engineer who knows Modbus and probably Wireshark. They want to see what a client and a device did, in Modbus terms, and to be sure the tool is not lying. Principles: dense and quiet, keyboard-first, every number traceable to packets, uncertainty always visible (a guess is labeled), nothing happens to the trace file. Branding and tokens are in `BRANDING.md`; requirement IDs refer to `REQUIREMENTS.md`.

## 1. Window anatomy (main screen)

```
+--------------------------------------------------------------------------------------+
| [modbus icon] Modbus Trace Explorer     powered by [weave] OT Fabric        [Theme]  |
+--------------------------------------------------------------------------------------+
| site-2026-10-14.pcapng  1.8 GB  14:02:11 - 16:40:02 (2 h 37 min)  4 devices          |
| 912,340 transactions  7 connections             [! Quality: 3 warnings]  [Export v]  |
+--------------------------------------------------------------------------------------+
| [unit: 1,2 x] [fc: 3,4 x] [-flag: retransmitted x]  [ filter text ...      ]  Reset  |
|                                                     31,204 of 912,340 transactions  |
+------------+-------------------------------------------------+-----------------------+
| Timeline   | #      Time          Client  Server  U TID Func  | Detail                |
| Devices    |                                                 |  Summary Request      |
| Conversat. | 1 14:02:11.384210 ...                           |  Response Raw Packets |
| Patterns   | 2 14:02:11.384891 ...                           |  Notes                |
| Addresses  |                                                 |                       |
| Quality    |                                                 |                       |
+------------+-------------------------------------------------+-----------------------+
| Status: 31,204 rows - time shown as local - cache: ready (212 MB)                    |
+--------------------------------------------------------------------------------------+
```

- **Header** as `BRANDING.md`: Modbus protocol icon, product name, "powered by OT Fabric", theme toggle. There is no LOCAL/BROWSER badge (there is only one mode).
- **File strip** (R4): the facts of the open trace and the Quality badge; the badge opens the Quality view. Export menu on the right.
- **Filter bar** (R7): chips for active filters, a text box, a counter, Reset.
- **Left rail**: the six views. **Center**: the active view. **Right**: the detail pane for the selected row (collapsible with `D`; resizable). The detail pane follows the selection in every view that has rows.
- **Status bar**: row count, time display mode, cache state, running jobs with a cancel button.

## 2. Empty state and import

**Empty state** (no trace open): the product name, a large drop target "Drop a pcap or pcapng here, or Open...", a short line of what formats work ("pcap and pcapng from tcpdump, Wireshark, dumpcap, tshark; .gz too"), the recent files list, and one line: "This tool reads recordings. To record, use tcpdump or Wireshark." with a link to a short how-to (tcpdump command for port 502 on a gateway: `tcpdump -i eth0 -s 0 -w site.pcap 'tcp port 502'`, noting that `-s 0` avoids truncated frames and that other ports need their own filter).

**Scanning**: a progress panel (bytes read of total, packets, rate, elapsed), Cancel.

**Flows step** (R2):

```
site-2026-10-14.pcapng   1.8 GB   pcapng   Ethernet, 802.1Q   14:02:11 - 16:40:02   0 dropped by capture host
Server ports to analyze: [502, 1502, 5021 ]  (found: 502, 1502, 5021)

[x] Class         Client            Server             Packets   Bytes    Duration   Modbus score
[x] Modbus/TCP    10.0.3.5:49822    10.0.3.21:502      812,044   96 MB    2h 37m     ####### 0.99
[x] Modbus/TCP    10.0.3.5:49830    10.0.3.22:1502      94,312   11 MB    2h 37m     ####### 0.98
[ ] TLS           10.0.9.2:51133    10.0.9.8:802           204   0.2 MB   14 s       not decodable (Modbus/TCP Security)
[ ] Other TCP     10.0.3.5:55012    10.0.3.1:22          1,811   0.3 MB   20 min     -
                                                                                   [Cancel]  [Analyze 2 flows]
```

Rows are selectable; the role override ("this endpoint is the server") is in the row menu; rows classed TLS or other are unchecked and explained. If exactly one server port group is Modbus-like, the dialog shows a single primary button. Analyzing shows progress with Cancel, and the main screen appears when the first rows are available (SHOULD; v0.2), with a banner giving the progress and Cancel until the analysis ends, or at the end.

**Errors** (plain language, with a next step): "No Modbus/TCP found. The trace has 3 TCP flows on ports 22, 443 and 8080. If your devices use another port, enter it above." / "This trace contains Modbus/TCP Security (TLS) on port 802; the payload is encrypted and cannot be decoded." / "The capture ends in the middle of a packet; the rest of the file was read." / "Link type 105 (IEEE 802.11) is not supported." / "This file is not a pcap or pcapng file (it starts with 50 4b 03 04, which looks like a ZIP archive)."

## 3. Timeline (R5)

Columns, left to right: `#` (sequence), `Time`, `Client`, `Server`, `Unit`, `TID`, `Function` (number and short name, e.g. `03 Read Holding`), `Summary` (the request in words), `Result`, `Latency`, `Flags` (icons).

**Row kinds**

| Kind | Row text | Icon |
|---|---|---|
| Transaction (paired) | summary and result as below | read arrow-down / write pencil / other dot |
| Connection opened | `Connection opened (SYN)` or `Connection seen mid-stream` + endpoints | plug |
| Connection closed | `Connection closed (FIN | RST | end of trace) after 12 min, 7,231 transactions` | plug-off |
| Gap | `Gap: about 1,460 bytes missing (server to client)`; the following transaction may be flagged | warning |
| Resync | `Framing recovered after 17 bytes (confidence: next header valid)` | warning |
| Note | e.g. `Capture host reported 12 dropped packets on eth0` | info |

Write transactions have a copper left border **and** a "W" label (never color alone). Exceptions show the danger color **and** an error icon and the code text. Unanswered requests and ambiguous pairs show the warning icon and text.

**Summary grammar** (request, in words; addresses are zero-based as on the wire, shown as `@ 4096 (0x1000)`; the reference-label toggle appends e.g. `[44097]` where applicable):

| Function | Summary |
|---|---|
| 01 / 02 | `Read 16 coils @ 0` / `Read 8 discrete inputs @ 100 (0x64)` |
| 03 / 04 | `Read 62 holding registers @ 4096 (0x1000)` / `Read 2 input registers @ 0` |
| 05 / 06 | `Write coil @ 12 = ON` / `Write register @ 100 = 0x0010 (16)` |
| 15 / 16 | `Write 8 coils @ 0` / `Write 2 registers @ 100` |
| 22 | `Mask write register @ 100 (AND 0xFF00, OR 0x0010)` |
| 23 | `Read 4 / write 2 holding registers @ 0 / 10` |
| 07 / 11 / 12 / 17 | `Read exception status`, `Get comm event counter`, `Get comm event log`, `Report server ID` (marked "serial function over TCP") |
| 08 | `Diagnostics: Return query data (0x0000)` |
| 20 / 21 | `Read file record (2 sub-requests)` / `Write file record` |
| 24 | `Read FIFO queue @ 100` |
| 43 / 14 | `Read device identification (basic, from object 0)` |
| unknown | `Function 0x41 (user-defined), 12 bytes` |

**Result** column: `62 words`, `16 bits`, `OK`, `Exception 02 Illegal data address`, `No response observed`, `Response without request`, `Ambiguous (2 candidates)`, `Malformed`, `Uncertain (gap)`. Latency in ms with one decimal (µs below 1 ms).

Sorting: time (default) or latency, ascending/descending. Selecting a row loads the detail pane. Double-click on a row's connection cell filters to that connection; on a device cell, to that device.

## 4. Detail pane (R6)

Tabs: **Summary**, **Request**, **Response**, **Raw**, **Packets**, **Notes**.

- *Summary*: one paragraph in words ("10.0.3.5 asked 10.0.3.21 unit 1 to read 62 holding registers from address 4096. It answered in 4.2 ms with 62 words."), times (request first/last packet, response first/last), the first values for reads (hex and decimal, up to 16, then "show all" opens the Raw tab at the data).
- *Request* / *Response*: a table of named fields (name, value, raw bytes, meaning), validation warnings in the warning style. Selecting a field highlights its bytes in *Raw*.
- *Raw*: hex dump of the whole ADU (MBAP header, then PDU) with offsets and ASCII; header and PDU visually separated; field highlight; click and shift-click select bytes; copy as hex, as space-separated hex or as a C/Python byte string.
- *Packets*: list of packets that carried the request and the response: frame number (as Wireshark counts), time, interface, file offset, payload range; buttons "Copy frame number" and "Copy Wireshark filter (`frame.number == N`)". Retransmissions that were folded away are mentioned.
- *Notes*: quality flags with their one-sentence meaning, validation warnings, connection (epoch, how it opened, role source and confidence), and **MBCAP status**: "Will be exported" or "Not exported because: writes are not part of replay data" (reason text from `MBCAP-EXPORT.md`).

## 5. Other views

**Devices** (R8): left a tree (server endpoint > unit with transaction counts), right a table for the selection: by-function counts with read/write/exception shares, clients seen (clickable), first/last, rate, bytes, unanswered, latency p50/p95/p99/max with a small histogram. A "Show in timeline" button applies the device as a filter.

**Conversations** (R9): table of connections (client:port, server:port, epoch, opened, closed, duration, transactions, bytes, role source with a guessed marker, flags). Selecting shows the connection's action history in a compact list (the same rows as the timeline, filtered) and a thin time bar with markers for gaps, resyncs and write bursts.

**Patterns** (R10): table of recurring requests per client > device > unit: function and range, count, median period, min/max, jitter, volatility chip (static, slow, noisy) with its meaning on hover ("share of polls whose response changed: 0.3%"), first/last. Selecting shows the request rate (per second and per minute, over the span of the pattern), the period distribution (histogram) and "Show in timeline". Text explains that a pattern is a repeated identical request, as a poll plan is seen from the wire.

**Addresses** (R11): choose device, unit, data space. Top: coverage strip, a horizontal map of the 0-65535 address space with the observed intervals (reads and writes in different patterns, not only colors), counts on hover, selectable. A switch shows the same coverage as pies instead: the share of requests per address block, one pie for reads and one for writes, both with the same eight blocks and colors (the rest fold into "other blocks"), each with a legend. Bottom: for the selected address or interval, the value history as a step chart with a table below (time, client, raw value in hex/dec/bin, changed marker), write events as vertical marks with the written value and who wrote. Loading shows progress and "Showing 200,000 of at least 1.3 million points - load more". A permanent line: "Shows what was observed on the wire; addresses never read are unknown, not zero."

**Quality** (R12): a list of conditions with count, one-sentence meaning, severity (info, warning, problem), and "Show in timeline" when rows carry the condition (a counter of segments may have none). Includes the capture-level facts (dropped packets reported by the capture, packets with snap length truncation, unsupported encapsulations) and the roles that were guessed. Nothing here is hidden when zero: a zero is shown as "none".

## 6. Filters and the text grammar (R7)

Chips come from the filter controls (device, unit, client, connection, function, status, class, flags, time range, address range) in a filter popover, and from the text box. Grammar: space-separated terms, all must match (AND); comma inside a value is OR; a leading `-` negates.

| Term | Meaning | Example |
|---|---|---|
| `server:` | server address or `address:port` | `server:10.0.3.21:502` |
| `unit:` | unit IDs | `unit:1,2` |
| `client:` | client address | `client:10.0.3.5` |
| `conn:` | connection IDs | `conn:7` |
| `fc:` | function codes, decimal or `0x`, or names | `fc:3,4,43` `fc:read` |
| `class:` | `read`, `write`, `other` | `class:write` |
| `status:` | `ok`, `exception`, `unanswered`, `orphan`, `ambiguous`, `malformed`, `uncertain` | `status:exception` |
| `exc:` | exception codes | `exc:2,3` |
| `flag:` | quality flags | `-flag:retransmitted` |
| `addr:` | data space and range, or a single address | `addr:holding/4096-4160` `addr:4100` |
| `from:` `to:` | clock time of the trace's day, ISO time, or an offset from the start | `from:14:30 to:+15m` |
| bare word | function name or alias | `identification` `holding` |

A term that cannot be parsed is shown as an error chip with the reason; it is never ignored.

## 7. Export dialogs (R13-R16)

**MBCAP**: one dialog, three zones. *Scope*: everything analyzed / current filters / one device; time window; exceptions on (default); change-only off; tick resolution; editable target labels (a small table: server, unit, label); encoding. *Preview* (live from the index): "880,101 samples in 3 targets and 4 streams" and the exclusion table (reason, count, explanation: for example "Writes (120): replay data holds reads only"). A bold line when the preview reveals risks: "2 writes happened after the last read of the same registers; a replay will keep the old value" (known only after the run: the report). *Result* (after the run): the report summary, "Save report (JSON)", and the **Replay** block with copyable commands:

```
mbcapctl verify site.mbcap
modbusctl server replay --input site.mbcap --listen 127.0.0.1 --port 1502 --before-first absent --missing exception
```

and, for several targets, one `--target N --port P` line each, with a one-line note on unit collisions.

**Timeline** and **Sub-trace**: scope (current filters / everything), format (CSV or JSON Lines; pcapng), time window, Save. The sub-trace dialog says: "Includes the packets of the selected connections as recorded, including writes and vendor functions."

Every export is a job in the status bar with progress and Cancel; a cancelled or failed export leaves no file.

## 8. Settings (R20)

Theme (dark default, light), time display (local, UTC, relative), default server ports, address display (zero-based; reference label toggle with an explanation), cache (location, size, cap, Clear), recent files (Clear).

## 9. Keyboard map

Every action has a keyboard path; `Cmd` on macOS, `Ctrl` elsewhere. Exact keys may change if they collide with the platform; the existence of a binding may not.

| Keys | Action |
|---|---|
| `Cmd/Ctrl+O` | Open trace |
| `Cmd/Ctrl+E` | Export menu |
| `Cmd/Ctrl+F` or `/` | Focus the filter box |
| `Esc` | Clear the box, close a dialog, return focus to the table |
| `Up/Down`, `PgUp/PgDn`, `Home/End` | Move the selected row |
| `Enter` | Move focus to the detail pane |
| `D` | Show or hide the detail pane |
| `1` ... `6` | Timeline, Devices, Conversations, Patterns, Addresses, Quality |
| `N` / `P` | Next / previous row of the same connection |
| `Cmd/Ctrl+G` | Go to time; `Cmd/Ctrl+Shift+G`: go to frame |
| `T` | Cycle time display |
| `Cmd/Ctrl+C` | Copy the selected row as text; `Cmd/Ctrl+Shift+C`: copy its raw hex |
| `Tab` / `Shift+Tab` | Move between rail, filter bar, table, detail pane |

## 10. Time, numbers, text

- Time display modes: local (with the offset in the status bar), UTC, relative to the first packet (`+00:12:04.384210`). Nanoseconds are available on hover and in the detail pane; the table shows microseconds.
- Numbers use thousands separators; hex is uppercase with `0x`; addresses are zero-based with the reference-label toggle; byte counts in IEC units (KiB, MiB).
- All text from the trace (identification strings, vendor names) is rendered as plain text, truncated visually but copyable in full.

## 11. Accessibility

Same rules as `BRANDING.md` section 7. Specific to this application: the table is a grid with row and column headers announced; the selected row is announced with its summary; icon-only buttons have labels and tooltips; charts have a table alternative; coverage-strip patterns are distinguishable without color; focus is always visible; no information is available only on hover.
