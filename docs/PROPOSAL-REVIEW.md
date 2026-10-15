# Review of the original proposal

Date: 2026-10-15. Subject: `modbus-capture-claude-proposal.md` ("OT Fabric Modbus Capture - proposal and Claude elaboration brief"), now superseded. Reviewed against the local checkouts of go-modbus-capture v0.0.2 (4b729e6), modbusctl v3.2.0 (c75d367), go-modbus v1.7.1, sunspec-explorer v0.0.3 (915d2c3) and the website, plus pkg.go.dev and npcap.com for the capture-stack facts.

## Verdict

The proposal's technical core was sound and mostly correct against the source: the MBCAP admission rules, the writer behavior, the replay semantics and the Explorer architecture all check out (below). Three things changed the plan:

1. **The user dropped live capture** (2026-10-15): tcpdump and Wireshark capture better than a new tool could, and live capture was the costliest part (capture backends per OS, a privilege model, Npcap licensing, recording durability and retention). The product became a **trace analyzer and exporter**: import a pcap or pcapng, show what happened in Modbus terms, break it down by device, unit, connection, poll pattern and address, and export MBCAP for replay.
2. The proposal was a **brief asking an agent to write the specification**, not a specification. It is replaced by the documents in this repository and in the library repository, with the decisions made.
3. The engine moved into a **separate library** (`go-modbus-trace`) with a CLI, and the headless story became that CLI (no daemon, no browser build).

## Changes and reasons

| # | Original proposal | Now | Reason |
|---|---|---|---|
| 1 | Passive **live capture** (AF_PACKET, BPF, Npcap), capture helper, admin policy, rotation, flush and crash recovery, retention, recording banner and library | Removed. Input is a trace file. | User decision. Removes the privilege model, three capture backends, durability and disk-full design. |
| 2 | Native desktop **and** headless daemon with browser UI, token, SSH forward | Desktop only. Headless = `mbtracectl` (library CLI). | Nothing needs to run continuously on a gateway any more; a CLI converts a trace where it lives (also over `ssh ... tcpdump -w -`). Saves the daemon, the API, the token handling and a second frontend build. The TS `Service`/`Platform` interfaces keep a web build possible later. |
| 3 | PCAPNG as mandatory "traffic evidence" plus MBCAP "replay data" | The input trace is the evidence. A **sub-trace export** (pcapng) is a SHOULD. | We no longer create the evidence; we read it. |
| 4 | Engine inside the application | Separate library `go-modbus-trace` + CLI `mbtracectl` | User decision. The CLI, the GUI, CI and possibly modbusctl share one engine and one set of tests. |
| 5 | One MBCAP stream per client conversation or connection epoch | One stream per (target, **client IP**) | modbusctl merges all streams of a target by time and has no stream-selection flag (`ReplayServerConfig`; `replay.Options.Streams` is internal), so finer streams cost size and give nothing. Connection detail stays in the trace. |
| 6 | "Large exports stream"; whole-session export via `edit.Merge` | The exporter writes directly with the `mbcap` writer; windows are exported with `--from/--to` | `edit.Merge` takes loaded captures (in memory) and `replay.Build` loads the whole capture, so the real ceiling is the replay machine's RAM, not our disk. The report estimates it. |
| 7 | Multi-interface deduplication as a feature | Not a feature: TCP reassembly drops duplicate observations as retransmissions | Offline, the same segment seen twice is a retransmission to the reassembler; no application-level dedup is needed. |
| 8 | "Choose the index technology on query and crash-recovery needs" | SQLite cache of pointers (pure-Go driver) with M0 budgets; exports re-stream the trace | Millions of rows must be paged and filtered; raw bytes stay in the trace. The Explorer's no-database principle does not carry over. The alternative (columnar in-memory) stays open until measured. |
| 9 | Typed decode profiles, SunSpec-aware profiles, a "state projector" | Later. Raw words, bits and hex only; coverage and address history kept, computed lazily | Scope. Raw is authoritative and sufficient for replay; interpretation is a display profile. |
| 10 | (not in the proposal) | Flow detection incl. non-502 ports and TLS classification; role detection; `stale_after_write` warnings; reproducible exports; exclusion report whose counts must add up; Wireshark/`tshark` as a test oracle; address-history and big-table designs | Needed once the input is someone else's trace: the tool must find Modbus, decide who is the server, and say what it could not use. |
| 11 | Name "Modbus Capture", repo `modbus-capture` / `modbus-recorder` | **Modbus Trace Explorer**, repo and binary `modbus-trace-explorer`; library `go-modbus-trace`; CLI `mbtracectl` (proposed) | Avoids the clash with the library `go-modbus-capture`, the format name and `modbusctl capture`; "Explorer" parallels SunSpec Explorer. |
| 12 | Dependency table: modbusctl v3.1.0 | modbusctl v3.2.0 (client TLS added); Explorer baseline v0.0.3 | Stale on 2026-10-15. |

