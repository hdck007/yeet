#!/usr/bin/env python3
"""Measure what yeet's unrouted subcommands would save if the hook routed to them.

Each pair is (native command, yeet equivalent). The yeet side exists and works;
the PreToolUse rewrite table simply never produces it, so the agent runs the
native form and pays full price.

Output is capped at Bash's maxResultSizeChars (30000) on both sides, because
that is what actually reaches the model.

  python3 scripts/gap-measure.py [path-to-yeet]
"""

import subprocess
import sys

YEET = sys.argv[1] if len(sys.argv) > 1 else "yeet"
BASH_CAP = 30000

PAIRS = [
    ("tree",            ["find", ".", "-not", "-path", "*/.git/*"],   ["tree"]),
    ("wc",              ["wc", "-l"],                                  ["wc", "-l"]),
    ("glob",            ["find", ".", "-name", "*.go"],                ["glob", "**/*.go"]),
    ("deps",            ["cat", "go.mod"],                             ["deps"]),
    ("env",             ["env"],                                       ["env"]),
    ("json",            ["cat", ".cc-audit/2.1.231.json"],             ["json", ".cc-audit/2.1.231.json"]),
]

GO_FILES = ["internal/cli/read.go", "internal/cli/grep.go", "internal/filter/signatures.go"]


def run(argv):
    try:
        p = subprocess.run(argv, capture_output=True, text=True, timeout=120)
        return (p.stdout + p.stderr)[:BASH_CAP]
    except Exception as e:
        return f"<failed: {e}>"


def tok(s):
    return (len(s) + 3) // 4


def main():
    print(f"{'CASE':<10} {'NATIVE':>9} {'YEET':>9} {'SAVED':>8}   NOTE")
    print("-" * 74)
    for name, native, yeetargs in PAIRS:
        if name == "wc":
            native = native + GO_FILES
            yeetargs = yeetargs + GO_FILES
        n = run(native)
        y = run([YEET] + yeetargs)
        tn, ty = tok(n), tok(y)
        note = ""
        if y.startswith("<failed") or not y.strip():
            note = "!! yeet produced NOTHING - not a saving"
        elif ty >= tn:
            note = "no gain"
        saved = f"{(tn - ty) * 100 // max(tn, 1)}%"
        print(f"{name:<10} {tn:>9} {ty:>9} {saved:>8}   {note}")


if __name__ == "__main__":
    main()
