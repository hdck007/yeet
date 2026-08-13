---
name: yeet-install
description: Install or upgrade yeet — the token-optimized CLI wrapper — and wire it into Claude Code (and optionally GitHub Copilot). Use when the user says "install yeet", "set up yeet", "upgrade yeet", "get yeet working with Claude Code", "reinstall yeet", "yeet isn't working", or asks to add the yeet hooks to a machine. Also use after a failed or partial install to repair the setup. For removal, use yeet-uninstall instead.
---

# Installing yeet

`install.sh` at the repo root is the single supported installer. It is idempotent,
records everything it does in a manifest, verifies itself, and rolls back on failure.
Never hand-edit `~/.claude/settings.json` to add yeet hooks — the installer owns that.

## The one-liner

```bash
curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/install.sh | bash
```

Interactive: it asks which editor integration to set up (Claude Code is the default)
and whether to enable auto-allow.

Non-interactive — use this whenever you are running it on the user's behalf:

```bash
curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/install.sh | bash -s -- --yes
```

`--yes` means: install the binary, set up **Claude Code** integration, enable
auto-allow. It does not set up Copilot.

From a local checkout (works offline — assets come from the working tree):

```bash
bash install.sh --yes
```

## Flags worth knowing

| Flag | Use it when |
|---|---|
| `--yes` / `-y` | Non-interactive. Always pass this when installing for the user. |
| `--claude` | Claude Code only (the `--yes` default). |
| `--copilot` | GitHub Copilot only. Writes into `$PWD/.github/hooks` and `$PWD/.vscode`. |
| `--both` | Both integrations. |
| `--binary-only` | Just the `yeet` binary, no hooks. |
| `--no-auto-allow` | Keep Claude Code's permission prompts for yeet commands. |
| `--dir <DIR>` | Install the binary somewhere other than `/usr/local/bin`. |
| `--version <TAG>` | Pin a release instead of taking the latest. |
| `--from-source` | Build with Go from a checkout (needs Go + a C compiler for CGO/sqlite). |
| `--dry-run` | Print every change without making it. **Run this first if the user is nervous.** |

## What it installs

- `/usr/local/bin/yeet` — falls back to `~/.local/bin` when that is not writable and
  sudo is unavailable, and warns if that directory is not on PATH.
- `~/.claude/hooks/yeet-proxy.sh` — the PreToolUse hook that rewrites bash commands.
- `~/.claude/settings.json` — six hook entries, each tagged `"_yeet": true`, prepended
  so the blockers win. Also sets `autoCompactThreshold` to 100000 and, with auto-allow,
  adds `Bash(yeet:*)` to `permissions.allow`.
- `~/.claude/yeet-awareness.md` plus `@yeet-awareness.md` as the **first line** of
  `~/.claude/CLAUDE.md`, so it outranks other instructions.
- `~/.local/share/yeet/install-manifest.json` — the record uninstall reads.

For Copilot it merges a delimited block into `~/.copilot/copilot-instructions.md`
rather than overwriting the file, so the user's own content survives.

## After installing

The installer verifies itself and exits non-zero if any check fails, so a clean exit
means it worked. Two things still need saying to the user:

1. **Restart Claude Code** — hooks and `CLAUDE.md` load at session start.
2. Confirm with `yeet version` and `yeet stats`.

## Troubleshooting

**"`yeet` still runs an old build"** — another copy is earlier on PATH. The installer
detects this and offers to remove the duplicates (it only ever touches a binary that
identifies itself as yeet). If it was declined, re-run with `--yes`.

**"The hooks aren't firing"** — check in this order:
```bash
jq '[.hooks.PreToolUse[] | select(._yeet == true)] | length' ~/.claude/settings.json  # expect 6
head -1 ~/.claude/CLAUDE.md                                                          # expect @yeet-awareness.md
printf '%s' '{"tool_name":"Bash","tool_input":{"command":"true"}}' | bash ~/.claude/hooks/yeet-proxy.sh
```
The hook needs both `jq` and `yeet` on PATH; it warns and passes commands through
untouched if either is missing.

**"settings.json is not valid JSON"** — the installer refuses to touch a corrupt
settings file and changes nothing. Fix or move the file, then re-run.

**Install failed halfway** — it already rolled itself back. Nothing is left
half-applied; fix the reported cause and re-run.

## Verifying an install without trusting the output

```bash
bash scripts/test-install.sh
```

Runs the full install/uninstall suite in throwaway sandboxes — never touches the
real machine. Use it after changing either script.
