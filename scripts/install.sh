#!/usr/bin/env bash
# install.sh — Set up yeet for Claude Code and/or GitHub Copilot (VS Code)
#
# Usage:
#   bash scripts/install.sh                     # Full install (build + claude + copilot)
#   bash scripts/install.sh --build             # Build & install binary only
#   bash scripts/install.sh --claude            # Claude Code hooks only
#   bash scripts/install.sh --copilot           # Copilot files only
#   bash scripts/install.sh --target /path      # Install into a different project
#   bash scripts/install.sh --help

set -euo pipefail

# ─── Colours ──────────────────────────────────────────────────────────────────
BOLD='\033[1m'
GREEN='\033[32m'
YELLOW='\033[33m'
RED='\033[31m'
CYAN='\033[36m'
DIM='\033[2m'
RESET='\033[0m'

ok()   { echo -e "  ${GREEN}✓${RESET} $*"; }
info() { echo -e "  ${CYAN}→${RESET} $*"; }
warn() { echo -e "  ${YELLOW}!${RESET} $*"; }
err()  { echo -e "  ${RED}✗${RESET} $*" >&2; }
die()  { err "$*"; exit 1; }

# ─── Parse args ───────────────────────────────────────────────────────────────
DO_BUILD=false
DO_CLAUDE=false
DO_COPILOT=false
TARGET=""

if [ $# -eq 0 ]; then
  DO_BUILD=true
  DO_CLAUDE=true
  DO_COPILOT=true
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --build)   DO_BUILD=true ;;
    --claude)  DO_CLAUDE=true ;;
    --copilot) DO_COPILOT=true ;;
    --target)  TARGET="$2"; shift ;;
    --help|-h)
      echo "Usage: bash scripts/install.sh [--build] [--claude] [--copilot] [--target <dir>]"
      echo ""
      echo "  --build    Build & install yeet binary (requires Go + CGO)"
      echo "  --claude   Set up Claude Code hooks in target project"
      echo "  --copilot  Set up GitHub Copilot instructions + hooks in target project"
      echo "  --target   Project directory to install into (default: current repo root)"
      exit 0
      ;;
    *) die "Unknown argument: $1. Run with --help for usage." ;;
  esac
  shift
done

# Determine where this script lives (the yeet repo root)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
YEET_REPO="$(cd "$SCRIPT_DIR/.." && pwd)"

# Target project (defaults to the yeet repo itself)
TARGET="${TARGET:-$YEET_REPO}"

echo ""
echo -e "${BOLD}  yeet installer${RESET}"
echo -e "  ${DIM}repo:   $YEET_REPO${RESET}"
echo -e "  ${DIM}target: $TARGET${RESET}"
echo ""

# ─── 1. Build & install binary ────────────────────────────────────────────────
if $DO_BUILD; then
  echo -e "${BOLD}  [1/3] Build & install${RESET}"

  # Check Go
  if ! command -v go &>/dev/null; then
    die "Go not found. Install from https://go.dev/dl/"
  fi
  GO_VER=$(go version | awk '{print $3}' | sed 's/go//')
  info "Go $GO_VER detected"

  # Check C compiler (required for CGO / mattn/go-sqlite3)
  if ! command -v gcc &>/dev/null && ! command -v clang &>/dev/null; then
    warn "No C compiler found. CGO_ENABLED=1 requires gcc or clang."
    if [[ "$OSTYPE" == "darwin"* ]]; then
      warn "Run: xcode-select --install"
    else
      warn "Run: sudo apt install gcc   (or equivalent for your distro)"
    fi
    die "Install a C compiler and re-run."
  fi

  info "Running: make install (CGO_ENABLED=1) in $YEET_REPO"
  (cd "$YEET_REPO" && CGO_ENABLED=1 make install)

  # Verify binary is on PATH
  if ! command -v yeet &>/dev/null; then
    GOPATH=$(go env GOPATH)
    GOBIN="$GOPATH/bin"
    warn "yeet not found on PATH. Add $GOBIN to PATH, then re-run."
    warn "  bash/zsh: echo 'export PATH=\"$GOBIN:\$PATH\"' >> ~/.bashrc"
    warn "  fish:     fish_add_path $GOBIN"
    die "PATH not set."
  fi

  ok "yeet installed: $(yeet version 2>/dev/null || echo '(version unavailable)')"
fi

