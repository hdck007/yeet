---
name: yeet-uninstall
description: Completely remove yeet — binary, Claude Code hooks, awareness file, CLAUDE.md reference, Copilot block, project files, and analytics — including installs from older versions that left no manifest. Use when the user says "uninstall yeet", "remove yeet", "get rid of yeet", "yeet is breaking my setup", "how do I undo the yeet install", "clean up yeet leftovers", or when Claude Code's Read/Grep/Edit tools are blocked and the user wants them back. For installing, use yeet-install instead.
---

# Uninstalling yeet

`uninstall.sh` at the repo root removes everything. It works two ways at once and
always tries both:

1. **Manifest reversal** — reads `~/.local/share/yeet/install-manifest.json` and undoes
   exactly what the installer did, restoring values it overwrote (notably the user's
   previous `autoCompactThreshold`).
2. **Legacy sweep** — finds anything an older installer left behind: hook entries with
   no `_yeet` marker, hook scripts in old locations, `~/.yeet`, stray binaries.

It exits **non-zero if anything yeet-related survives**, so a zero exit is a real
guarantee rather than a claim.

## Run it

```bash
curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/uninstall.sh | bash
```

Non-interactive:

```bash
curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/uninstall.sh | bash -s -- --yes
```

Show what would go without touching anything — **do this first when the user is
unsure**, it needs no confirmation and changes nothing:

```bash
bash uninstall.sh --dry-run
```

## Flags

| Flag | Use it when |
|---|---|
| `--yes` / `-y` | Skip the confirmation. Pass this when running on the user's behalf. |
| `--keep-data` | Preserve `~/.local/share/yeet` — the user keeps their `yeet stats` history. Offer this before wiping analytics. |
| `--purge` | Also remove project-level files: `.github/hooks/yeet-rewrite.*`, the Copilot keys in `.vscode/settings.json`, and yeet hooks in a project's `.claude/settings*.json`. **Not on by default** — without it, project files are left alone. |
| `--project <DIR>` | Which project `--purge` cleans (default: cwd). |
| `--dry-run` | Report only. |

By default the analytics database is deleted. If the user has been running yeet for a
while, ask before wiping it — `--keep-data` costs nothing and stats are not recoverable.

## What gets removed

- **The binary, wherever it is.** It checks the manifest path, walks `$PATH` by hand
  (`command -v` only reports the first hit), and probes `/usr/local/bin`,
  `/opt/homebrew/bin`, `~/.local/bin`, `~/bin`, `~/go/bin`, and `$(go env GOPATH)/bin`.
  It only ever deletes a file that identifies itself as yeet when run.
- `~/.claude/hooks/yeet-proxy.sh` and legacy `yeet-rewrite.sh` / `yeet-failure.sh`.
- yeet hook entries from `~/.claude/settings.json` and `settings.local.json` — both the
  `_yeet`-tagged ones and unmarked legacy entries identified by content. Everything
  else in those files is preserved.
- `autoCompactThreshold` restored to whatever it was before (or deleted if the user
  never had it), and `Bash(yeet:*)` removed from `permissions.allow`.
- `~/.claude/yeet-awareness.md` and the `@yeet-awareness.md` line in `CLAUDE.md`. The
  rest of `CLAUDE.md` is kept; the file is deleted only if it held nothing else.
- The delimited yeet block in `~/.copilot/copilot-instructions.md` — the user's own
  content stays. A legacy full-file install is deleted outright.
- `~/.local/share/yeet`, `~/.yeet`, and leftover `*.yeet-bak-*` files.

## Afterwards

Tell the user to **restart Claude Code** — a running session keeps the old hooks in
memory, so Read/Grep/Edit stay blocked until it restarts. In the current shell,
`hash -r` clears the cached path to the deleted binary.

## If it reports leftovers

The script prints each surviving item and exits 1. Almost always this is a root-owned
file:

```bash
sudo bash uninstall.sh --yes
```

If something still survives, that is a bug worth reporting with the printed list —
`https://github.com/hdck007/yeet/issues/new`.

## Common situations

**"I only want the tool blockers gone, keep the binary"** — there is no flag for that.
Uninstall fully, then reinstall with `--binary-only`.

**"Nothing to uninstall" but yeet still works** — the binary is somewhere unusual.
`for d in $(echo $PATH | tr : ' '); do [ -f "$d/yeet" ] && echo "$d/yeet"; done`
will find it; then re-run the uninstaller, which probes PATH the same way.

**Verify the removal is genuinely complete:**
```bash
bash scripts/test-install.sh roundtrip
```
Installs and uninstalls inside a sandbox and asserts the home directory is
byte-for-byte identical afterwards.
