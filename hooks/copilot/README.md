# GitHub Copilot Hooks

> Part of [`hooks/`](../README.md) — see also [`hooks/claude/`](../claude/) for Claude Code integration

## What's here

| File | Purpose |
|---|---|
| `yeet-awareness.md` | Awareness file installed as `.github/copilot-instructions.md` in target projects |
| `test-yeet-rewrite.sh` | Test suite for the PreToolUse rewrite hook |

The hook script and config live in [`.github/hooks/`](../../.github/hooks/).

## How it works

Two layers of integration:

1. **Prompt layer** — `yeet-awareness.md` is copied to `.github/copilot-instructions.md` in your project.
   VS Code loads this at session start and instructs Copilot to prefer yeet commands.

2. **Hook layer** — `yeet-rewrite.sh` runs as a `PreToolUse` hook in VS Code agent mode.
   It intercepts raw bash calls at runtime and rewrites them transparently.

## Installation

```bash
# Full install (build + Claude + Copilot hooks)
bash scripts/install.sh

# Copilot only (no binary build required)
bash scripts/install.sh --copilot

# Into another project
bash scripts/install.sh --copilot --target /path/to/your/project
```

Manual copy:

```bash
PROJECT=/path/to/your/project
mkdir -p "$PROJECT/.github/hooks"
cp hooks/copilot/yeet-awareness.md "$PROJECT/.github/copilot-instructions.md"
cp .github/hooks/yeet-rewrite.sh   "$PROJECT/.github/hooks/yeet-rewrite.sh"
cp .github/hooks/yeet-rewrite.json "$PROJECT/.github/hooks/yeet-rewrite.json"
chmod +x "$PROJECT/.github/hooks/yeet-rewrite.sh"
```

## What gets rewritten

| Raw command | Rewritten to |
|---|---|
| `cat file.go` | `yeet read file.go` |
| `ls` / `ls -la path/` | `yeet ls [path]` |
| `grep pattern src/` | `yeet grep pattern src/` |
| `find . -name "*.go"` | `yeet glob "**/*.go" .` |
| `diff a b` | `yeet diff a b` |

Already-rewritten `yeet ...` commands pass through untouched.

## Testing

```bash
bash hooks/copilot/test-yeet-rewrite.sh
```

## Requirements

- `jq` — used by the hook script to parse/emit JSON
- `yeet` — must be on PATH in the VS Code terminal