# ─── 2. Claude Code hooks ─────────────────────────────────────────────────────
if $DO_CLAUDE; then
  echo ""
  echo -e "${BOLD}  [2/3] Claude Code integration${RESET}"

  # Verify yeet is available
  if ! command -v yeet &>/dev/null; then
    die "yeet not found. Run with --build first."
  fi
  YEET_BIN=$(command -v yeet)

  CLAUDE_DIR="$TARGET/.claude"
  HOOKS_DIR="$CLAUDE_DIR/hooks"
  mkdir -p "$HOOKS_DIR"

  # Copy CLAUDE.md (only if target is a different project)
  if [ "$TARGET" != "$YEET_REPO" ]; then
    cp "$YEET_REPO/CLAUDE.md" "$TARGET/CLAUDE.md"
    ok "CLAUDE.md → $TARGET/CLAUDE.md"
  else
    ok "CLAUDE.md already in place (running in yeet repo)"
  fi

  # yeet-failure.sh hook
  HOOK_SH="$HOOKS_DIR/yeet-failure.sh"
  cp "$YEET_REPO/.claude/hooks/yeet-failure.sh" "$HOOK_SH"
  chmod +x "$HOOK_SH"
  ok "PostToolUse failure hook → $HOOK_SH"

  # settings.local.json — generate fresh with the resolved hook path
  SETTINGS="$CLAUDE_DIR/settings.local.json"
  if [ -f "$SETTINGS" ]; then
    warn "settings.local.json already exists — skipping (edit manually if needed)"
    warn "Reference: $YEET_REPO/.claude/settings.local.json"
  else
    cat > "$SETTINGS" << ENDJSON
{
  "permissions": {
    "allow": [
      "Bash(yeet:*)",
      "Bash(make:*)",
      "Bash(go:*)",
      "Bash(git:*)",
      "Bash(sqlite3:*)",
      "Bash(cat:*)",
      "Bash(bash:*)",
      "Bash(./yeet:*)",
      "Bash(echo:*)",
      "Bash(chmod:*)"
    ],
    "deny": []
  },
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Read",
        "hooks": [{ "type": "command", "command": "echo 'BLOCKED: Use \`yeet read <file>\` instead of the Read tool.' >&2; exit 2" }]
      },
      {
        "matcher": "Glob",
        "hooks": [{ "type": "command", "command": "echo 'BLOCKED: Use \`yeet glob \"<pattern>\" [path]\` instead of the Glob tool.' >&2; exit 2" }]
      },
      {
        "matcher": "Grep",
        "hooks": [{ "type": "command", "command": "echo 'BLOCKED: Use \`yeet grep \"<pattern>\" [path]\` instead of the Grep tool.' >&2; exit 2" }]
      },
      {
        "matcher": "Write",
        "hooks": [{ "type": "command", "command": "echo 'BLOCKED: Pipe content to \`yeet write <file>\` instead of the Write tool.' >&2; exit 2" }]
      },
      {
        "matcher": "Edit",
        "hooks": [{ "type": "command", "command": "echo 'BLOCKED: Use \`yeet edit <file> --old \"...\" --new \"...\"\` instead of the Edit tool.' >&2; exit 2" }]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "bash $HOOK_SH" }]
      }
    ]
  }
}
ENDJSON
    ok "Claude Code settings → $SETTINGS"
  fi

  echo ""
  echo -e "  ${DIM}Claude Code is now configured to:${RESET}"
  echo -e "  ${DIM}  • Block Read/Glob/Grep/Write/Edit tools (force yeet)${RESET}"
  echo -e "  ${DIM}  • Coach Claude to fix yeet source on failure${RESET}"
fi

# ─── 3. Copilot integration ───────────────────────────────────────────────────
if $DO_COPILOT; then
  echo ""
  echo -e "${BOLD}  [3/3] GitHub Copilot (VS Code) integration${RESET}"

  GITHUB_DIR="$TARGET/.github"
  HOOKS_DIR="$GITHUB_DIR/hooks"
  mkdir -p "$HOOKS_DIR"

  # copilot-instructions.md
  cp "$YEET_REPO/.github/copilot-instructions.md" "$GITHUB_DIR/copilot-instructions.md"
  ok "Copilot instructions → $GITHUB_DIR/copilot-instructions.md"

  # Hook script + config
  cp "$YEET_REPO/.github/hooks/yeet-rewrite.sh"  "$HOOKS_DIR/yeet-rewrite.sh"
  cp "$YEET_REPO/.github/hooks/yeet-rewrite.json" "$HOOKS_DIR/yeet-rewrite.json"
  chmod +x "$HOOKS_DIR/yeet-rewrite.sh"
  ok "PreToolUse hook  → $HOOKS_DIR/yeet-rewrite.sh"
  ok "Hook config      → $HOOKS_DIR/yeet-rewrite.json"

  # VS Code settings
  VSCODE_DIR="$TARGET/.vscode"
  VSCODE_SETTINGS="$VSCODE_DIR/settings.json"
  mkdir -p "$VSCODE_DIR"

  if [ -f "$VSCODE_SETTINGS" ]; then
    warn ".vscode/settings.json already exists — add these settings manually:"
    warn "  \"github.copilot.chat.agent.enabled\": true"
    warn "  \"github.copilot.chat.agent.runTasks\": true"
  else
    cat > "$VSCODE_SETTINGS" << 'JSON'
{
  "github.copilot.chat.agent.enabled": true,
  "github.copilot.chat.agent.runTasks": true,
  "github.copilot.chat.useProjectTemplates": true
}
JSON
    ok "VS Code settings → $VSCODE_SETTINGS"
  fi

  echo ""
  echo -e "  ${DIM}Copilot is now configured to:${RESET}"
  echo -e "  ${DIM}  • Load yeet instructions at session start (.github/copilot-instructions.md)${RESET}"
  echo -e "  ${DIM}  • Rewrite bash → yeet commands via PreToolUse hook (agent mode)${RESET}"
fi

# ─── Done ─────────────────────────────────────────────────────────────────────
echo ""
echo -e "  ${BOLD}${GREEN}Done.${RESET}"
echo ""
echo -e "  Verify with:"
echo -e "    ${CYAN}yeet version${RESET}    — confirm binary"
echo -e "    ${CYAN}yeet stats${RESET}      — view token savings"
echo -e "    ${CYAN}bash demo.sh${RESET}    — run interactive demo (from yeet repo)"
echo ""
