# Yeet — Token-Optimized CLI Wrapper

**Always use yeet commands. Never use raw Bash equivalents or built-in Read/Glob/Grep/Edit/Write tools.**

## Mandatory replacements

| NEVER use | ALWAYS use instead |
|---|---|
| Read / Glob / Grep / Edit / Write tools | equivalent `yeet` command |
| `cat` / `head` / `tail` | `yeet read <file>` |
| `ls` / `find` / `grep -rn` | `yeet ls` / `yeet find` / `yeet grep` |

## Decision flow — always follow this order

1. **`yeet grep "symbol" path`** — START HERE. Returns match lines with file + line number.
2. **`yeet read <file> --lines N-M`** — when you need more lines around a known line number.
3. **`yeet read <file> -l aggressive`** — full API shape: signatures with original line numbers (91% reduction).
4. **`yeet read <file> -l minimal`** — LAST RESORT only if absolutely necessary.

> **Rule:** grep → `--lines` if you need more → `-l aggressive` for shape.
> **After `yeet edit` or `yeet write`: trust the confirmation — do NOT re-read to verify.**
> Bare `yeet read` on files >150 lines warns and stops — follow the decision flow above.

## Reference

```bash
# Search & explore
yeet grep "pattern" [path]          # grouped matches with file + line numbers
yeet grep "pattern" [path] -C 2         # with 2 context lines (use when match line alone is not enough)
yeet glob "**/*.go" [path]          # files matching pattern, sorted by modification time
yeet find "*.go" [path]             # compact dir-grouped file list
yeet ls [path]                      # directory listing: dirs first, files with sizes
yeet ls -laR [path]                 # recursive listing
yeet tree [path]                    # tree view
yeet smart <file>                   # 2-line summary: type/size/declarations with line numbers

# Read
yeet read <file> --lines N-M        # exact lines (original line numbers preserved)
yeet read <file> --lines N-M -n     # same, with line numbers shown
yeet read <file> -l aggressive      # signatures only — always includes line numbers
yeet read <file> -l minimal         # full content minus comments/blanks (last resort)
yeet read <file> -n                 # full content with line numbers

# Edit & write
yeet edit <file> --old 'old' --new 'new'        # replace first match
yeet edit <file> --old 'old' --new 'new' --all  # replace all
cat <<'WRITE' | yeet write path/to/file         # write/overwrite file
content here
WRITE
```

> In compound commands use `yeet` explicitly in every segment:
> `cd /path && yeet read file.go` ✓   `cd /path && cat file.go` ✗
