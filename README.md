# OT Fabric Modbus Trace Explorer

A desktop tool to understand Modbus/TCP traffic recorded by someone else. Open a packet trace (pcap or pcapng from tcpdump, Wireshark, dumpcap or tshark), confirm which flows are Modbus, and get a timeline in Modbus terms (connect, read 62 holding registers at 0x1000, write 1 register, exception, no response, disconnect), with breakdowns per device, unit, connection, poll pattern and address, a detail view down to the bytes and packets, and exports:

- **MBCAP**, so a passive capture of a real installation can be replayed as a virtual device with [`modbusctl server replay`](https://github.com/otfabric/modbusctl);
- the timeline as CSV or JSON Lines;
- a filtered sub-trace as pcapng.

It does not capture live (use tcpdump or Wireshark), never talks to devices and never writes to Modbus. All trace understanding is in the library [go-modbus-trace](https://github.com/otfabric/go-modbus-trace), whose CLI `mbtracectl` is the headless way to do the same on a gateway or in CI.

**Status: milestone M3 of M4**: open a trace, confirm its Modbus flows, browse its timeline with filters and a detail view down to the bytes and the packets, see its devices, connections, poll patterns, address coverage with value histories and capture quality, and export it as an MBCAP capture that `modbusctl` can replay, as a timeline (CSV, JSON Lines) or as a pcapng sub-trace. Packaging (M4) follows. Start with [AGENTS.md](AGENTS.md), then [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/UX.md](docs/UX.md), [docs/BRANDING.md](docs/BRANDING.md) and [docs/PROPOSAL-REVIEW.md](docs/PROPOSAL-REVIEW.md). `modbus-capture-claude-proposal.md` is the superseded original proposal.

## Development

```sh
make frontend-install    # once: npm ci
make check               # Go and frontend checks
make build-gui           # ./bin/modbus-trace-explorer (cgo; the platform's webview)
cd frontend && npm run dev   # the user interface over the mock engine, in a browser (?rows=10000000, ?empty)
make spike               # the M0 index measurements (SPIKE_ROWS, default 10,000,000)
```

## License

MIT
