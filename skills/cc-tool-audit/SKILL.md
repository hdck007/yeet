---
name: cc-tool-audit
description: Audit the internal tool surface of an installed Claude Code build and diff it against previous releases — which tools exist, what parameters they take, and where their output caps sit. Use when the user asks "what changed in the new Claude Code", "re-run the tool audit", "did the caps move", "audit claude code", "check the internal tools", after a Claude Code upgrade, or before trusting yeet's measured savings on a new release.
---

# Auditing the Claude Code tool surface

yeet's entire value proposition is a comparison: is routing this command through
yeet cheaper than letting the native tool handle it? That comparison depends on
numbers Anthropic does not publish and does not put in a changelog — per-tool
output caps, Read's token budget, the file-state cache thresholds. When one of
those moves, a saving can silently invert into a loss.

This audit reads them straight out of the shipped binary.

## Run it

```bash
python3 scripts/cc-tool-audit.py snapshot          # record the installed build
python3 scripts/cc-tool-audit.py diff              # two newest snapshots
python3 scripts/cc-tool-audit.py diff A.json B.json
```

Snapshots land in `.cc-audit/<version>.json`. Keep them — old builds are pruned
by the auto-updater within days, so a snapshot is often the only remaining
record of what a release did.

## What it extracts

| | |
|---|---|
| Tool registry | every internal tool, its `searchHint`, its `maxResultSizeChars` |
| Input schemas | each tool's parameters, with type, optionality and defaults |
| Watched limits | Read's line and token caps, PDF page cap, max editable size, file-state cache sizing |

## Reading the output

`ADDED` / `REMOVED` / `CAP` / `PARAM+` / `PARAM-` / `PARAM~` are real findings.

`STALE` is not a finding — it means a pattern stopped matching in one of the
builds, so that field could not be read and therefore cannot be compared. Fix
the pattern before drawing a conclusion.

## Why it is built the way it is

The bundle is a Bun single-file executable with the JavaScript embedded
verbatim. Identifiers are minified, and **they are regenerated on every build**.
Anything anchored on a minified name works once and then silently returns
nothing:

- The wrapper around a tool definition was `Oi({name:` in 2.1.231 and
  `Fi({name:` in 2.1.228. Matching the literal name yielded *zero tools* on the
  older build, and the diff cheerfully reported all 64 as newly added.
- Bash is `di` in one release and `mi` in another.
- The Zod helpers (`N()` string, `ct()` number, `Mr()` enum) are minified too,
  which is why parameter types can come back unreadable on an older build.

So: **key everything on prose, never on symbols.** Tools are identified by
`searchHint`, which is written for humans and stays stable across builds. The
wrapper is matched as a wildcard.

The watched numeric constants are the exception — they genuinely are symbols
(`XTr`, `j0b`) with no prose anchor, so they will need updating from time to
time. That is why an unreadable limit reports `STALE` rather than a value.

## The guard that matters

A failed extraction and a genuinely tool-less build produce identical output.
`snapshot` therefore counts `searchHint:"` occurrences in the binary and refuses
to record a snapshot that accounts for less than 80% of them. Without that
check, a future rename produces a confident, entirely wrong diff.

If it refuses, the bundle shape changed. Update the patterns in `audit()`.

## After a Claude Code upgrade

1. `snapshot` the new build.
2. `diff` against the previous snapshot.
3. If any cap moved, re-run `/yeet-benchmark` — the break-even point has shifted
   and the recorded savings are stale.
