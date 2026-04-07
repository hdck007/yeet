#!/usr/bin/env bash
# install.sh — One-line installer for yeet
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/install.sh | bash

set -euo pipefail

REPO="hdck007/yeet"
INSTALL_DIR="/usr/local/bin"
CLAUDE_GLOBAL="$HOME/.claude"
RAW_BASE="https://raw.githubusercontent.com/$REPO/main"

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
die()  { echo -e "  ${RED}✗${RESET} $*" >&2; exit 1; }

echo ""
echo -e "${BOLD}  yeet installer${RESET}"
echo -e "  ${DIM}https://github.com/$REPO${RESET}"
echo ""

# ─── 1. Detect platform ───────────────────────────────────────────────────────
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin) FILE_NAME="yeet-darwin-universal" ;;
  Linux)
    case "$ARCH" in
      x86_64)  FILE_NAME="yeet-linux-amd64" ;;
      aarch64) FILE_NAME="yeet-linux-arm64" ;;
      *) die "Unsupported architecture: $ARCH" ;;
    esac
    ;;
  *) die "Unsupported OS: $OS" ;;
esac

# ─── 2. Fetch latest release version ─────────────────────────────────────────
info "Fetching latest release..."
VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
  | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
[ -z "$VERSION" ] && die "Could not determine latest version. Check https://github.com/$REPO/releases"
ok "Latest version: $VERSION"

# ─── 3. Download & install binary ────────────────────────────────────────────
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$FILE_NAME"
TMP_BIN="$(mktemp)"
trap "rm -f $TMP_BIN" EXIT

info "Downloading $FILE_NAME..."
curl -fsSL "$DOWNLOAD_URL" -o "$TMP_BIN" || die "Download failed: $DOWNLOAD_URL"
chmod +x "$TMP_BIN"
[ "$OS" = "Darwin" ] && xattr -d com.apple.quarantine "$TMP_BIN" 2>/dev/null || true

info "Installing to $INSTALL_DIR/yeet..."
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP_BIN" "$INSTALL_DIR/yeet"
else
  sudo mv "$TMP_BIN" "$INSTALL_DIR/yeet"
fi
ok "Binary installed: $(yeet version 2>/dev/null || echo "$VERSION")"

# ─── 4. Install jq if needed ─────────────────────────────────────────────────
if ! command -v jq &>/dev/null; then
  info "jq not found — installing..."
  if [ "$OS" = "Darwin" ] && command -v brew &>/dev/null; then
    brew install jq >/dev/null
  elif command -v apt-get &>/dev/null; then
    sudo apt-get install -y jq >/dev/null
  elif command -v yum &>/dev/null; then
    sudo yum install -y jq >/dev/null
  elif command -v apk &>/dev/null; then
    sudo apk add jq >/dev/null
  else
    die "Cannot auto-install jq. Install manually: https://stedolan.github.io/jq/download/"
  fi
  ok "jq installed"
fi

# ─── 5. Choose AI integration ─────────────────────────────────────────────────
echo ""
echo -e "${BOLD}  AI integration${RESET}"
echo ""
echo -e "  Which AI assistant do you use?"
echo ""
echo -e "  ${CYAN}1)${RESET} Claude Code"
echo -e "  ${CYAN}2)${RESET} GitHub Copilot"
echo -e "  ${CYAN}3)${RESET} Both"
echo -e "  ${CYAN}4)${RESET} Skip"
echo ""

# Read from /dev/tty so this works when piped through curl | bash
CHOICE=""
if [ -t 0 ]; then
  read -r -p "  Choice [3]: " CHOICE
elif [ -e /dev/tty ]; then
  read -r -p "  Choice [3]: " CHOICE </dev/tty
fi
CHOICE="${CHOICE:-3}"

DO_CLAUDE=false
DO_COPILOT=false
case "$CHOICE" in
  1) DO_CLAUDE=true ;;
  2) DO_COPILOT=true ;;
  3) DO_CLAUDE=true; DO_COPILOT=true ;;
  4) ;;
  *) warn "Invalid choice '$CHOICE', defaulting to both"; DO_CLAUDE=true; DO_COPILOT=true ;;
esac

