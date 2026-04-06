# Yeet Setup Guide

Install and configure **yeet** — a token-optimized CLI wrapper — for use with Claude Code and GitHub Copilot in VS Code.

---

## Prerequisites

| Requirement | Minimum | Notes |
|---|---|---|
| Go | 1.21+ | `go version` |
| C compiler | any | Xcode CLT on macOS (`xcode-select --install`), `gcc` on Linux |
| SQLite headers | system | Bundled via `mattn/go-sqlite3` (CGO required) |

## 1. Build & Install

```bash
# Clone the repo
git clone https://github.com/hdck007/yeet.git
cd yeet

# Build and install the binary to $GOPATH/bin (or ~/go/bin)
make install

# Verify
yeet version
yeet stats
```

If `yeet` is not found after `make install`, add `~/go/bin` to your PATH:

```bash
# bash / zsh
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.bashrc   # or ~/.zshrc

# fish
fish_add_path ~/go/bin
```

---

## 2. Claude Code Integration

Yeet hooks into Claude Code via two config files in `.claude/`:

| File | Purpose |
|---|---|
| `CLAUDE.md` | Tells Claude to always prefer `yeet` commands over built-in tools |
| `.claude/settings.local.json` | `PreToolUse` hooks that block Read/Glob/Grep/Write/Edit tools + `PostToolUse` failure coach |
| `.claude/hooks/yeet-failure.sh` | Coaches Claude to fix `yeet` source when a command fails |

### Automatic setup

Run the install script (see [§4](#4-automated-install-script)):

```bash
bash scripts/install.sh --claude
```

### Manual setup

Copy the project's CLAUDE.md to any project you want yeet-aware:

```bash
cp /path/to/yeet/CLAUDE.md /your/project/CLAUDE.md
```

Then create `.claude/settings.local.json` in your project — see the reference copy at
`.claude/settings.local.json` in this repo.

> **Note:** `.claude/settings.local.json` is local-only and gitignored by default. Use
> `.claude/settings.json` if you want hooks committed to the repo for your whole team.

### Verify Claude Code integration

Open Claude Code in your project. Claude should now:

- Use `yeet read`, `yeet grep`, `yeet glob`, etc. automatically
- Be blocked (exit 2) if it tries to use the built-in Read / Grep / Glob / Write / Edit tools directly
- Receive coaching when a `yeet` command exits non-zero

Check analytics after a few operations:

```bash
yeet stats
```

---

## 3. GitHub Copilot (VS Code) Integration

Copilot integration uses two mechanisms:

### 3a. Custom Instructions (`.github/copilot-instructions.md`)

VS Code Copilot Chat loads `.github/copilot-instructions.md` at the start of every session.
It instructs Copilot to use `yeet` commands instead of raw shell equivalents.

```bash
# Copy into your project
cp /path/to/yeet/.github/copilot-instructions.md /your/project/.github/copilot-instructions.md
```

Or run the install script:

```bash
bash scripts/install.sh --copilot
```

### 3b. PreToolUse Hook (`.github/hooks/yeet-rewrite.sh`)

For Copilot agent mode (VS Code Copilot Chat with tool use enabled), a `PreToolUse` hook
intercepts raw shell commands and transparently rewrites them to `yeet` equivalents.

The hook is a shell script at `.github/hooks/yeet-rewrite.sh`. It is loaded via the
hook config at `.github/hooks/yeet-rewrite.json`.

```bash
# Copy hook files into your project
mkdir -p /your/project/.github/hooks
cp /path/to/yeet/.github/hooks/yeet-rewrite.sh  /your/project/.github/hooks/
cp /path/to/yeet/.github/hooks/yeet-rewrite.json /your/project/.github/hooks/
chmod +x /your/project/.github/hooks/yeet-rewrite.sh
```

> **Note:** If `yeet` is missing or the rewrite fails, the hook exits 0 silently — Copilot
> runs the original command unchanged.

### 3c. VS Code Settings

Enable Copilot agent tool use in `.vscode/settings.json`:

```json
{
  "github.copilot.chat.agent.enabled": true,
  "github.copilot.chat.agent.runTasks": true,
  "github.copilot.chat.useProjectTemplates": true
}
```

### Verify Copilot integration

1. Open VS Code in your project
2. Open Copilot Chat (`Ctrl+Shift+I` / `Cmd+Shift+I`)
3. Ask: `"What Go files are in this project?"`
4. Copilot should run `yeet glob "**/*.go"` instead of `find . -name "*.go"`

---

## 4. Automated Install Script

The `scripts/install.sh` script handles everything in one command:

```bash
# Full install (build + Claude Code + Copilot)
bash scripts/install.sh

# Individual components
bash scripts/install.sh --build       # Build & install yeet binary only
bash scripts/install.sh --claude      # Claude Code hooks only (requires yeet installed)
bash scripts/install.sh --copilot     # Copilot instruction + hook files only

# Install into a different project directory
bash scripts/install.sh --target /path/to/your/project
```

### What the script does

1. **--build**: Runs `make install` (CGO_ENABLED=1), checks `$PATH`, verifies `yeet version`
2. **--claude**: Creates `.claude/hooks/yeet-failure.sh` and `.claude/settings.local.json` in the target project
3. **--copilot**: Creates `.github/copilot-instructions.md`, `.github/hooks/yeet-rewrite.sh`, and `.github/hooks/yeet-rewrite.json`

---

## 5. Command Reference

| Instead of | Use |
|---|---|
| `cat <file>` / `Read` tool | `yeet read <file>` |
| `Read` tool (signatures only) | `yeet read <file> -l aggressive` |
| Quick file summary | `yeet smart <file>` |
| `Edit` tool | `yeet edit <file> --old "..." --new "..."` |
| `find` / `Glob` tool | `yeet glob "<pattern>" [path]` |
| `grep` / `Grep` tool | `yeet grep "<pattern>" [path]` |
| `Write` tool / `cat > file` | `cat <<'EOF' \| yeet write <file>` |
| `ls -laR` | `yeet ls [path]` |
| `find -name` | `yeet find "<pattern>" [path]` |
| `diff` | `yeet diff <file1> <file2>` |

### Analytics

```bash
yeet stats          # Token savings dashboard
yeet stats --json   # Machine-readable JSON
yeet clear          # Reset analytics DB
yeet update         # Rebuild & reinstall from source
```

---

## 6. Troubleshooting

### `yeet: command not found`
- Run `which yeet` and ensure the output path is in your `PATH`
- Check `go env GOPATH` — the binary installs to `$GOPATH/bin`

### `make install` fails with CGO error
- macOS: `xcode-select --install`
- Ubuntu/Debian: `sudo apt install gcc`
- Alpine: `apk add gcc musl-dev`

### Claude Code still uses built-in tools
- Confirm `.claude/settings.local.json` exists in the project root (not `~/.claude/`)
- Check that the PreToolUse hooks are listed under `"hooks"` in that file
- Run `claude --version` to ensure you are on Claude Code ≥ 1.x (hooks require 1.0+)

### Copilot ignores instructions
- Confirm `.github/copilot-instructions.md` exists in the **project root** (not a subdirectory)
- Reload VS Code window (`Cmd+Shift+P → Reload Window`)
- Ensure Copilot Chat extension is up to date

### Hook not firing for Copilot
- Verify `github.copilot.chat.agent.enabled: true` in VS Code settings
- Check that `jq` is installed (`brew install jq` / `apt install jq`)
- Run the hook manually to test: `echo '{}' | bash .github/hooks/yeet-rewrite.sh`
