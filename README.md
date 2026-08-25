# 🚀 yeet

> **Token-optimized CLI wrapper for AI coding agents**
> Stop burning context window on noisy command output. Yeet filters it down to what actually matters.

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat)](LICENSE)

---

## 🧒 ELI5 — Explain Like I'm 5

Imagine you ask your friend *"what's in the fridge?"*

😩 **Without yeet**, they read you every single label on every single item, expiry dates, nutrition facts, barcode numbers — the whole thing. By the time they're done you forgot what you even asked.

😎 **With yeet**, they just say: *"milk, eggs, leftover pizza."* Done.

That's it. **Yeet makes command output short and sweet so your AI doesn't waste its brain reading the noise.** Claude, Copilot, and other AI coding agents only have so much memory — yeet makes sure none of it gets wasted on junk.

---

## 🤔 Why?

When AI agents like Claude Code run shell commands, they read every single character of the output — and every character costs tokens (= 💸 + 🧠 context).

```
$ ls -laR
drwxr-xr-x  14 user  staff   448 Apr  5 12:34 .
drwxr-xr-x   8 user  staff   256 Apr  5 12:30 ..
-rw-r--r--   1 user  staff  1234 Apr  5 12:34 main.go
... (200 more lines of noise)
```

vs.

```
$ yeet ls
src/
├── main.go
├── utils.go
└── config.go
```

**60–90% fewer tokens. Same information. Every. Single. Command.**

---

## ✨ Features

| Command | What it does | Savings |
|---|---|---|
| `yeet ls` | 🌳 Clean directory tree (no permissions/dates noise) | ~80% |
| `yeet read` | 📄 File content with line numbers, no bloat | ~30% |
| `yeet smart` | 🧠 Just function/type signatures — skip the body | ~70% |
| `yeet grep` | 🔍 Deduplicated matches, grouped by file | ~60% |
| `yeet glob` | 📂 File paths only, no metadata | ~70% |
| `yeet find` | 🔎 Pattern search, clean output | ~70% |
| `yeet diff` | 🔀 Compact diff summary | ~50% |
| `yeet edit` | ✏️ Surgical text replacement, tiny confirmation | ~95% |
| `yeet write` | 💾 Write files, get a one-liner back | ~95% |
| `yeet env` | 🔐 Env vars with secrets masked | ~60% |
| `yeet ps` | 🧬 Grouped by program, CPU/memory leaders | ~94% |
| `yeet du` | 💽 Largest first, long tail collapsed | ~98% |
| `yeet kubectl` | ☸️ Health summary; only not-ready rows in full | ~87–99% |
| `yeet docker` | 🐳 Grouped by status, wide columns dropped | ~60–86% |
| `yeet vitest` | 🧪 Failures only, stack trimmed | ~99% |
| `yeet tsc` | 🔷 Errors grouped by file, capped per file | ~85% |
| `yeet lint` | 🧹 Violations grouped by rule | ~70% |
| `yeet npm` / `pnpm` / `yarn` | 📦 What changed, warnings counted | ~99% |
| `yeet stats` | 📊 Token savings dashboard | — |

