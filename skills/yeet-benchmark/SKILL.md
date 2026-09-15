---
name: yeet-benchmark
description: Measure how many tokens yeet actually saves, either deterministically with no API cost or end-to-end by running the same task in real Claude Code sessions with and without yeet. Use when the user asks "how much does yeet save", "benchmark yeet", "prove yeet works", "is yeet worth it", "compare with and without yeet", "measure the token savings", or wants numbers for a README or a write-up.
---

# Benchmarking yeet

Two benchmarks, and they answer different questions. Pick deliberately.

| | `scripts/bench-offline.sh` | `scripts/bench-live.sh` |
|---|---|---|
| Measures | bytes each command puts in context | tokens a whole task actually bills |
| Cost | none | real money (2 × reps sessions) |
| Reproducible | exactly, every run | no — model behavior varies |
| Answers | "how much smaller is yeet's output?" | "how much cheaper was the task?" |

**Default to the offline one.** It is free, instant, deterministic, and enough for a
README claim. Reach for the live one only when the user specifically wants end-to-end
task cost, and tell them it costs money before running it.

## Offline — deterministic, no API

```bash
bash scripts/bench-offline.sh                          # against the yeet repo
bash scripts/bench-offline.sh --target ~/my-app        # against a real project
bash scripts/bench-offline.sh --md report.md --json report.json
```

For each case it runs the native command, asks **`yeet rewrite`** what the PreToolUse
hook would turn it into, runs that, and compares output sizes. Using `yeet rewrite`
means it measures the real production path rather than a hand-picked pairing.

Read the output carefully — three numbers matter:

- **`% fewer tokens`** — the headline, over cases that worked.
- **`N broken`** — rewrites that produced a *failing* command. These are excluded from
  the totals and are **worse than no saving**: the agent burns a turn on an error.
  If this is non-zero, say so; it is a bug in `internal/cli/rewrite.go`, not a
  benchmark artifact.
- **cases larger with yeet** — honest losses. Some commands genuinely get bigger.

Run it against a large real repo as well as this one. Savings scale with file and
result sizes, so measuring only against yeet's own small repo understates the case.

## Live — real Claude Code sessions

```bash
bash scripts/bench-live.sh                       # 2 reps per arm
bash scripts/bench-live.sh --reps 3 --target ~/big-app
bash scripts/bench-live.sh --task-file ./task.txt
```

Design points to explain if the user questions the result:

- **Your `~/.claude` is never touched.** Each arm gets a throwaway
  `CLAUDE_CONFIG_DIR`, and `--setting-sources user` stops project or local settings
  leaking in. The yeet arm is built by running `install.sh` against that directory, so
  it tests the shipped setup rather than an imitation.
- **Runs alternate** (yeet, native, native, yeet …) so prompt-cache warmth and API
  weather land on both arms evenly.
- **Tokens come from the API's own usage numbers** — input + cache-creation +
  cache-read, plus the cost the API reported.
- **Every run is validated and excluded if it fails.** Arm purity (no `yeet` command in
  the native arm; at least one in the yeet arm), task completion (the answer must match
  the completion pattern), and non-zero usage. This is the important part: a run that
  quietly did less work is cheaper without being better, and would otherwise look like
  a win.
- **Medians are the headline**, means reported alongside, so one weird run cannot carry
  the result.

Use `--reps 3` or more before quoting a number anywhere. With `--reps 2` a single
outlier moves the median a lot.

Reports land in `scripts/bench-results/live-<stamp>/` — `report.md`, `report.json`, and
the raw `*.jsonl` streams for auditing what each arm actually did.

### Custom tasks

`--task-file` takes any prompt. Set `YEET_BENCH_COMPLETION_RE` to a regex that only a
genuinely finished answer matches, otherwise the completion check passes everything:

```bash
YEET_BENCH_COMPLETION_RE='TOTAL:[[:space:]]*[0-9]+' \
  bash scripts/bench-live.sh --task-file ./task.txt
```

