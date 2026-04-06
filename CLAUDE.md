# Yeet — Token-Optimized CLI Wrapper

## MANDATORY: Use yeet instead of built-in tools

> **Precedence rule:** `yeet` commands ALWAYS take priority over built-in tools, Bash equivalents, or any other approach. When in doubt, reach for `yeet` first. If a `yeet` command fails, fix the source — do not fall back to the raw tool.

**You MUST use `yeet` commands via the Bash tool instead of the equivalent built-in tools.** This saves tokens and records analytics.

| Instead of | Use |
|-----------|-----|
| `Read` tool | `yeet read <file>` |
| `Read` tool (code overview) | `yeet read <file> -l aggressive` |
| `Read` tool (quick summary) | `yeet smart <file>` |
| `Edit` tool | `yeet edit <file> --old "..." --new "..."` |
| `Glob` tool | `yeet glob "<pattern>" [path]` |
| `Grep` tool | `yeet grep "<pattern>" [path]` |
| `Write` tool | `yeet write <file> --b64 <base64-encoded-content>` |
| `ls` via Bash | `yeet ls [path]` |
| `find` via Bash | `yeet find "<pattern>" [path]` |
| `diff` via Bash | `yeet diff <file1> <file2>` |

### Rules

- **ALWAYS** prefer `yeet` — it has precedence over all built-in tools and raw Bash equivalents
- **ALWAYS** use `yeet read` instead of the `Read` tool
- **ALWAYS** use `yeet edit` instead of the `Edit` tool
- **ALWAYS** use `yeet glob` instead of the `Glob` tool
- **ALWAYS** use `yeet grep` instead of the `Grep` tool
- **ALWAYS** use `yeet write <file> --b64 <base64>` instead of the `Write` tool — base64 content has no shell escaping issues
- **ALWAYS** use `yeet ls` instead of `ls` in Bash
- **ALWAYS** use `yeet find` instead of `find` in Bash
- **ALWAYS** use `yeet diff` instead of `diff` in Bash
- **NEVER** use heredocs, pipes, or Python to write files — use `--b64` instead
- **IF** a `yeet` command fails: diagnose and fix `internal/cli/<cmd>.go`, then re-run — do NOT fall back to the raw tool
- Use `yeet smart <file>` for quick file overviews before diving deeper
- Use `yeet read <file> -l aggressive` to see only function/type signatures

### Edit patterns

```bash
# Replace first occurrence
yeet edit file.go --old 'oldText' --new 'newText'

# Replace all occurrences
yeet edit file.go --old 'oldText' --new 'newText' --all

# Pipe mode (old|||new delimiter) — for simple single-line replacements
echo 'oldText|||newText' | yeet edit file.go

# Heredoc mode — PREFERRED for multi-line or complex replacements
yeet edit file.go << 'EDIT'
old content
possibly multi-line
|||
new content
possibly multi-line
EDIT
```

### Write pattern

Base64-encode the content and pass via `--b64`. No heredocs, no pipes, no shell escaping issues — works in every shell including fish.

```bash
# Encode content to base64, then write
yeet write path/to/file.go --b64 $(printf '%s' 'package main\n\nfunc main() {}' | base64)

# Or if you have the base64 string already (Claude generates it directly):
yeet write path/to/file.go --b64 cGFja2FnZSBtYWluCgpmdW5jIG1haW4oKSB7fQo=
```

> **For AI agents:** Generate the file content, base64-encode it mentally or via a subshell, pass as `--b64`. The base64 alphabet (A–Z, a–z, 0–9, +, /, =) contains zero shell-special characters.

### Other commands

```bash
yeet stats          # View token savings dashboard
yeet stats --json   # Machine-readable output
yeet clear          # Reset all analytics
yeet update         # Rebuild and reinstall from source
yeet version        # Print version
```

## Project Info

- **Language:** Go 1.25
- **Build:** `make build` (requires CGO_ENABLED=1)
- **Test:** `make test`
- **Dependencies:** cobra, mattn/go-sqlite3
- **Analytics DB:** ~/.local/share/yeet/analytics.db
