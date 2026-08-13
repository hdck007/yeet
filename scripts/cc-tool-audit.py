#!/usr/bin/env python3
"""Audit the internal tool surface of an installed Claude Code build.

yeet's value depends on facts about the host agent that are not in any changelog:
which tools exist, what parameters they take, and where their output caps sit.
Those caps decide whether routing a command through yeet is cheaper than letting
the native tool handle it, so a release that moves one can silently invert a
saving into a loss. This snapshots the surface and diffs releases.

  python3 scripts/cc-tool-audit.py snapshot        # record the installed build
  python3 scripts/cc-tool-audit.py diff            # two newest snapshots
  python3 scripts/cc-tool-audit.py diff A.json B.json

Snapshots land in .cc-audit/<version>.json.

Identity note: the bundle is minified and identifiers are regenerated on every
build, so `na` is Read in one release and something else in the next. Everything
here is therefore keyed on searchHint — prose written for humans, stable across
builds — never on the symbol.
"""

import json
import re
import sys
from pathlib import Path

AUDIT_DIR = Path(__file__).resolve().parent.parent / ".cc-audit"

# Limits worth watching. A change in any of these moves the break-even point
# between a yeet command and the native tool it replaces.
WATCHED = {
    "read_default_line_limit": r"XTr=(\d+)",
    "read_token_cap": r"j0b=(\d+)",
    "max_pdf_pages": r"L9e=(\d+)",
    "max_editable_file_bytes": r"g5d=(\d+)",
    "readfilestate_max_entries": r"hG=(\d+),",
    "readfilestate_max_bytes": r"v9_=(\d+)",
    "readfilestate_inline_threshold": r"T9_=(\d+)",
}


def find_binary(explicit=None):
    if explicit:
        return Path(explicit)
    versions = Path.home() / ".local/share/claude/versions"
    if versions.is_dir():
        builds = [p for p in versions.iterdir() if p.is_file()]
        if builds:
            return max(builds, key=lambda p: p.stat().st_mtime)
    local = Path.home() / ".local/bin/claude"
    if local.exists():
        return local.resolve()
    sys.exit("could not locate a Claude Code binary; pass a path explicitly")


def scan(s, i):
    """Return the index just past the bracket opened at i, string-aware."""
    pairs = {"(": ")", "{": "}", "[": "]"}
    stack, n = [], len(s)
    while i < n:
        c = s[i]
        if c in "'\"`":
            q, i = c, i + 1
            while i < n:
                if s[i] == "\\":
                    i += 2
                    continue
                if q == "`" and s[i] == "$" and i + 1 < n and s[i + 1] == "{":
                    i = scan(s, i + 1)
                    continue
                if s[i] == q:
                    i += 1
                    break
                i += 1
            continue
        if c in pairs:
            stack.append(pairs[c])
            i += 1
            continue
        if c in ")}]":
            if not stack:
                return i
            stack.pop()
            i += 1
            if not stack:
                return i
            continue
        i += 1
    return n


def describe(expr):
    """Decode one Zod field expression into a portable description."""
    types = [
        (r"\bN\(\)", "string"), (r"\bct\(\)", "number"), (r"\bqt\(\)", "boolean"),
        (r"\bgt\(", "array"), (r"\bSe\(\{", "object"), (r"\bri\(\{", "object"),
        (r"\bMr\(\[", "enum"), (r"\bIt\(", "literal"), (r"\bno\(", "record"),
    ]
    kind = next((n for pat, n in types if re.search(pat, expr)), "?")
    if kind == "enum":
        m = re.search(r"Mr\(\[(.*?)\]\)", expr, re.S)
        if m:
            kind = "enum[" + re.sub(r"\s+", "", m.group(1)) + "]"
    default = None
    dm = re.search(r"\.default\((!0|!1|[^)]*)\)", expr)
    if dm:
        default = {"!0": "true", "!1": "false"}.get(dm.group(1), dm.group(1))
    return {"type": kind, "optional": ".optional()" in expr, "default": default}


def split_fields(body):
    out, i, n = [], 0, len(body)
    key_re = re.compile(r'([A-Za-z_$][\w$]*|"[^"]+")\s*:')
    while i < n:
        m = key_re.match(body, i)
        if not m:
            i += 1
            continue
        key, j, k = m.group(1).strip('"'), m.end(), m.end()
        while k < n:
            c = body[k]
            if c in "'\"`":
                q, k = c, k + 1
                while k < n:
                    if body[k] == "\\":
                        k += 2
                        continue
                    if q == "`" and body[k] == "$" and k + 1 < n and body[k + 1] == "{":
                        k = scan(body, k + 1)
                        continue
                    if body[k] == q:
                        k += 1
                        break
                    k += 1
                continue
            if c in "({[":
                k = scan(body, k)
                continue
            if c == ",":
                break
            k += 1
        out.append((key, body[j:k]))
        i = k + 1
    return out


