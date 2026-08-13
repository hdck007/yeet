#!/usr/bin/env python3
"""Re-measure `yeet read` against the native Read tool after the fallback fix.

Native Read paginates a whole-file read at a 5000-token budget. yeet's output
travels through the shell, which truncates at 30000 characters. Both caps are
applied here so the comparison reflects what actually reaches the model.

  python3 scripts/read-remeasure.py [path-to-yeet] file [file...]
"""

import os
import subprocess
import sys

READ_TOKEN_CAP = 5000
BASH_CAP = 30000


def tok(s):
    return (len(s) + 3) // 4


def native(path):
    with open(path, encoding="utf-8", errors="replace") as fh:
        lines = fh.read().split("\n")[:2000]
    rendered = "".join(f"{i + 1:6d}\t{l}\n" for i, l in enumerate(lines))
    return min(tok(rendered), READ_TOKEN_CAP)


def yeet(binary, path, *flags):
    env = dict(os.environ, YEET_NO_READ_CACHE="1")
    p = subprocess.run([binary, "read", path, *flags],
                       capture_output=True, text=True, env=env)
    return p.stdout[:BASH_CAP]


def main():
    binary = sys.argv[1]
    files = sys.argv[2:]
    print(f"{'FILE':<26}{'NATIVE':>8}{'default':>9}{'+aggr':>8}{'BEST':>7}  VERDICT")
    print("-" * 78)
    for f in files:
        if not os.path.isfile(f):
            continue
        n = native(f)
        d = yeet(binary, f)
        a = yeet(binary, f, "-l", "aggressive")
        td, ta = tok(d), tok(a)

        # A refusal is not a rendering: its cost is paid and then a second call
        # still has to happen, so the real price is refusal + follow-up.
        refused = d.startswith("yeet:")
        no_patterns = a.startswith("yeet: no signature patterns")
        best = td + ta if refused else min(td, ta)

        if no_patterns:
            verdict = "advises native Read (fallback fixed)"
        elif best < n:
            verdict = f"{(n - best) * 100 // n}% cheaper"
        else:
            verdict = f"{(best - n) * 100 // n}% MORE EXPENSIVE"
        print(f"{os.path.basename(f):<26}{n:>8}{td:>9}{ta:>8}{best:>7}  {verdict}")


if __name__ == "__main__":
    main()
