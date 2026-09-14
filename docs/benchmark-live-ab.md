# Live A/B benchmark: what yeet actually costs in a real session

Measured 2026-09-14 against `eslint/eslint` @ `24310e3a` (2,363 tracked files),
model `sonnet`, 3 reps per arm, rotating order, each run on a pristine copy of
the target at a byte-identical path. Harness: `scripts/bench-sim.sh`.

Billed input = `input + cache_creation + cache_read`, taken from the API's own
per-session usage. This is the number that matters: **it is dominated by the
context re-sent on every turn, not by the size of any one tool result.**

## Headline

| variant | billed (median) | turns | spread | vs native |
|---|---:|---:|---|---:|
| `native` — no yeet | 393,768 | 11 | 294,833–454,710 (54%) | ref |
| `yeet` shipped, stale binary | 1,796,505 | 34 | — | −296% |
| `yeet` shipped, current binary | 623,828 | 16 | — | −58.4% |
| `yeet-bash` — Bash rewrite only | 486,541 | 15 | — | −23.6% |
| `yeet-sel` — selective, before fixes | 447,916 | 13 | — | −13.8% |
| **`yeet-sel` — after the fixes in this PR** | **395,886** | 13 | 387,420–402,748 (3.9%) | **−0.5%** |

Parity with no-yeet, and materially more consistent than no-yeet.

## Why the shipped configuration lost

One extra turn costs about **32,000 billed tokens** (each turn re-sends the
accumulated context). Compressing every tool result in an entire run saved about
**1,900 tokens**. So a single wasted turn is worth ~17 runs' worth of byte
savings, and any change that adds a turn loses regardless of how well it
compresses.

The shipped configuration added turns in two ways:

1. **Five hard blocks** on `Read`/`Glob`/`Grep`/`Write`/`Edit`. The blocked
   tools remain in the tool schema, so their definitions are still paid for on
   every request, and a model that reaches for one pays a wasted turn on top.
2. **Wrong or lossy output**, which made agents re-run commands. Per-arm counts
   of `command git` / `which git` / `type git` in the transcripts:

   | arm | bypass + diagnose calls | turns |
   |---|---:|---:|
   | `yeet-sel` before fixes | 5 | 13–14 |
   | **`yeet-sel` after fixes** | **0** | **12–13** |

The run with zero bypasses tied the no-yeet baseline exactly. That is the whole
effect.

## The bugs that caused the re-runs

**`git log --oneline` reported `no commits` for a file that had two.**
`renderGitLog` parses yeet's own `--pretty=format:%h|%an|%ar|%s`. A caller's
`--oneline` overrides that layout, every line failed the 4-field split, and the
renderer returned `no commits`. The never-worse guard then *preferred* that
answer because `no commits` (11 bytes) is shorter than the real log (108 bytes).
A wrong answer won on size.

**`git diff` returned a stat line with no hunks.** It defaulted to `--numstat`
on the theory that "the per-file summary is what an agent needs to decide where
to look". The transcripts contradict that: agents asked for a diff, got a
summary, and re-ran `command git diff` to see the change.

Both are the same underlying problem: **the "run both, keep the smaller"
comparison optimises for bytes, and byte-minimisation systematically selects for
information loss.** `printBetterN` sees two strings; it cannot know that
`no commits` is a lie. Fidelity has to be guaranteed by each renderer *before*
the size comparison is allowed to choose, which is what the per-renderer
regression tests in this PR pin down.

## Why an explanatory note helps

Agents did not merely retry — they *investigated*, running `which git`,
`type git`, `alias git`, `git config --get alias.log` to work out whether git
was broken. A one-line `<note-for-llms>` marker removes that ambiguity for ~60
bytes against a turn that costs ~32,000 tokens. It is counted inside the
never-worse comparison, so it can never push a condensed result past the raw one
and silently disable filtering.

## Offline vs live

`scripts/bench-offline.sh` reports **74% fewer tokens** on the same repo. That is
not wrong, but it answers a different question: it scores yeet against raw shell
output (`grep -rn` = 109,045 bytes) that an agent would rarely receive, because
it would use the native `Grep` tool instead (2,231 bytes in context). Quote the
offline number as "bytes per command", never as "cost of a task".

## Limits of this measurement

- **n=3 per arm.** Native's own median moved 294,833 → 454,710 across reps, a
  54% spread. Differences smaller than that are not resolvable here.
- **Parity, not a win.** `yeet-sel` matches no-yeet; it does not beat it.
- **This workload is yeet's worst terrain** — search, read, edit and git are
  exactly where Claude Code's native tools are already compact. The verbose
  commands yeet has no native competitor for (`npm install`, `tsc`, `lint`,
  test runners) were not exercised; `npm install --dry-run` was the only
  build-ish command and it was small.
- **Three changes are bundled** in the final arm (git log correctness, git diff
  content, the note). The git-diff fix *alone* measured worse (598,291), so the
  gain is carried by the other two; attribution would need separate arms.
- `/usage` cannot measure this. `rate_limit_event.utilization` is 1%-granular,
  account-wide, and emitted only on threshold crossings — one session moves it
  by roughly one granule. Exact per-session token counts are the only workable
  instrument.

## Reproducing

```bash
bash scripts/bench-sim.sh --reps 3 --model sonnet \
  --arms "native yeet yeet-bash yeet-sel" --yeet-bin ./yeet
```

Pin the binary with `--yeet-bin`. The first run of this investigation measured an
installed binary that had diverged from the source tree and inflated yeet's cost
3.2×; `--yeet-bin` plus the version line in the report makes that visible.