Good benchmark tasks are exploration-heavy (many reads, greps, and listings — that is
where yeet acts) and end in a checkable fact. A task that is mostly reasoning with few
tool calls will show little difference, which is a true result about that task, not
evidence against yeet.

## Live — a week of your own work (`scripts/yeet-ab.sh`)

The most honest instrument, and the one to reach for when someone asks whether yeet
is worth it. A single synthetic task cannot settle it: yeet's edge measured **~0.8%
per turn** on search/read/edit work, while session cost swings over 50% with the
task. So sample real work instead.

```bash
eval "$(bash scripts/yeet-ab.sh shell-init)"   # gives `claude --yeet`
claude --yeet        # recorded in the yeet arm
claude               # recorded without yeet
bash scripts/yeet-ab.sh report --since 7d
bash scripts/yeet-ab.sh export --csv
```

Each session lands in a datalake with totals, per-turn usage, every tool call, and
diagnostic signals: failures, repeated identical invocations, bypasses
(`command git`, `which git`), truncation admissions, and outlier result sizes.
Report **billed per turn**, not per session — session totals mostly track how long
the task ran. Do not read the comparison below ~20 sessions per arm.

## What the live A/B actually found

Quote these rather than re-deriving them (full method in `docs/benchmark-live-ab.md`):

- **Report cost, never a raw token sum.** `input + cache_creation + cache_read`
  added at 1:1 is dominated by cache reads, which bill at ~0.1x, while cache
  creation bills at ~1.25x, so the sum mostly measures the cheapest class and can
  point the opposite way to the bill. Use `total_cost_usd` from the API.
- **Do not build an argument on cache_creation.** It is the expensive class, so it
  is tempting, but it swings 3x between identical runs (8,554 to 27,563). An
  apparent "yeet writes 38% more cache" finding turned out to be a warmup artifact:
  a plain no-yeet warmup session produced 12,704, the same value that had been read
  as a yeet defect.
- **Turns dominate everything else.** Each extra turn re-sends the whole
  accumulated context, which costs far more than condensing tool output recovers.
- **Tool output is only ~4% of billed context.** The rest is the system prompt, tool
  schemas and message history. Compressing a 4% slice by 20% is worth ~0.8%.
- **The blocking hook set cost 35% MORE than no yeet at all** (real API cost) and has
  been removed. It ran 16 turns where no-yeet ran 11 -- a countable mechanism, which
  is why that number survives the noise below.
- **The current set shows no measurable difference from no yeet** on a
  search/read/edit/git workload. Do not quote a percentage for it.
- **Discard warmup sessions.** The first session of a fresh benchmark reports far more
  cache_creation than later ones (33,360 vs ~9,000), and whichever arm draws the first
  slot is charged for it. `bench-sim.sh --warmup N` runs and throws away sessions first.
- **cache_creation is noisy even warm** -- 8,554 to 27,563 across identical runs. A
  single outlier there moved one arm from "cheaper than native" to "+11.7%".
- Offline says 74%, live says roughly break-even. Both are correct — offline scores
  against raw `grep -rn` output that an agent would rarely receive, because it would
  use the native `Grep` tool instead.

## Do not use `/usage`

`rate_limit_event.utilization` is what `/usage` displays. It is 1%-granular,
account-wide, and emitted only on threshold crossings — one session moves it by
roughly one granule, and any other session on the account contaminates it. Exact
per-session token counts from the transcript are the only workable instrument.

## Reporting numbers honestly

- Quote the offline percentage with the case mix and target repo named — it is specific
  to both.
- Never quote a live result from a single rep, and never quote one while runs were
  excluded without saying how many.
- `yeet stats` shows the user's own cumulative savings from real use. Read it as
  bytes-per-command, not as task cost — and note that `find`/`glob` rows were logged
  from a hardcoded 2x estimate before they were marked `BaselineSynthetic`, so older
  histories overstate those two.
- Never quote a live result from a single rep, and never quote one while runs were
  excluded without saying how many.