# ─── 6. Claude Code setup ────────────────────────────────────────────────────
if $DO_CLAUDE; then
  echo ""
  echo -e "${BOLD}  Setting up Claude Code (global)${RESET}"

  HOOKS_DIR="$CLAUDE_GLOBAL/hooks"
  mkdir -p "$HOOKS_DIR"

  info "Downloading yeet-proxy.sh..."
  curl -fsSL "$RAW_BASE/hooks/yeet-proxy.sh" -o "$HOOKS_DIR/yeet-proxy.sh" \
    || die "Failed to download yeet-proxy.sh"
  chmod +x "$HOOKS_DIR/yeet-proxy.sh"
  ok "Proxy hook → $HOOKS_DIR/yeet-proxy.sh"

  SETTINGS_FILE="$CLAUDE_GLOBAL/settings.json"
  HOOK_CMD="bash \"$HOOKS_DIR/yeet-proxy.sh\""
  TMP_SETTINGS="$(mktemp)"

  WRITE_CMD='echo "BLOCKED: Use `cat <<'"'"'EOF'"'"' | yeet write <file>` instead of the Write tool." >&2; exit 2'
  YEET_HOOKS=$(jq -n --arg cmd "$HOOK_CMD" --arg write_cmd "$WRITE_CMD" '[
    {"matcher": "Read",  "hooks": [{"type": "command", "command": "echo '\''BLOCKED: Use `yeet read <file>` or `yeet smart <file>` instead of the Read tool.'\'' >&2; exit 2"}]},
    {"matcher": "Glob",  "hooks": [{"type": "command", "command": "echo '\''BLOCKED: Use `yeet glob \"<pattern>\" [path]` instead of the Glob tool.'\'' >&2; exit 2"}]},
    {"matcher": "Grep",  "hooks": [{"type": "command", "command": "echo '\''BLOCKED: Use `yeet grep \"<pattern>\" [path]` instead of the Grep tool.'\'' >&2; exit 2"}]},
    {"matcher": "Write", "hooks": [{"type": "command", "command": $write_cmd}]},
    {"matcher": "Edit",  "hooks": [{"type": "command", "command": "echo '\''BLOCKED: Use `yeet edit <file> --old \"...\" --new \"...\"` instead of the Edit tool.'\'' >&2; exit 2"}]},
    {"matcher": "Bash",  "hooks": [{"type": "command", "command": $cmd}]}
  ]')

  if [ ! -f "$SETTINGS_FILE" ]; then
    jq -n --argjson hooks "$YEET_HOOKS" \
      '{"hooks": {"PreToolUse": $hooks}, "autoCompactThreshold": 100000}' \
      > "$SETTINGS_FILE"
    ok "Created ~/.claude/settings.json"
  else
    jq --argjson hooks "$YEET_HOOKS" '
      .hooks             //= {} |
      .hooks.PreToolUse  //= [] |
      .hooks.PreToolUse  |= map(select(._yeet != true)) |
      .hooks.PreToolUse  = $hooks + .hooks.PreToolUse |
      .autoCompactThreshold = 100000
    ' "$SETTINGS_FILE" > "$TMP_SETTINGS" && mv "$TMP_SETTINGS" "$SETTINGS_FILE"
    ok "Updated ~/.claude/settings.json"
  fi

  AWARENESS_FILE="$CLAUDE_GLOBAL/yeet-awareness.md"
  CLAUDE_MD="$CLAUDE_GLOBAL/CLAUDE.md"
  AWARENESS_REF="@yeet-awareness.md"

  # Always re-download so the awareness stays current with every install/upgrade
  info "Downloading yeet-awareness.md (latest)..."
  curl -fsSL "$RAW_BASE/hooks/claude/yeet-awareness.md" -o "$AWARENESS_FILE" \
    || die "Failed to download yeet-awareness.md"
  ok "Awareness instructions → $AWARENESS_FILE"

  # Ensure @yeet-awareness.md is the FIRST line of CLAUDE.md so it takes
  # precedence over any other project-level instructions.
  if [ ! -f "$CLAUDE_MD" ]; then
    printf '%s\n' "$AWARENESS_REF" > "$CLAUDE_MD"
    ok "Created ~/.claude/CLAUDE.md with @yeet-awareness.md as first entry"
  elif grep -qF "$AWARENESS_REF" "$CLAUDE_MD"; then
    # Already present — move it to the top if it is not already there
    if [ "$(head -1 "$CLAUDE_MD")" != "$AWARENESS_REF" ]; then
      TMP_MD="$(mktemp)"
      printf '%s\n' "$AWARENESS_REF" > "$TMP_MD"
      grep -vF "$AWARENESS_REF" "$CLAUDE_MD" >> "$TMP_MD"
      mv "$TMP_MD" "$CLAUDE_MD"
      ok "Moved @yeet-awareness.md to top of ~/.claude/CLAUDE.md"
    else
      ok "~/.claude/CLAUDE.md already has @yeet-awareness.md at top"
    fi
  else
    TMP_MD="$(mktemp)"
    printf '%s\n' "$AWARENESS_REF" > "$TMP_MD"
    cat "$CLAUDE_MD" >> "$TMP_MD"
    mv "$TMP_MD" "$CLAUDE_MD"
    ok "Prepended @yeet-awareness.md to ~/.claude/CLAUDE.md (top priority)"
  fi
