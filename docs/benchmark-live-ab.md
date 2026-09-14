# Live A/B benchmark: what yeet actually costs in a real session

Measured 2026-09-14 against `eslint/eslint` @ `24310e3a` (2,363 tracked files),
model `sonnet`, 3 reps per arm, rotating order, each run on a pristine copy of the
target at a byte-identical path. Harness: `scripts/bench-sim.sh`.

**The headline metric is `total_cost_usd`, as reported by the API.** An earlier
version of this document led with "billed input tokens", summing
`input + cache_creation + cache_read` at 1:1. That is wrong: cache reads bill at
roughly **0.1x** and cache creation at **1.25x**, so a raw token sum is dominated by
the cheapest class and hid a real saving. Every conclusion below is drawn from cost;
token counts appear only as supporting detail.

## Headline — one alternating run, arms interleaved

These three arms ran in a single benchmark (`sim-20260914-152912`), alternating, so
they share cache warmth and API conditions. No cross-run comparison is involved.

| arm | cost/run | vs native | cache_creation | turns |
|---|---:|---:|---:|---:|
| `yeet` — the old blocking set | $0.2915 | **+35%** | 33,133 | 16 |
| `native` — no yeet | $0.2159 | ref | 28,866 | 11 |
| `yeet-bash` — Bash rewrite only | **$0.2029** | **−6.0%** | 15,329 | 15 |

The shipped configuration (`yeet-sel`: Bash rewrite + Grep/Glob intercept,
Read/Write/Edit untouched) measured **$0.1885, −12.7%**, but in a *separate* run
(`sim-20260914-170104`). Treat that number as directional, not head-to-head.

## Where the saving actually is

Not in the raw token count — in **cache creation**, the expensive class:

| arm | cache_creation (mean) | cache_read (mean) |
|---|---:|---:|
| `native` | 28,866 | 352,223 |
| `yeet-sel` | **18,005** (−38%) | 397,454 |

Smaller tool results mean less *new* content written into the cache each turn. Cache
reads are larger in the yeet arm and barely matter, because they bill at ~0.1x. A
metric that adds them at full weight reports the opposite of the truth.

## Why the blocking set lost

It installed five `PreToolUse` hooks that rejected `Read`, `Glob`, `Grep`, `Write`
and `Edit` with *"BLOCKED: use `yeet ...` instead"*. Every block forces the model to
reformulate the call as Bash — an extra turn, and every turn re-sends the whole
accumulated context. The blocking arm ran 16 turns against native's 11.

Blocked tools also stay in the tool schema, so their definitions are paid for on
every request whether or not the model ever reaches for one.

## The bugs that made agents re-run commands

Counts of `command git` / `which git` / `type git` in the transcripts:

| arm | bypass + diagnose calls | turns |
|---|---:|---:|
| `yeet-sel` before fixes | 5 | 13–14 |
| `yeet-sel` after fixes | **0** | 12–13 |

**`git log --oneline` reported `no commits` for a file that had two.**
`renderGitLog` parses yeet's own `--pretty=format:%h|%an|%ar|%s`. A caller's
`--oneline` overrides that layout, every line failed the 4-field split, and the
renderer returned `no commits`. The never-worse guard then *preferred* that answer,
because `no commits` (11 bytes) is shorter than the real log (108 bytes). A wrong
answer won on size.

**`git diff` returned a numstat summary with no hunks**, on the documented theory
that "the per-file summary is what an agent needs to decide where to look". The
transcripts contradict it: agents asked for a diff, got a summary, and re-ran
`command git diff`.

Both are the same root problem. "Run both, keep the smaller" optimises for bytes,
and byte-minimisation systematically selects for information loss. `printBetterN`
sees two strings; it cannot know that `no commits` is a lie. Fidelity has to be
guaranteed by each renderer *before* the size comparison is allowed to choose, which
is what the per-renderer regression tests pin down.

## Why the condensed-output note exists

Agents did not merely retry — they *investigated*, running `which git`, `type git`,
`alias git`, `git config --get alias.log` to work out whether git was broken. A
one-line marker removes the ambiguity for ~95 bytes.

It is worded carefully. It does **not** claim nothing was lost: `yeet grep` caps at
`--max-results 200` (527 real matches → 200 shown) and `yeet ps` shows 85 of 662
lines. It does **not** tell the reader not to re-run — when the omitted detail is
what they need, re-running is correct. The note is counted inside the never-worse
comparison and only added when raw output is ≥400 bytes, so it can never push a
condensed result past the raw one and silently disable filtering.

## Offline vs live

`scripts/bench-offline.sh` reports **74% fewer tokens** on the same repo. That is not
wrong, but it answers a different question: it scores yeet against raw shell output
(`grep -rn` = 109,045 bytes) that an agent would rarely receive, because it would use
the native `Grep` tool instead (2,231 bytes in context). Quote the offline number as
"bytes per command", never as "cost of a task".

## Limits

- **n=3 per arm.** Cost ranged $0.19–0.23 for native and $0.16–0.23 for `yeet-sel`.
  The ranges overlap; only the 35% gap for the blocking set is comfortably outside
  the noise.
- **The shipped config was not run head-to-head against native.** Its −12.7% is
  cross-run. The −6.0% for `yeet-bash` is same-run and carries no such caveat.
- **This workload is yeet's weakest terrain** — search, read, edit and git are where
  Claude Code's native tools are already compact. The verbose commands yeet has no
  native competitor for (`npm install`, `tsc`, lint, test runners) were not
  exercised; `npm install --dry-run` was the only build-ish command and it was small.
- **Pooling across builds is invalid.** `yeet-sel` was measured under five different
  binaries while iterating; those medians must not be averaged together.
- `/usage` cannot measure any of this. `rate_limit_event.utilization` is 1%-granular,
  account-wide, and emitted only on threshold crossings — one session moves it by
  roughly one granule.

## Reproducing

```bash
bash scripts/bench-sim.sh --reps 3 --model sonnet \
  --arms "native yeet yeet-bash yeet-sel" --yeet-bin ./yeet
```

Pin the binary with `--yeet-bin`. The first run of this investigation measured an
installed binary that had diverged from the source tree and inflated yeet's cost
3.2x; `--yeet-bin` plus the version line in the report makes that visible.

For a verdict on your own work rather than one synthetic task, use
`scripts/yeet-ab.sh` — it records real sessions in both arms over days.
