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

## Reporting numbers honestly

- Quote the offline percentage with the case mix and target repo named — it is specific
  to both.
- Never quote a live result from a single rep, and never quote one while runs were
  excluded without saying how many.
- `yeet stats` shows the user's own cumulative savings from real use. That is the most
  persuasive number available and costs nothing to read.
