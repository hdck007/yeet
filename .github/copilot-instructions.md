# Copilot Instructions for yeet

**yeet** is a token-optimized CLI wrapper. Instead of using raw shell commands or built-in file tools, always use `yeet` equivalents. This saves 60-90% of tokens per operation and records analytics.

## Command Substitutions

**Always prefer `yeet` commands in every terminal/shell operation:**

```bash
# Instead of:           Use:
cat file.go             yeet read file.go
cat file.go             yeet smart file.go        # quick summary / signatures
ls -laR                 yeet ls [path]
find . -name "*.go"     yeet glob "**/*.go" [path]
grep -rn pattern .      yeet grep "pattern" [path]
diff file1 file2        yeet diff file1 file2
```

**Writing files — pipe to yeet write:**

```bash
cat <<'CONTENT' | yeet write path/to/file.go
package main
...
CONTENT
```

**Editing files — use yeet edit:**

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

## Meta Commands

```bash
yeet stats          # Token savings dashboard for this session
yeet stats --json   # Machine-readable output
yeet clear          # Reset analytics
yeet update         # Rebuild and reinstall from source
yeet version        # Print version
```

## Verification

Before starting a session, confirm yeet is available:

```bash
yeet version    # Should print version number
yeet stats      # Should show a dashboard (not "command not found")
which yeet      # Should be ~/go/bin/yeet or similar
```

## Build & Test

```bash
make build      # Compile (CGO_ENABLED=1 required for SQLite)
make install    # Build and install to $GOPATH/bin
make test       # Run all tests
```

> If `yeet` is not found: `make install` then add `$GOPATH/bin` (typically `~/go/bin`) to PATH.
