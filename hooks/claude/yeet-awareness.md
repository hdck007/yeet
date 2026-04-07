# Yeet — Token-Optimized CLI Wrapper

Yeet wraps common tools to produce compact, LLM-optimized output and save tokens.
**Always use yeet commands instead of built-in tools or raw Bash equivalents.**

## Tool replacements

| Instead of | Use |
|---|---|
| Read tool | `yeet read <file>` |
| Glob tool | `yeet glob "<pattern>" [path]` |
| Grep tool | `yeet grep "<pattern>" [path]` |
| Write tool | pipe to `yeet write <file>` |
| Edit tool | `yeet edit <file> --old "..." --new "..."` |
| `cat` | `yeet read <file>` |
| `ls` | `yeet ls [path]` |
| `find` | `yeet find "<pattern>" [path]` |
| `diff` | `yeet diff <file1> <file2>` |
| `grep` | `yeet grep "<pattern>" [path]` |

## Before editing — audit first

Before making any edit, read the file and locate the exact text to change:

```bash
yeet read file.go -n              # read with line numbers to locate the target
yeet grep "pattern" file.go       # search for the text you want to replace
```

Only edit once you know the exact string. This prevents failed replacements.

## Reading files

```bash
yeet read file.go                 # full content
yeet read file.go -l moderate     # strip comments, collapse blanks
yeet read file.go -l aggressive   # signatures only (func/type/class/struct)
yeet read file.go -n              # add line numbers
yeet read file.go --max-lines 50  # first 50 lines
yeet read file.go --tail 20       # last 20 lines
yeet smart file.go                # 2-line heuristic summary
```

## Searching

```bash
yeet grep "pattern" [path]        # compact file:line results
yeet glob "**/*.go" [path]        # files matching pattern, newest first
yeet find "pattern" [path]        # compact find results
```

## Editing files

```bash
# Replace first occurrence
yeet edit file.go --old 'oldText' --new 'newText'

# Replace all occurrences
yeet edit file.go --old 'oldText' --new 'newText' --all

# Multi-line — heredoc mode (best for blocks of code)
yeet edit file.go << 'EDIT'
old content
|||
new content
EDIT
```

## Writing files

```bash
# Pipe content via stdin
printf '%s' "content" | yeet write path/to/file

# Heredoc for multi-line
cat <<'EOF' | yeet write path/to/file
line one
line two
EOF
```

## Compound commands

The proxy hook only rewrites the **first** command in a chain.
Use yeet explicitly in every segment when chaining:

```bash
# Wrong
cd /path && cat file.go
cat file.go | grep "pattern"

# Right
cd /path && yeet read file.go
yeet read file.go | yeet grep "pattern"
```

## Inspecting project structure

```bash
yeet ls [path]                    # token-optimized directory tree
yeet tree [path]                  # directory tree, filters noise dirs
yeet deps [path]                  # summarize dependencies from lock files
yeet env [filter]                 # filtered env vars (secrets masked)
yeet json <file>                  # inspect JSON structure
```

## Language tooling (filtered output)

```bash
yeet tsc                          # TypeScript errors grouped by file
yeet lint [args]                  # ESLint/Biome output grouped by rule
yeet vitest [args]                # test failures only
yeet playwright [args]            # E2E failures only
yeet next [args]                  # Next.js build — routes and bundle sizes
yeet npm [args]                   # npm with auto run injection, filtered
yeet prettier [args]              # files that need formatting only
yeet prisma [args]                # Prisma CLI without ASCII art
```

## Utilities

```bash
yeet diff f1 f2                   # condensed diff
yeet log [file]                   # deduplicated log output
yeet wc [file]                    # compact word/line/byte count
yeet stats                        # token savings dashboard
yeet stats --json                 # machine-readable output
yeet version                      # print version
```