fi

# ─── 7. Copilot setup ────────────────────────────────────────────────────────
if $DO_COPILOT; then
  echo ""
  echo -e "${BOLD}  Setting up GitHub Copilot${RESET}"

  GITHUB_HOOKS_DIR="$PWD/.github/hooks"
  mkdir -p "$GITHUB_HOOKS_DIR"

  # Install copilot-instructions.md globally so Copilot picks it up in every project
  GLOBAL_COPILOT_DIR="$HOME/.copilot"
  mkdir -p "$GLOBAL_COPILOT_DIR"
  info "Downloading copilot-instructions.md (latest)..."
  curl -fsSL "$RAW_BASE/hooks/copilot/yeet-awareness.md" \
    -o "$GLOBAL_COPILOT_DIR/copilot-instructions.md" \
    || die "Failed to download copilot-instructions.md"
  ok "Copilot instructions (global) → $GLOBAL_COPILOT_DIR/copilot-instructions.md"

  info "Downloading yeet-rewrite.sh..."
  curl -fsSL "$RAW_BASE/.github/hooks/yeet-rewrite.sh" \
    -o "$GITHUB_HOOKS_DIR/yeet-rewrite.sh" \
    || die "Failed to download yeet-rewrite.sh"
  chmod +x "$GITHUB_HOOKS_DIR/yeet-rewrite.sh"
  ok "PreToolUse hook → $GITHUB_HOOKS_DIR/yeet-rewrite.sh"

  info "Downloading yeet-rewrite.json..."
  curl -fsSL "$RAW_BASE/.github/hooks/yeet-rewrite.json" \
    -o "$GITHUB_HOOKS_DIR/yeet-rewrite.json" \
    || die "Failed to download yeet-rewrite.json"
  ok "Hook config → $GITHUB_HOOKS_DIR/yeet-rewrite.json"

  # VS Code settings
  VSCODE_DIR="$PROJECT_DIR/.vscode"
  VSCODE_SETTINGS="$VSCODE_DIR/settings.json"
  mkdir -p "$VSCODE_DIR"

  if [ ! -f "$VSCODE_SETTINGS" ]; then
    cat > "$VSCODE_SETTINGS" << 'JSON'
{
  "github.copilot.chat.agent.enabled": true,
  "github.copilot.chat.agent.runTasks": true,
  "github.copilot.chat.useProjectTemplates": true
}
JSON
    ok "VS Code settings → $VSCODE_SETTINGS"
  else
    warn ".vscode/settings.json already exists — add these manually if needed:"
    warn "  \"github.copilot.chat.agent.enabled\": true"
    warn "  \"github.copilot.chat.agent.runTasks\": true"
  fi

  echo ""
  echo -e "  ${DIM}Installed to: $PROJECT_DIR${RESET}"
  echo -e "  ${DIM}Commit .github/ to your repo so teammates get it too.${RESET}"
fi

# ─── Done ─────────────────────────────────────────────────────────────────────
echo ""
echo -e "  ${BOLD}${GREEN}Done.${RESET}"
echo ""

if $DO_CLAUDE; then
  echo -e "  ${DIM}Claude Code: proxy hook active globally, awareness loaded${RESET}"
  echo -e "  ${DIM}Restart Claude Code to pick up the changes.${RESET}"
fi
if $DO_COPILOT; then
  echo -e "  ${DIM}Copilot: instructions + PreToolUse hook installed in $PWD/.github/${RESET}"
fi

echo ""
echo -e "  Verify:"
echo -e "    ${CYAN}yeet version${RESET}"
echo -e "    ${CYAN}yeet stats${RESET}"
echo ""
