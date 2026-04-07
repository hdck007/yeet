# Yeet — Token-Optimized CLI Wrapper

**yeet** is a token-optimized CLI wrapper. Instead of raw shell commands, always use `yeet` equivalents.
This saves 60-90% of tokens per operation and records analytics automatically.

## Command substitutions

Always prefer `yeet` commands in every terminal/shell operation:

| Instead of | Use |
|---|---|
| `cat file.go` | `yeet read file.go` |
| `cat file.go` | `yeet smart file.go` (quick summary / signatures) |
| `ls` / `ls -laR` | `yeet ls [path]` |
| `find . -name "*.go"` | `yeet glob "**/*.go" [path]` |
| `grep -rn pattern .` | `yeet grep "pattern" [path]` |
| `diff file1 file2` | `yeet diff file1 file2` |

## Reading files

```bash
yeet read file.go                 # full content
yeet read file.go -l moderate     # strip comments, collapse blanks
yeet read file.go -l aggressive   # signatures only (func/type/class/struct)
yeet read file.go -n              # add line numbers
yeet read file.go --max-lines 50  # first 50 lines
yeet smart file.go                # 2-line heuristic summary
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

## Editing files

```bash
# Single replacement
yeet edit file.go --old 'oldText' --new 'newText'

# Replace all occurrences
yeet edit file.go --old 'oldText' --new 'newText' --all

# Multi-line via heredoc
yeet edit file.go << 'EDIT'
old content
|||
new content
EDIT
```

## Searching

```bash
yeet grep "pattern" [path]        # compact file:line results
yeet glob "**/*.go" [path]        # files matching pattern, newest first
yeet find "pattern" [path]        # compact find results
```

## Project structure

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

## Meta commands

```bash
yeet stats          # Token savings dashboard
yeet stats --json   # Machine-readable output
yeet version        # Print version
```

## Verification

Before starting a session, confirm yeet is available:

```bash
yeet version    # Should print version number
yeet stats      # Should show a dashboard (not "command not found")
which yeet      # Should be ~/go/bin/yeet or similar
```

## Build & install

```bash
make build      # Compile (CGO_ENABLED=1 required for SQLite)
make install    # Build and install to $GOPATH/bin
make test       # Run all tests
```

> If `yeet` is not found: run `make install` then add `$GOPATH/bin` (typically `~/go/bin`) to PATH.
