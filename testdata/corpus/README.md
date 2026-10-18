# Test corpus

Packet traces and MBCAP captures for tests, demos and screenshots, all synthetic: written by
`internal/corpus` (`make golden` rewrites them; a test checks they are what the generator makes).
`manifest.json` says how the application must take each file; the engine's corpus test, the fuzz test
and `make smoke-corpus` (the built application on every file) hold it to that.

- **realistic/**: scenarios as a site would record them; use these for screenshots and demos.
- **edge/**: valid input that is hard to read: losses, odd link types and file variants, pairing trouble,
  clocks, scale, IPv6, TLS, unusual captures.
- **broken/**: empty, garbage, cut, corrupt and misnamed files. "error" means a clear message and no
  crash; "ok" means what can be read is shown.

| File | Kind | Expect | About |
|---|---|---|---|
| `broken/bad-line.mbcap.jsonl` | capture | ok, ≥ 100 | A JSON Lines capture with a broken line in the middle: the samples before it are shown, and Quality says where it broke. |
| `broken/cut-mid-block.pcapng` | trace | ok, ≥ 3 | A pcapng cut in the middle of a block. |
| `broken/cut-mid-packet.pcap` | trace | ok, ≥ 3 | A pcap cut in the middle of a packet (the capture was copied while it was written). |
| `broken/cut.mbcap` | capture | ok, ≥ 1 | A binary capture cut in the middle of a data block. |
| `broken/cut.mbcap.csv` | capture | ok, ≥ 100 | A CSV capture cut in the middle of a row. |
| `broken/cut.mbcap.jsonl` | capture | ok, ≥ 100 | A JSON Lines capture cut in the middle of a line: the samples before it are shown. |
| `broken/empty.mbcap` | capture | error | A capture of zero bytes. |
| `broken/empty.pcap` | trace | error | A file of zero bytes. |
| `broken/flipped-byte.mbcap` | capture | ok, ≥ 1 | A binary capture with one byte changed a third of the way in (that block's CRC no longer matches): the samples before it are shown, and Quality says it is damaged. |
| `broken/garbage.mbcap.jsonl` | capture | error | JSON Lines that are not a capture. |
| `broken/garbage.pcapng` | trace | error | Random bytes with a trace's name. |
| `broken/gzip-cut.pcapng.gz` | trace | ok, ≥ 1 | A gzip trace cut before its end: what decompresses is read. |
| `broken/gzip-garbage.pcap.gz` | trace | error | A gzip header followed by garbage. |
| `broken/header-only.pcap` | trace | empty | A pcap header and no packet: a trace with nothing in it. |
| `broken/huge-record-length.pcap` | trace | ok, ≥ 3 | A packet record that claims 2 GiB: the reader stops there and keeps what came before. |
| `broken/library-crc-corrupt.mbcap` | capture | error | From go-modbus-capture's test vectors: a block whose CRC is wrong. |
| `broken/library-no-index.mbcap` | capture | ok | From go-modbus-capture's test vectors: a capture without its index block. |
| `broken/library-truncated.mbcap` | capture | ok | From go-modbus-capture's test vectors: cut before its end. |
| `broken/library-unknown-block.mbcap` | capture | ok | From go-modbus-capture's test vectors: a block type the reader does not know (skipped). |
| `broken/pcap-named.mbcap.csv` | capture | error | A packet trace named like a CSV capture: the name says capture, the content does not. |
| `broken/text-file.pcap` | trace | error | A text file renamed .pcap. |
| `broken/unknown-link-type.pcap` | trace | empty | A pcap whose header names a link type no reader knows (65000): no packet can be decoded, so no Modbus flow is found and the link type is counted as unsupported. |
| `broken/wrong-columns.mbcap.csv` | capture | error | A CSV whose columns are not those of a capture. |
| `edge/clock-backwards.pcapng` | trace | ok, ≥ 20 | The capture host's clock steps back by two seconds in the middle (time disorder). |
| `edge/ip-fragments.pcap` | trace | ok, ≥ 10 | Responses that do not fit one packet arrive as IP fragments. |
| `edge/ipv6.pcapng` | trace | ok, ≥ 10 | Modbus/TCP over IPv6. |
| `edge/link-linux-cooked.pcap` | trace | ok, ≥ 10 | Linux cooked capture (tcpdump -i any). |
| `edge/link-loopback.pcap` | trace | ok, ≥ 10 | BSD loopback (captured on lo0, a simulator on the same machine). |
| `edge/link-raw-ip.pcap` | trace | ok, ≥ 10 | Raw IP, no link layer. |
| `edge/link-vlan-qinq.pcapng` | trace | ok, ≥ 10 | Ethernet with two VLAN tags (802.1ad and 802.1Q). |
| `edge/lossy-link.pcapng` | trace | ok, ≥ 20 | A bad link: lost segments (gaps and resyncs), retransmissions, a retransmission with different bytes, segments out of order, ADUs split byte by byte, keepalives. |
| `edge/many-connections.pcapng` | trace | ok, ≥ 300 | 300 short connections, one poll each (a client that reconnects for every read), from 12 clients to 25 devices: long lists in the Conversations and Devices views. |
| `edge/midstream.pcapng` | trace | ok, ≥ 40 | The capture starts while the connections are open: no handshakes, the server side is inferred from the traffic (guessed roles). |
| `edge/no-end.mbcap.jsonl` | capture | ok, ≥ 3 | A capture never closed (the recorder was killed): no end record. |
| `edge/no-samples.mbcap` | capture | empty | A capture that holds targets and streams but no sample (a read that got nothing). |
| `edge/noise-around-modbus.pcap` | trace | ok, ≥ 10 | Modbus/TCP between ARP, UDP and ICMP packets that are not TCP (counted as unsupported). |
| `edge/nonstandard-port.pcapng` | trace | ok, ≥ 30 | Modbus/TCP on port 5020 next to SSH on 22: the scan proposes 5020. |
| `edge/pcap-big-endian.pcap` | trace | ok, ≥ 10 | Classic pcap written big endian. |
| `edge/pcap-nanoseconds.pcap` | trace | ok, ≥ 10 | Classic pcap with nanosecond timestamps. |
| `edge/pcapng-big-endian.pcapng` | trace | ok, ≥ 10 | pcapng written big endian. |
| `edge/pcapng-ns-resolution.pcapng` | trace | ok, ≥ 10 | pcapng with nanosecond timestamp resolution (if_tsresol 9). |
| `edge/serial-and-tls-targets.mbcap` | capture | ok, ≥ 6 | A capture of a serial RTU device and a TLS device (modbusctl reading them), with labels in several scripts and a very long one. |
| `edge/serial-and-tls-targets.mbcap.jsonl` | capture | ok, ≥ 6 | The same, as JSON Lines. |
| `edge/simple-packets.pcapng` | trace | ok, ≥ 1 | pcapng Simple Packet Blocks: packets without timestamps. |
| `edge/snaplen-64.pcap` | trace | ok, ≥ 5 | Captured with a snap length of 64 bytes (tcpdump -s 64): long ADUs are cut. |
| `edge/tid-and-pairing.pcapng` | trace | ok, ≥ 10 | Pairing trouble: two requests with the same transaction ID outstanding, a response without a request, a request never answered, malformed PDUs (wrong byte counts, an exception code 0), a non-zero protocol ID, broadcast unit 0 and unit 255. |
| `edge/time-disorder.mbcap` | capture | ok, ≥ 6 | Samples out of time order, as a merge of two recorders can leave them. |
| `edge/tls-and-modbus.pcapng` | trace | ok, ≥ 10 | Modbus/TCP Security on port 802 (encrypted, not decodable) next to plain Modbus/TCP on 502. |
| `edge/two-interfaces-drops.pcapng` | trace | ok, ≥ 10 | Captured on two interfaces at once, some packets seen on both, and the capture host reporting dropped packets. |
| `realistic/function-tour.pcapng` | trace | ok, ≥ 20 | Every kind of function the decoder knows, answered and as exceptions: coils, discrete inputs, registers, writes, FC23, FC24 FIFO, FC20/21 file records, FC08 diagnostics, FC11/FC17, FC43/14 device identification over two pages, and a vendor function. |
| `realistic/plant-ring_00001.pcapng` | trace | ok, ≥ 300 | Part 1 of 3 of the plant as a ring buffer of five-minute files (as dumpcap -b writes it): open the three together, in any order, as one trace. |
| `realistic/plant-ring_00002.pcapng` | trace | ok, ≥ 300 | Part 2 of 3 of the plant as a ring buffer of five-minute files (as dumpcap -b writes it): open the three together, in any order, as one trace. |
| `realistic/plant-ring_00003.pcapng` | trace | ok, ≥ 300 | Part 3 of 3 of the plant as a ring buffer of five-minute files (as dumpcap -b writes it): open the three together, in any order, as one trace. |
| `realistic/plant-scada-change-only.mbcap` | capture | ok, ≥ 100 | The plant exported change-only: repeated identical responses left out. |
| `realistic/plant-scada.mbcap` | capture | ok, ≥ 1500 | The plant's reads exported as an MBCAP capture (mbcap), as modbusctl server replay would serve them. |
| `realistic/plant-scada.mbcap.csv` | capture | ok, ≥ 1500 | The plant's reads exported as an MBCAP capture (csv), as modbusctl server replay would serve them. |
| `realistic/plant-scada.mbcap.jsonl` | capture | ok, ≥ 1500 | The plant's reads exported as an MBCAP capture (jsonl), as modbusctl server replay would serve them. |
| `realistic/plant-scada.pcapng` | trace | ok, ≥ 2000 | 15 minutes of a small plant: SCADA polls a PV inverter, an energy meter and a battery gateway (units 1–3); an HMI writes setpoints; device identification at the start; the meter rejects one block (illegal data address); battery unit 3 stops answering for a minute and its gateway then reports it unreachable; the meter resets its connection once; a few retransmissions. |
| `realistic/plant-scada.pcapng.gz` | trace | ok, ≥ 2000 | The plant trace compressed with gzip (decompressed into the cache while open). |
| `realistic/rtu-gateway.pcap` | trace | ok, ≥ 400 | Classic pcap: a Modbus/TCP-to-RTU gateway with eight units, polled round-robin for coils, discrete inputs and registers; units 6 and 8 are not connected (gateway exceptions 0x0A and 0x0B). |
| `realistic/site-diagnosis.pcapng` | trace | ok, ≥ 4000 | The demo and screenshot trace: 10 minutes of a site with the usual problems. SCADA polls a PV inverter, a battery and four energy meters behind a Modbus/TCP-to-RTU gateway; an EMS polls the inverter's block again every 250 ms (duplicate polling); the gateway's meters answer in 90 to 400 ms and meter 4 is offline (exception 0x0B after the gateway's 1 s timeout), so the SCADA's 1 s cycle stretches to about 1.5 s; meter 2 misses a poll now and then; SCADA reads the battery at 40001 (a 4x-offset mistake, illegal data address); an HMI writes the battery's setpoint every two minutes, once out of range (illegal data value); the EMS resets its connection once; one retransmission. |