def audit(path):
    data = path.read_bytes().decode("latin-1")

    factories = {}
    for m in re.finditer(r"([A-Za-z_$][\w$]*)=we\(\(\)=>", data):
        j = m.start() + len(m.group(1)) + 3
        factories.setdefault(m.group(1), data[m.end():scan(data, j) - 1])

    tools = {}
    # The helper that wraps a tool definition is itself minified and gets a new
    # name every release (Oi in 2.1.231, Fi in 2.1.228), so it is matched as a
    # wildcard. Anchoring on the literal name silently yields zero tools.
    for m in re.finditer(r"[A-Za-z_$][\w$]*\(\{name:([A-Za-z_$][\w$]*)[,}]", data):
        start = data.index("{", m.start())
        body = data[start:scan(data, start)]
        hint = re.search(r'searchHint:"([^"]*)"', body)
        if not hint:
            continue
        cap = re.search(r"maxResultSizeChars:([0-9e.+/]*)", body)
        fac = re.search(r"inputSchema\(\)\{return ([A-Za-z_$][\w$]*)\(\)", body)

        params = {}
        if fac and fac.group(1) in factories:
            fbody = factories[fac.group(1)]
            om = re.search(r"\b(ri|Se)\(\{", fbody)
            if om:
                st = fbody.index("{", om.start())
                for key, val in split_fields(fbody[st + 1:scan(fbody, st) - 1]):
                    params[key] = describe(val)

        tools[hint.group(1)] = {
            "symbol": m.group(1),
            "max_result_chars": cap.group(1) if cap else None,
            "params": params,
        }

    limits = {}
    for name, pat in WATCHED.items():
        found = re.search(pat, data)
        limits[name] = int(found.group(1)) if found else None

    return {
        "version": path.name,
        "binary": str(path),
        "size_bytes": path.stat().st_size,
        "tool_count": len(tools),
        "searchhint_occurrences": len(re.findall(r'searchHint:"', data)),
        "limits": limits,
        "tools": tools,
    }


def cmd_snapshot(argv):
    binary = find_binary(argv[0] if argv else None)
    print(f"auditing {binary} ...", file=sys.stderr)
    result = audit(binary)

    # A failed extraction and a genuinely tool-less build look identical in the
    # output, and the diff would then report every tool as removed. Refuse to
    # record a snapshot that cannot account for most of the searchHint strings
    # actually present in the binary.
    seen, expected = result["tool_count"], result["searchhint_occurrences"]
    if expected and seen < expected * 0.8:
        sys.exit(
            f"extraction recovered only {seen} of ~{expected} tools in {binary.name}.\n"
            "The bundle shape changed; update the patterns in audit() before trusting a diff."
        )

    AUDIT_DIR.mkdir(exist_ok=True)
    out = AUDIT_DIR / f"{result['version']}.json"
    out.write_text(json.dumps(result, indent=1, sort_keys=True))
    print(f"{result['tool_count']} tools -> {out}")
    for k, v in result["limits"].items():
        print(f"  {k:34s} {v}")
    return result


def newest_two():
    snaps = sorted(AUDIT_DIR.glob("*.json"), key=lambda p: p.stat().st_mtime)
    if len(snaps) < 2:
        sys.exit("need two snapshots to diff; snapshot another release first")
    return snaps[-2], snaps[-1]


def cmd_diff(argv):
    if len(argv) == 2:
        a_path, b_path = Path(argv[0]), Path(argv[1])
    else:
        a_path, b_path = newest_two()
    a, b = json.loads(a_path.read_text()), json.loads(b_path.read_text())
    print(f"{a['version']}  ->  {b['version']}\n")

    # The Zod helpers and the numeric constants are minified too, so a pattern
    # that stops matching yields None / "?" rather than a real reading. Those
    # are unknowns, not changes: reporting them as movement would manufacture
    # findings out of our own staleness.
    changed = False
    stale = []
    for k in sorted(set(a["limits"]) | set(b["limits"])):
        old, new = a["limits"].get(k), b["limits"].get(k)
        if old is None or new is None:
            if old != new:
                stale.append(f"{k} (unreadable in {a['version'] if old is None else b['version']})")
            continue
        if old != new:
            changed = True
            print(f"LIMIT   {k}: {old} -> {new}")

    old_t, new_t = set(a["tools"]), set(b["tools"])
    for hint in sorted(new_t - old_t):
        changed = True
        print(f"ADDED   {hint}")
    for hint in sorted(old_t - new_t):
        changed = True
        print(f"REMOVED {hint}")

    for hint in sorted(old_t & new_t):
        ta, tb = a["tools"][hint], b["tools"][hint]
        if ta["max_result_chars"] != tb["max_result_chars"]:
            changed = True
            print(f"CAP     {hint}: {ta['max_result_chars']} -> {tb['max_result_chars']}")
        pa, pb = ta["params"], tb["params"]
        for p in sorted(set(pb) - set(pa)):
            changed = True
            print(f"PARAM+  {hint}.{p} ({pb[p]['type']})")
        for p in sorted(set(pa) - set(pb)):
            changed = True
            print(f"PARAM-  {hint}.{p}")
        for p in sorted(set(pa) & set(pb)):
            if pa[p] == pb[p]:
                continue
            if "?" in (pa[p]["type"], pb[p]["type"]):
                stale.append(f"{hint}.{p} (type unreadable)")
                continue
            changed = True
            print(f"PARAM~  {hint}.{p}: {pa[p]} -> {pb[p]}")

    if not changed:
        print("no changes to the tool surface")
    if stale:
        print(f"\n{len(stale)} field(s) could not be read in one of the builds —")
        print("the decoder patterns need updating before these can be compared:")
        for s in stale[:10]:
            print(f"  STALE  {s}")
        if len(stale) > 10:
            print(f"  ... and {len(stale) - 10} more")
    return 0


def main():
    argv = sys.argv[1:]
    cmd = argv[0] if argv else "snapshot"
    if cmd == "snapshot":
        cmd_snapshot(argv[1:])
    elif cmd == "diff":
        cmd_diff(argv[1:])
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()
