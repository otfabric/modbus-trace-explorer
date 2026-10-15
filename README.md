# OT Fabric Modbus Trace Explorer

A desktop tool to understand Modbus/TCP traffic recorded by someone else. Open a packet trace (pcap or pcapng from tcpdump, Wireshark, dumpcap or tshark), confirm which flows are Modbus, and get a timeline in Modbus terms (connect, read 62 holding registers at 0x1000, write 1 register, exception, no response, disconnect), with breakdowns per device, unit, connection, poll pattern and address, a detail view down to the bytes and packets, and exports:

- **MBCAP**, so a passive capture of a real installation can be replayed as a virtual device with [`modbusctl server replay`](https://github.com/otfabric/modbusctl);
- the timeline as CSV or JSON Lines;
- a filtered sub-trace as pcapng.

It does not capture live (use tcpdump or Wireshark), never talks to devices and never writes to Modbus. All trace understanding is in the library [go-modbus-trace](https://github.com/otfabric/go-modbus-trace), whose CLI `mbtracectl` is the headless way to do the same on a gateway or in CI.

**Status: not implemented yet.** This repository starts from its handoff documents. Start with [AGENTS.md](AGENTS.md), then [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/UX.md](docs/UX.md), [docs/BRANDING.md](docs/BRANDING.md) and [docs/PROPOSAL-REVIEW.md](docs/PROPOSAL-REVIEW.md). `modbus-capture-claude-proposal.md` is the superseded original proposal.

## License

MIT