## Verified (source in parentheses)

- **Admission rules**: reads FC01-04, 07, 08, 11, 12, 17, 20, 24, 43; FC08 only sub-functions 0x0000, 0x0002, 0x000B-0x0012; FC43 only MEI 14; writes (05, 06, 15, 16, 22, 23) rejected; PDUs 1 to 253 bytes; exception responses only with `Header.Exceptions` (go-modbus-capture `capture/check.go`, `docs/concepts.md`).
- **Writer**: `NewWriter`, `WriteTarget`, `WriteStream`, `WriteSample`, `Flush`, `Close(End)`, not concurrency-safe, definitions before samples, drops exceptions silently without the header flag; `ReadIndex`, `Repair`, `SkipCorrupt`; `capture.SplitAddresses`; `edit.WithProvenance` (`docs/library.md`, `mbcap/writer.go`, `edit/edit.go`).
- **Format status**: draft 0.1, not frozen; 1.0 readers will not read it (`docs/FORMAT.md`, README).
- **Replay**: flags `--listen --port --unit --target --speed --loops --from --missing --before-first --max-clients --input` and `static --at`; defaults (`hold`, `exception`, loops 0 = forever); exceptions ignored; per-address (FC01-04), per-object (FC43/14), per-request (others); all streams of a target merged by time (modbusctl `internal/config/types.go`, `internal/replay/replay.go`).
- **go-modbus**: `PDUTap` sees only an active client's exchanges, so it cannot sniff; no exported PDU parsers (only `ParseFunctionCode`); a server that serves every function (v1.7.x), which makes it a frame generator for tests (`tap.go`, `modbus.go`, git history).
- **Explorer**: Wails shell, two-build frontend with `Service`/`Platform` interfaces, daemon, CI workflows, packaging, icon rule, BRANDING (the base for this application's shell and conventions).
- **Website**: a Modbus protocol icon exists (`public/icons/go-modbus.svg`).

Facts used to justify dropping live capture (no longer load-bearing): Npcap's free edition may not be redistributed and covers up to 5 systems unless used only with Nmap, Wireshark or Defender for Identity (npcap.com, 2026-10-15); `google/gopacket` last tagged v1.1.19 in 2020, the fork `gopacket/gopacket` shows v1.7.4 (Oct 2026) (pkg.go.dev).

## Not verified (each has an owner milestone)

| Item | Where it is settled |
|---|---|
| Whether the gopacket fork (or any dependency) is CGO-free on all seven targets, has a usable pcapng writer, and beats ~500 lines of own reassembly | Library L0: settled, own code (library DECISIONS D1–D3) |
| MODBUS Messaging on TCP/IP Implementation Guide V1.0b (not in the local checkouts); framing rules here come from the Application Protocol spec and practice | Library L0 |
| modbusctl memory per MBCAP sample (to calibrate the report's replay estimate) | Library L0: settled (D6) |
| `tshark` field names for the oracle test | Library L4: settled (TESTING section 4) |
| tcpdump writes classic pcap and Wireshark/dumpcap write pcapng by default (general knowledge, not checked here); the readers support both either way | Library L1 |
| Throughput and memory targets (L23, R21) | Library L4: met (Scan 760 MB/s, Analyze 207 MB/s); App M0: index measured (ARCHITECTURE section 5), M1 measures end to end |
| SQLite versus columnar index budgets; keyset paging with row values in the driver's SQLite | App M0: measured; columnar chosen by the user on 2026-10-16 (ARCHITECTURE section 5) |
| Maximum scrollable element height in WKWebView, WebView2 and WebKitGTK; the scaled-scroll table | App M0: the table caps its spacer at 4,000,000 px, below every engine's limit; checked in Chromium; the three WebViews in M1 |
| Wails v3 beta.29 file-drop and open-with APIs | App M0: found in its source and wired (ARCHITECTURE section 12); run-time check in M1 |
| That the static Linux CLI runs unchanged on Debian 11, 12 and 13 on all three architectures (expected from `CGO_ENABLED=0`; to be tested like the Explorer's A12) | Library L1/L4 |

## Risks worth knowing

- **Passive replay data is partial by nature**: only what the observed clients read exists, writes are not replayed, and a value changed by a write is visible only at its next read (the export report warns about it). This is inherent, not a defect of the tool.
- **The MBCAP format is a draft**: a future 1.0 will not read today's files. Exports record the library versions; re-export from the trace after an upgrade (the trace remains the source of truth).
- **Wails v3 is a prerelease** and pins an API that may still change.
- **Large-trace UX** is where this tool earns or loses trust: the budgets in R21 are proposals until M0 measures them.
- **Two repositories** need a disciplined contract; section 3 of `ARCHITECTURE.md` is the single place where the application states what it needs from the library.
