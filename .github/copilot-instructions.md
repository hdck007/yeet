# Yeet — Token-Optimized CLI Wrapper

**yeet** is a token-optimized CLI wrapper. Always use `yeet` commands instead of raw shell equivalents.
Saves 40-90% of tokens per operation. Never use `cat`, `ls`, `grep`, `find`, `diff`, `wc` directly.

---

## Mandatory substitutions

| NEVER use | ALWAYS use instead |
|---|---|
| `cat file` / `head` / `tail` | `yeet read <file>` |
| `ls` / `ls -la` / `ls -laR` | `yeet ls [path]` / `yeet ls -laR [path]` |
| `find . -name "*.go"` | `yeet find "*.go" [path]` |
| `grep -rn pattern .` | `yeet grep "pattern" [path]` |
| `diff f1 f2` | `yeet diff f1 f2` |
| `wc -l file` | `yeet wc -l file` |

---

## Smart decisions — choose the right command

Use this decision ladder — stop at the first level that answers your question:

1. `yeet smart <file>` — start here for any unfamiliar file. 2-line summary: type, size, key symbols.
2. `yeet read <file> -l aggressive` — signatures only (func/type/class/struct). Use for API shape.
3. `yeet read <file> -l minimal` — strip comments and blanks. Use when reading logic.
4. `yeet read <file> -n` — full content with line numbers. Use before any edit.
5. `yeet read <file>` — full content. Only when you need everything.

**Exploring a codebase:**

```bash
yeet ls [path]                     # what is in this directory?
yeet ls -laR [path]                # recursive view of all subdirs
yeet smart <file>                  # quick summary before deciding to read fully
yeet grep "SymbolName" [path]      # where is this defined or used?
yeet find "*.go" [path]            # find files by extension
yeet glob "**/*.go" [path]         # glob, sorted by modification time
```

**Editing — always read before writing:**

```bash
yeet read file -n                  # find exact text with line numbers
yeet edit file --old "..." --new "..."       # replace first occurrence
yeet edit file --old "..." --new "..." --all # replace all
```

---

## Reading files

```bash
yeet smart file.go                # 2-line summary — use for unfamiliar files
yeet read file.go -l aggressive   # signatures only: func/type/class/struct
yeet read file.go -l minimal      # strip comments, collapse blanks
yeet read file.go -n              # full content with line numbers (use before editing)
yeet read file.go --max-lines 50  # first 50 lines
yeet read file.go --tail 20       # last 20 lines
yeet read file.go                 # full content (only when you need everything)
```

## Searching

```bash
yeet grep "pattern" [path]         # grouped by file with line content
yeet glob "**/*.go" [path]         # files matching pattern, newest first
yeet find "*.go" [path]            # compact dir-grouped: dir/ f1 f2 f3
```

## Editing files

```bash
yeet edit file.go --old 'oldText' --new 'newText'
yeet edit file.go --old 'oldText' --new 'newText' --all

# Multi-line heredoc
yeet edit file.go << 'EDIT'
old content
|||
new content
EDIT
```

## Writing files

```bash
printf '%s' "content" | yeet write path/to/file

cat <<'EOF' | yeet write path/to/file
line one
line two
EOF
```

## Project structure

```bash
yeet ls [path]                    # directory listing: dirs first, files with sizes
yeet ls -laR [path]               # recursive listing
yeet tree [path]                  # directory tree, filters noise dirs
yeet deps [path]                  # summarize dependencies from lock files
yeet env [filter]                 # filtered env vars, secrets masked
yeet json <file>                  # inspect JSON structure
```

## Language tooling

```bash
yeet tsc                          # TypeScript errors grouped by file
yeet lint [args]                  # ESLint/Biome output grouped by rule
yeet vitest [args]                # test failures only
yeet playwright [args]            # E2E failures only
yeet next [args]                  # Next.js build: routes and bundle sizes
yeet npm [args]                   # npm with auto run injection, filtered
yeet prettier [args]              # files that need formatting only
yeet prisma [args]                # Prisma CLI without ASCII art
```

## Utilities

```bash
yeet diff f1 f2                   # condensed diff
yeet log [file]                   # deduplicated log output
yeet wc -l [file]                 # line count
yeet wc [file]                    # compact word/line/byte count
yeet stats                        # token savings dashboard
yeet version                      # print version
```

## Verify yeet is available

```bash
yeet version
yeet stats
```
