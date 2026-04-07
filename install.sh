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

# ─── 5. Set up Claude Code global proxy hook ──────────────────────────────────
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

if [ ! -f "$SETTINGS_FILE" ]; then
  jq -n --arg cmd "$HOOK_CMD" '{
    "hooks": {
      "PreToolUse": [
        {"matcher": "Read",  "hooks": [{"type": "command", "command": "echo '\''BLOCKED: Use `yeet read <file>` or `yeet smart <file>` instead of the Read tool.'\'' >&2; exit 2"}]},
        {"matcher": "Glob",  "hooks": [{"type": "command", "command": "echo '\''BLOCKED: Use `yeet glob \"<pattern>\" [path]` instead of the Glob tool.'\'' >&2; exit 2"}]},
        {"matcher": "Grep",  "hooks": [{"type": "command", "command": "echo '\''BLOCKED: Use `yeet grep \"<pattern>\" [path]` instead of the Grep tool.'\'' >&2; exit 2"}]},
        {"matcher": "Write", "hooks": [{"type": "command", "command": "echo '\''BLOCKED: Use `cat <<\'EOF\' | yeet write <file>` instead of the Write tool.'\'' >&2; exit 2"}]},
        {"matcher": "Edit",  "hooks": [{"type": "command", "command": "echo '\''BLOCKED: Use `yeet edit <file> --old \"...\" --new \"...\"` instead of the Edit tool.'\'' >&2; exit 2"}]},
        {"matcher": "Bash",  "hooks": [{"type": "command", "command": $cmd}]}
      ]
    }
  } | . + {"autoCompactThreshold": 100000}' > "$SETTINGS_FILE"
  ok "Created ~/.claude/settings.json with blocking hooks + proxy hook" 
else
  TMP_SETTINGS="$(mktemp)"
  jq --arg cmd "$HOOK_CMD" '
    .hooks                //= {} |
    .hooks.PreToolUse     //= [] |

    # Ensure Bash proxy hook is present
    ( if (.hooks.PreToolUse | map(.hooks // [] | map(.command) | any(. == $cmd)) | any)
      then .
      else .hooks.PreToolUse += [{"matcher": "Bash", "hooks": [{"type": "command", "command": $cmd}]}]
      end ) |

    # Ensure blocking hooks are present (idempotent by matcher)
    ( . as $root |
      [ "Read", "Glob", "Grep", "Write", "Edit" ] |
      reduce .[] as $m (
        $root;
        if (.hooks.PreToolUse | map(.matcher) | any(. == $m)) then .
        else
          .hooks.PreToolUse = (
            [ { "matcher": $m,
                "hooks": [{ "type": "command",
                            "command": ("echo '\''BLOCKED: use yeet instead of the " + $m + " tool.'\'' >&2; exit 2") }] }
            ] + .hooks.PreToolUse
          )
        end
      )
    )
  ' "$SETTINGS_FILE"     | jq '. + {"autoCompactThreshold": 100000}'     > "$TMP_SETTINGS" && mv "$TMP_SETTINGS" "$SETTINGS_FILE"
  ok "Updated ~/.claude/settings.json"  
fi

# --- 6. Install yeet awareness instructions for Claude Code -------------------
AWARENESS_FILE="$CLAUDE_GLOBAL/yeet-awareness.md"
CLAUDE_MD="$CLAUDE_GLOBAL/CLAUDE.md"
AWARENESS_REF="@yeet-awareness.md"

info "Downloading yeet-awareness.md..."
curl -fsSL "$RAW_BASE/hooks/claude/yeet-awareness.md" -o "$AWARENESS_FILE" \
  || die "Failed to download yeet-awareness.md"
ok "Awareness instructions → $AWARENESS_FILE"

if [ ! -f "$CLAUDE_MD" ]; then
  printf '%s\n' "$AWARENESS_REF" > "$CLAUDE_MD"
  ok "Created ~/.claude/CLAUDE.md with @yeet-awareness.md reference"
elif ! grep -qF "$AWARENESS_REF" "$CLAUDE_MD"; then
  printf '\n%s\n' "$AWARENESS_REF" >> "$CLAUDE_MD"
  ok "Added @yeet-awareness.md to ~/.claude/CLAUDE.md"
else
  ok "~/.claude/CLAUDE.md already references yeet-awareness.md"
fi

# ─── Done ─────────────────────────────────────────────────────────────────────
echo ""
echo -e "  ${BOLD}${GREEN}Done.${RESET}"
echo ""
echo -e "  ${DIM}Proxy hook active globally: cat/grep/ls/find/diff → yeet equivalents${RESET}"
echo -e "  ${DIM}Native tool blocking active: Read/Glob/Grep/Write/Edit → yeet${RESET}"
echo -e "  ${DIM}Awareness instructions loaded into Claude's global context${RESET}"
echo -e "  ${DIM}Restart Claude Code to pick up the changes.${RESET}"
echo ""
echo -e "  Verify:"
echo -e "    ${CYAN}yeet version${RESET}"
echo -e "    ${CYAN}yeet stats${RESET}"
echo ""
