# Yeet — Token-Optimized CLI Wrapper

Yeet wraps common tools to produce compact, LLM-optimized output.
**Always use yeet commands. Never use raw Bash equivalents or built-in Read/Glob/Grep/Edit/Write tools.**

---

## Mandatory tool replacements

| NEVER use | ALWAYS use instead |
|---|---|
| Read tool | `yeet read <file>` |
| Glob tool | `yeet glob "<pattern>" [path]` |
| Grep tool | `yeet grep "<pattern>" [path]` |
| Write tool | pipe to `yeet write <file>` |
| Edit tool | `yeet edit <file> --old "..." --new "..."` |
| `cat` / `head` / `tail` | `yeet read <file>` |
| `ls` / `ls -la` / `ls -laR` | `yeet ls [path]` / `yeet ls -laR [path]` |
| `find . -name "*.go"` | `yeet find "*.go" [path]` |
| `grep -rn pattern .` | `yeet grep "pattern" [path]` |
| `diff f1 f2` | `yeet diff f1 f2` |
| `wc -l file` | `yeet wc -l file` |

---

## Smart decisions — choose the right command

**Before reading a file, ask: do I need the full content?**

Use this decision ladder — stop at the first level that answers your question:

1. `yeet smart <file>` — always start here for unfamiliar files. Shows type, size, key symbols.
2. `yeet read <file> -l aggressive` — function/type signatures only. Use when you need the API shape.
3. `yeet read <file> -l minimal` — strips comments/blanks. Use when reading logic.
4. `yeet read <file> -n` — full content with line numbers. Use before editing.
5. `yeet read <file>` — full content. Only when you need everything including comments.

**For exploring a codebase:**

```bash
yeet ls [path]                     # start here — what is in this directory?
yeet ls -laR [path]                # recursive if you need to see all subdirs
yeet grep "SymbolName" [path]      # find where something is defined or used
yeet find "*.go" [path]            # find files by extension
yeet glob "**/*.go" [path]         # glob with full path, sorted by modification time
yeet smart <file>                  # quick summary before deciding to read fully
```

**For editing — always read before you write:**

```bash
yeet read file -n                  # read with line numbers to find exact text
yeet edit file --old "..." --new "..."  # replace first occurrence
yeet edit file --old "..." --new "..." --all  # replace all occurrences
```

---

## Reading files

```bash
yeet smart file.go                # 2-line summary — start here for unfamiliar files
yeet read file.go -l aggressive   # signatures only: func/type/class/struct
yeet read file.go -l minimal      # strip comments, collapse blanks
yeet read file.go -l moderate     # balanced filtering
yeet read file.go -n              # full content with line numbers (use before editing)
yeet read file.go --max-lines 50  # first 50 lines
yeet read file.go --tail 20       # last 20 lines
yeet read file.go                 # full content (only when you need everything)
```

## Searching

```bash
yeet grep "pattern" [path]         # grouped: [file] path (N)  /  linenum: content
yeet glob "**/*.go" [path]         # files matching pattern, sorted newest first
yeet find "*.go" [path]            # compact dir-grouped: dir/ f1 f2 f3
```

## Editing files

```bash
# Replace first occurrence
yeet edit file.go --old 'oldText' --new 'newText'

# Replace all occurrences
yeet edit file.go --old 'oldText' --new 'newText' --all

# Multi-line heredoc mode
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

## Compound commands

The proxy hook rewrites only the first command in a chain.
Always use yeet explicitly in every segment:

```bash
# Wrong — second command bypasses yeet
cd /path && cat file.go

# Right
cd /path && yeet read file.go
yeet read file.go | yeet grep "pattern"
```

## Project structure

```bash
yeet ls [path]                    # directory listing: dirs first, files with sizes
yeet ls -laR [path]               # recursive listing
yeet tree [path]                  # tree view, filters noise dirs
yeet deps [path]                  # summarize dependencies from lock files
yeet env [filter]                 # filtered env vars, secrets masked
yeet json <file>                  # inspect JSON structure compactly
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
