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
| `yeet stats` | 📊 Token savings dashboard | — |

---

## 📦 Install

### Quick install script
```
curl -sSL https://raw.githubusercontent.com/hdck007/yeet/main/install.sh | bash
```

### Uninstall
```bash
curl -sSL https://raw.githubusercontent.com/hdck007/yeet/main/uninstall.sh | bash
```

The uninstaller checks what's installed, shows you exactly what will be removed, and asks for confirmation before touching anything.

### Manual
**Prerequisites:** Go 1.21+, a C compiler (for SQLite)

```bash
curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/install.sh | bash
```

That's it. The installer will:
- Download the pre-built binary to `/usr/local/bin/yeet`
- Install `jq` if missing
- Set up the Claude Code proxy hook globally (`~/.claude/`)

**Verify:**
```bash
yeet version
yeet stats
```

### Build from source

```bash
# Prerequisites: Go 1.21+, a C compiler (for SQLite)
xcode-select --install   # macOS only, one-time

git clone https://github.com/hdck007/yeet.git
cd yeet
make install
```

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
| PreToolUse blockers | `--claude` | Blocks native Read/Glob/Grep tools |
| `yeet-proxy.sh` | `--plugin` | Rewrites `cat`/`grep` Bash calls to `yeet` before execution |

After setup:

```
Native Read tool      →  BLOCKED
Native Grep tool      →  BLOCKED
Native Glob tool      →  BLOCKED
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
