#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Run the built application on every file of testdata/corpus (make smoke-corpus).

Each file is opened with `-smoke`: the window imports it without a click
and reports. A file the manifest expects to show transactions must exit 0;
an empty or refused one must exit 1 with a reason. Anything else (a crash,
a signal, the 3-minute timeout's exit 2, a hang) fails. The ring buffer is
also opened as its three files at once, in the wrong order.

Usage: scripts/smoke_corpus.py [binary] [--only SUBSTRING]
"""

import json
import os
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CORPUS = os.path.join(ROOT, "testdata", "corpus")


def main() -> int:
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    only = sys.argv[sys.argv.index("--only") + 1] if "--only" in sys.argv else ""
    if only in args:
        args.remove(only)
    binary = args[0] if args else os.path.join(ROOT, "bin", "modbus-trace-explorer")
    with open(os.path.join(CORPUS, "manifest.json")) as f:
        manifest = json.load(f)
    runs = [([e["file"]], 0 if e["expect"] == "ok" and e.get("min_transactions", 0) > 0 else
             (None if e["expect"] == "ok" else 1)) for e in manifest]
    ring = [f"realistic/plant-ring_0000{k}.pcapng" for k in (3, 1, 2)]
    runs.append((ring, 0))
    failed = 0
    for files, want in runs:
        name = " + ".join(files)
        if only and only not in name:
            continue
        cmd = [binary, "-smoke"] + [os.path.join(CORPUS, f) for f in files]
        start = time.monotonic()
        try:
            p = subprocess.run(cmd, capture_output=True, text=True, timeout=240)
            code, out = p.returncode, (p.stdout + p.stderr).strip().splitlines()
        except subprocess.TimeoutExpired:
            code, out = "hang", []
        took = time.monotonic() - start
        # want None: the file may show rows or nothing (exit 0 or 1).
        ok = code in (0, 1) if want is None else code == want
        failed += 0 if ok else 1
        last = out[-1] if out else ""
        print(f"{'ok  ' if ok else 'FAIL'} {code!s:>4} {took:5.1f}s  {name}  {last[:110]}")
    print(f"\n{len(runs)} runs, {failed} failed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