Savings are measured, not estimated — see [Real Numbers](#-real-numbers) for the
command that reproduces them.

---

## 📦 Install

```bash
curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/install.sh | bash
```

It asks which editor integration you want (Claude Code is the default) and whether to
enable auto-allow. To skip the questions:

```bash
curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/install.sh | bash -s -- --yes
```

The installer downloads the binary to `/usr/local/bin` (falling back to `~/.local/bin`),
installs `jq` if it is missing, sets up the Claude Code hooks in `~/.claude/`, and
verifies every piece before it exits. Then restart Claude Code and check:

```bash
yeet version
yeet stats
```

**Useful flags** — `--dry-run` prints every change without making it, `--claude` /
`--copilot` / `--both` / `--binary-only` pick the integration, `--no-auto-allow` keeps
Claude Code's permission prompts, `--dir` changes where the binary goes, `--version`
pins a release, `--from-source` builds with Go. `install.sh --help` lists them all.

Re-running the installer is safe: it strips its previous hook entries before writing
new ones, so hooks never stack up. It records everything it touched in
`~/.local/share/yeet/install-manifest.json`, and if any step fails it rolls back rather
than leaving you half-installed.

### Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/uninstall.sh | bash
```

It lists what it found, asks before touching anything, then removes the binary
(searching every location it could be in, not just `/usr/local/bin`), the hooks, the
awareness file, the `CLAUDE.md` reference, and the analytics database. Settings you
had before yeet — including your original `autoCompactThreshold` — are restored, and
unrelated hooks and permissions are left alone. Installs from older versions that left
no manifest are found and removed too.

It verifies the result and **exits non-zero if anything survives**, so a clean exit is
a real guarantee.

```bash
bash uninstall.sh --dry-run      # show what would go, change nothing
bash uninstall.sh --keep-data    # keep your yeet stats history
bash uninstall.sh --purge        # also clean project files (.github/hooks, .vscode)
```

Restart Claude Code afterwards — a running session keeps the old hooks in memory.

### Build from source

```bash
# Prerequisites: Go 1.21+, a C compiler (for SQLite)
xcode-select --install   # macOS only, one-time

git clone https://github.com/hdck007/yeet.git
cd yeet
make install
```

---

## 📊 How much does it save?

Two benchmarks ship with the repo.

**Deterministic, free, reproducible** — runs each native command, asks `yeet rewrite`
what the hook would turn it into, and compares output sizes:

```bash
bash scripts/bench-offline.sh                     # against this repo
bash scripts/bench-offline.sh --target ~/my-app   # against your project
```

It reports per-case numbers, the totals, and — importantly — any rewrite that produced
a *failing* command, which are excluded from the totals rather than counted as savings.

**End-to-end on a real task** — runs the same task in Claude Code with and without
yeet and compares the tokens the API actually billed:

```bash
bash scripts/bench-live.sh --reps 3
```

Your `~/.claude` is never touched: each arm gets its own throwaway `CLAUDE_CONFIG_DIR`,
and the yeet arm is built by running `install.sh` against it. Runs alternate between
arms so cache warmth is shared, and any run that used yeet in the native arm, skipped
yeet in the yeet arm, or failed to finish the task is reported and excluded. Costs real
money — it asks first.

---

## 🤖 Claude Code Setup

Two layers work together: **blockers** prevent Claude from using native tools directly, **proxy hook** silently rewrites raw Bash commands to `yeet` equivalents.

### Option A — Project-level (this repo only)

```bash
bash scripts/install.sh --claude --plugin
```

### Option B — Global proxy hook (all projects)

```bash
# Blockers for this project
bash scripts/install.sh --claude

# Proxy hook for every Claude Code session globally
bash scripts/install.sh --plugin --global
```

### What each piece does

| Component | Flag | What it does |
|-----------|------|--------------|
| PreToolUse blockers | `--claude` | Blocks native Read/Glob/Grep/Write/Edit tools |
| `yeet-proxy.sh` | `--plugin` | Rewrites `cat`/`grep` Bash calls to `yeet` before execution |

After setup:

```
Native Read tool      →  BLOCKED
Native Grep tool      →  BLOCKED
Native Glob tool      →  BLOCKED
Native Write tool     →  BLOCKED
Native Edit tool      →  BLOCKED
Bash: cat file.go     →  yeet read file.go    (rewritten silently)
Bash: grep foo .      →  yeet grep foo .      (rewritten silently)
```

`jq` is required for the proxy hook and auto-installed if missing.

### Compaction limit

The installer sets `autoCompactThreshold` to **100,000 tokens** in `~/.claude/settings.json`. This tells Claude Code to compact the conversation earlier, keeping context lean across long sessions. You can adjust it manually:

```json
{ "autoCompactThreshold": 100000 }
```

---

## 🐙 GitHub Copilot (VS Code) Setup

```bash
bash scripts/install.sh --copilot
```

**What this does:**
- 📝 Creates `.github/copilot-instructions.md` → loads at every Copilot session
- 🪝 Creates `.github/hooks/yeet-rewrite.sh` → intercepts raw commands in agent mode
- ⚙️ Creates `.vscode/settings.json` → enables Copilot agent tool use

---

## 🚀 Advanced Setup (from source)

For contributors or users who want everything — binary build, Claude Code blockers, proxy hook, and Copilot:

```bash
git clone https://github.com/hdck007/yeet.git
cd yeet
bash scripts/install.sh
```

Equivalent to `--build --claude --plugin --copilot`. Individual flags available:

```bash
bash scripts/install.sh --build             # binary only
bash scripts/install.sh --claude            # Claude Code blockers only
bash scripts/install.sh --plugin            # proxy hook (project-level)
bash scripts/install.sh --plugin --global   # proxy hook (all projects)
bash scripts/install.sh --copilot           # Copilot only
```

---

## 📖 Usage

### Replacing built-in tools

```bash
# Reading files
yeet read internal/cli/root.go                       # full file with line numbers
yeet read internal/cli/root.go -l aggressive         # signatures only
yeet smart internal/cli/root.go                      # quick summary

# Searching
yeet grep "func Run" .                               # grep across project
yeet glob "**/*.go" .                                # find files by pattern
yeet find "*.go" internal/                           # find by name

# Editing
yeet edit main.go --old "foo" --new "bar"            # replace first
yeet edit main.go --old "foo" --new "bar" --all      # replace all

# Multi-line edit (heredoc)
yeet edit main.go << 'EDIT'
old content
|||
new content
EDIT

# Writing files (base64-encode content — no shell escaping issues)
yeet write path/to/file.go --b64 $(printf '%s' 'package main
func main() {}' | base64)

# Other
yeet ls .                                            # directory tree
yeet diff file1.go file2.go                          # compact diff
yeet env                                             # filtered env vars
```

### Tests, builds, and package managers

```bash
yeet vitest run                                      # failures only
yeet tsc --noEmit                                    # errors grouped by file
yeet lint src                                        # violations grouped by rule
yeet npm install                                     # what changed + warning count
yeet pnpm install --frozen-lockfile
```

### Processes, disks, clusters, containers

```bash
yeet ps aux                                          # totals, leaders, then grouped
yeet du -sh *                                        # largest first
yeet kubectl get pods -A                             # health summary; not-ready in full
yeet kubectl describe pod api-0                      # status + events, annotations dropped
yeet docker ps -a                                    # grouped by status
```

`kubectl` and `docker` follow the same read-only boundary as `git` and `gh`:
`apply`, `delete`, `exec`, `run`, `rm`, `compose up` and every other mutation
reaches the real binary untouched.

### Chained commands

The proxy hook splits a command on `;`, `&&`, `||`, `|` and newlines and
rewrites each segment:

```bash
cat pkg.json && ls src        # → yeet read pkg.json && yeet ls src
grep -rn foo src | head -20   # → yeet grep foo src | head -20
cat a.ts; cat b.ts            # → yeet read a.ts; yeet read b.ts
```

Two shapes are deliberately left alone, because rewriting them would change the
answer rather than shorten it:

- **A command reading the pipe** — `git log | grep fix`. `yeet grep` searches the
  working tree, not stdin.
- **A consumer that parses the exact bytes** — `cat data.json | jq .name`,
  `ls | wc -l`. Piping into `head`, `tail`, `less`, `more` or `cat` is safe and
  is rewritten.

A chain whose segments are not all read-only still gets rewritten, but the
permission prompt is kept: `cat a.ts && rm -rf build` becomes
`yeet read a.ts && rm -rf build` and is shown to you rather than auto-allowed.

### Analytics

```bash
yeet stats            # 📊 Token savings dashboard
yeet stats --json     # Machine-readable output
yeet clear            # Reset analytics DB
yeet update           # Rebuild & reinstall from source
yeet version          # Print version
```

### Configuration

**Auto-allow** — when enabled, Claude Code never prompts for permission before running a `yeet` command (including heredoc pipe forms like `cat <<'X' | yeet edit`):

```bash
yeet auto-allow         # show current state (default: false)
yeet auto-allow true    # enable — no more permission prompts for yeet
yeet auto-allow false   # disable
```

The install script asks about this during setup. Setting is stored in `~/.local/share/yeet/auto-allow`.

**Read threshold** — number of lines at which `yeet read` (with no filter flags) warns and stops instead of dumping the whole file. Pushing the agent toward `grep` or `--lines` first:

```bash
yeet threshold          # show current effective threshold (default: 150)
yeet threshold 200      # persist a new value (minimum: 100)
yeet threshold reset    # remove persisted value, fall back to default
```

Override order: `--threshold` flag > `YEET_BIG_FILE_THRESHOLD` env var > persisted config > 150.

---

## 🏗️ How it works

```
AI Agent
   │
   ▼
yeet <cmd>              ← thin wrapper, always exits with original exit code
   │
   ├─ runs underlying tool (cat, ls, grep, find, diff...)
   ├─ filters & compresses the output
   ├─ records raw vs. rendered char count in SQLite (~/.local/share/yeet/analytics.db)
   └─ prints compact result
```

Every invocation records:
- 📥 Raw character count (what you'd get without yeet)
- 📤 Rendered character count (what the agent actually sees)
- 💰 Tokens saved (estimated at ~4 chars/token)

---

## 🔢 Real Numbers

Run `bash demo.sh` from the repo root to see live savings on this codebase:

| Command | Raw | Yeet | Saved |
|---|---|---|---|
| `ls` | ~8,000 chars | ~400 chars | **95%** |
| `grep` | ~12,000 chars | ~1,800 chars | **85%** |
| `read` | ~3,200 chars | ~2,800 chars | **13%** |
| `read -l agg` | ~3,200 chars | ~200 chars | **94%** |
| `glob` | ~600 chars | ~480 chars | **20%** |
| `diff` | ~2,400 chars | ~1,600 chars | **33%** |

### The noisy verbs

`ps aux`, `kubectl get pods` and `docker ps` print something different on every
machine, so a number measured against a live cluster would not be reproducible.
`scripts/bench-verbs.sh` generates fixed fixtures instead — same bytes every
run, on any machine, with no cluster, no daemon and no `node_modules`:

```bash
bash scripts/bench-verbs.sh
```

| Command | Raw bytes | Yeet bytes | Tokens saved | Cut |
|---|---|---|---|---|
| `ps aux` | 96,901 | 3,664 | 23,310 | **96.2%** |
| `du -h .` | 106,730 | 2,277 | 26,113 | **97.9%** |
| `kubectl get pods -A` | 19,614 | 461 | 4,788 | **97.6%** |
| `kubectl describe pod` | 57,260 | 289 | 14,242 | **99.5%** |
| `kubectl logs deploy/api` | 82,899 | 11,006 | 17,973 | **86.7%** |
| `docker ps -a` | 11,639 | 4,619 | 1,755 | **60.3%** |
| `docker images` | 14,591 | 1,998 | 3,148 | **86.3%** |
| `docker compose logs` | 39,985 | 6,862 | 8,281 | **82.8%** |
| `vitest run` | 37,980 | 419 | 9,390 | **98.9%** |
| `tsc --noEmit` | 40,846 | 12,734 | 7,028 | **68.8%** |
| `npm install` | 67,315 | 123 | 16,798 | **99.8%** |
| `pnpm install` | 67,315 | 125 | 16,797 | **99.8%** |
| **Total** | **643,075** | **44,577** | **149,624** | **93.1%** |

### On real output

Fixtures keep the numbers reproducible; these were taken from actual tools on a
developer machine and one live staging cluster, and are the numbers that
matter. `kubectl get pods -A` on a 4,000-pod cluster is a single command that
costs ~159,000 tokens raw:

| Real command | Raw bytes | Yeet bytes | Tokens saved | Cut |
|---|---|---|---|---|
| `kubectl get pods -A` (4,011 pods) | 635,311 | 3,649 | 157,915 | **99.4%** |
| `du -h /opt/homebrew/lib` | 241,512 | 2,170 | 59,835 | **99.1%** |
| `ps aux` (478 processes) | 159,692 | 4,471 | 38,805 | **97.2%** |
| `ps -ef` | 141,120 | 3,322 | 34,449 | **97.6%** |
| `npm install` (166 packages) | 1,286 | 70 | 304 | **94.6%** |

The `kubectl` line is not just shorter, it is a better answer: instead of 4,012
rows it opens with

```
4011 resources: 3547 Running, 169 Completed, 163 ImagePullBackOff, 37 Error,
31 CrashLoopBackOff, 31 ContainerStatusUnknown, 20 ContainerCreating, ...
```

and then lists the not-ready rows in full, saying how many it capped.

That `npm install` is small in absolute terms only because the machine it ran on
wraps npm in a tool that already suppresses most of its output; against a plain
npm the raw side is far larger.

The same script reports how many commands the hook actually rewrites, which is
the other half of the number — a renderer nothing routes to saves nothing:

| | Rewritten | Auto-allowed | Prompted | Coverage |
|---|---|---|---|---|
| v0.1.8 | 8 / 50 | 8 | 0 | 16.0% |
| this build | 50 / 50 | 33 | 17 | **100.0%** |

...over `scripts/bench-corpus.txt`, 50 commands of the kind an agent issues,
with chained forms over-represented because that is what a real session looks
like. `scripts/bench-corpus-negative.txt` holds the other side: 34 commands
that must come back untouched — every state change, plus every shape where a
rewrite would answer the question wrongly. The script fails if any of them
leaks.

---

## 🛠️ Development

```bash
make build    # compile
make install  # build + install to ~/go/bin
make test     # run tests
```

**Project layout:**

```
internal/
├── cli/        # one file per yeet subcommand
├── filter/     # output compression logic
├── analytics/  # SQLite recording
├── token/      # char → token estimator
├── exec/       # subprocess runner
└── ignore/     # .gitignore-aware path filtering
```

---

## 💡 Inspiration

Yeet is inspired by [rtk](https://github.com/rtk-ai/rtk) — a Rust-based token-killer CLI proxy. RTK pioneered the idea of wrapping dev commands to compress LLM context. Yeet takes that concept and applies it to file operations and AI agent tooling specifically.

---

## 📄 License

MIT © [hdck007](https://github.com/hdck007)
