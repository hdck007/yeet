# Live A/B benchmark: what yeet costs in a real session

Measured 2026-09-14 on `eslint/eslint` @ `24310e3a` (2,363 tracked files), model
`sonnet`, 3 reps per arm, rotating order, pristine copy of the target per run,
3 warmup sessions discarded. Harness: `scripts/bench-sim.sh`.

## The honest headline

**On this workload, the benchmark cannot distinguish yeet from running without it,
and cannot distinguish yeet from rtk.** The per-run noise is larger than any effect
being measured. What it *can* resolve is the configuration yeet used to ship, which
was far worse than both.

| configuration | result | confidence |
|---|---|---|
| The old blocking hook set | **+35% cost vs no yeet** | solid — gap far exceeds noise, and the mechanism is independently visible in turn counts (16 vs 11) |
| Current hook set vs no yeet | no measurable difference | n=3; noise dominates |
| Current hook set vs rtk | no measurable difference | n=3; noise dominates |

## Why the blocking set lost, and why that one is trustworthy

It installed five `PreToolUse` hooks rejecting `Read`, `Glob`, `Grep`, `Write` and
`Edit` with *"BLOCKED: use `yeet ...` instead"*. Each block forces the model to
reformulate the call as Bash — an extra turn, and every turn re-sends the whole
accumulated context. That arm ran **16 turns against native's 11**.

This conclusion does not rest on a token or cache measurement. The turn count is a
direct, countable mechanism, and a 1.35x cost gap cannot be produced by the noise
described below. Blocked tools also stay in the tool schema, so their definitions are
paid for on every request whether or not the model reaches for one.

## What the noise actually is

Two effects, both large enough to swamp the thing being measured.

**Cold-cache ramp.** The first session of a fresh benchmark reports a much larger
`cache_creation` than every session after it:

```
warmup 1  cache_create = 33,360
warmup 2  cache_create =  8,631
warmup 3  cache_create = 12,704
```

Whichever arm draws the first slot is charged for it. Rotating arms spreads that
distortion evenly but leaves it in the numbers, so `--warmup N` now runs and discards
sessions before measuring. Default 3.

**`cache_creation` is noisy even warm.** Across warm runs it ranged **8,554 to
27,563** — a 3.2x swing on identical configuration. Any comparison resting on this
field at n=3 is reading noise. A single `rtk` run with `cache_creation = 27,563` was
enough on its own to move that arm from "cheaper than native" to "+11.7%".

**Session totals track task length, not efficiency.** Runs varied 11-15 turns on the
same prompt. Always compare turn-matched, and treat cells with n=1 as anecdote.

## Measure cost, not a raw token sum

An earlier revision of this document led with "billed input tokens", summing
`input + cache_creation + cache_read` at 1:1. That is wrong: cache reads bill at
roughly **0.1x** and cache creation at **1.25x**, so the sum is dominated by the
cheapest class. Use `total_cost_usd` as the API reports it. Token counts are useful
detail, never the headline.

## Bugs this benchmark found

Each was discovered by reading transcripts for commands the model ran twice.

**`git log --oneline` reported `no commits` for a file with two.** `renderGitLog`
parses yeet's own `--pretty` layout; a caller's `--oneline` overrode it, every line
failed the 4-field split, and it reported an empty history. The never-worse guard then
*preferred* that answer, because `no commits` (11 bytes) is shorter than the truth
(108 bytes). **A wrong answer won on size.**

**Stripping the caller's format flag was the wrong fix.** The first attempt removed
`--pretty`/`--format`/`--oneline` to protect the parser. That silently returned a
shape the caller had not asked for, so they re-ran to get it — one run spent **three
turns on a single `git log`**. An explicit format is now passed straight to git,
unrendered. rtk had this right already.

**`git diff` returned a stat line with no hunks**, so agents ran `command git diff`.

**`--raw` was a no-op on five commands.** A local `rawOutput :=` shadowed the
package-level flag — while six renderers print *"re-run with `--raw` for all"*.

**`find`/`glob` logged a fabricated saving** from a hardcoded `len(rendered)*2`
estimate written to analytics as if measured. Now marked `BaselineSynthetic`.

The common thread: **"run both, keep the smaller" optimises for bytes, and
byte-minimisation systematically selects for information loss.** `printBetterN` sees
two strings and cannot know that `no commits` is a lie. Fidelity has to be guaranteed
by each renderer before the size comparison is allowed to choose.

## Offline vs live

`scripts/bench-offline.sh` reports large per-command savings. That answers a different
question — it scores against raw shell output (`grep -rn` = 109,045 bytes) that an
agent would rarely receive, because it would use the native `Grep` tool instead
(2,231 bytes in context). Quote it as "bytes per command", never "cost of a task".

Against rtk 0.49.0 on the offline suite, rtk is substantially smaller overall. An
earlier comparison showing yeet ahead used rtk 0.35.0 — fourteen versions stale. Pin
and print the version of anything you benchmark against.

## Limits

- **n=3 per arm, and turn-matched cells are often n=1.** Nothing here supports a
  specific percentage for the current hook set.
- **This is yeet's weakest terrain.** Search, read, edit and git are where Claude
  Code's native tools are already compact. The verbose commands yeet has no native
  competitor for — real `npm install`, `tsc`, lint, test runners — were never
  exercised. `npm install --dry-run` was the only build-ish command and it was small.
- **Do not pool across builds.** yeet was measured under several binaries while
  iterating; those medians are not comparable and must not be averaged.
- `/usage` cannot measure any of this: `rate_limit_event.utilization` is 1%-granular,
  account-wide, and emitted only on threshold crossings.

## Reproducing

```bash
bash scripts/bench-sim.sh --reps 3 --warmup 3 --model sonnet \
  --arms "native yeet yeet-bash yeet-sel rtk" --yeet-bin ./yeet
```

Pin the binary with `--yeet-bin`. The first run of this investigation measured an
installed yeet that had diverged from source and inflated its cost 3.2x.

For a verdict on your own work rather than one synthetic task, use
`scripts/yeet-ab.sh`: `claude --yeet` versus plain `claude` across real sessions,
recorded to a datalake with failures, repeats, bypasses and truncation flags. That is
the instrument that can actually answer whether yeet pays off, because it varies the
workload — which is the variable that decides it.
