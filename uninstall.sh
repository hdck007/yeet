#!/usr/bin/env bash
# uninstall.sh — Remove yeet and all its installed components
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/uninstall.sh | bash
#   ./uninstall.sh

set -euo pipefail

REPO="hdck007/yeet"
INSTALL_DIR="/usr/local/bin"
CLAUDE_GLOBAL="$HOME/.claude"
YEET_DATA_DIR="$HOME/.local/share/yeet"

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
skip() { echo -e "  ${DIM}-${RESET} ${DIM}$*${RESET}"; }
die()  { echo -e "  ${RED}✗${RESET} $*" >&2; exit 1; }

_read() {
  if [ -t 0 ]; then
    read -r -p "$1" "$2"
  elif [ -e /dev/tty ]; then
    read -r -p "$1" "$2" </dev/tty
  fi
}

echo ""
echo -e "${BOLD}  yeet uninstaller${RESET}"
echo -e "  ${DIM}https://github.com/$REPO${RESET}"
echo ""

# ─── Feedback note ────────────────────────────────────────────────────────────
echo -e "  If something didn't work or you have feedback, please open an issue —"
echo -e "  it helps make yeet better for everyone."
echo -e "  ${CYAN}https://github.com/$REPO/issues/new${RESET}"
echo ""
echo "$(printf '─%.0s' {1..60})"

# ─── Discover what is installed ───────────────────────────────────────────────
echo ""
echo -e "${BOLD}  Checking what's installed...${RESET}"
echo ""

FOUND=()

# Binary
if [ -f "$INSTALL_DIR/yeet" ]; then
  YEET_VERSION=$("$INSTALL_DIR/yeet" version 2>/dev/null || echo "unknown")
  info "Binary: $INSTALL_DIR/yeet  ($YEET_VERSION)"
  FOUND+=("binary")
else
  skip "Binary: $INSTALL_DIR/yeet  (not found)"
fi

# Claude Code hook
if [ -f "$CLAUDE_GLOBAL/hooks/yeet-proxy.sh" ]; then
  info "Claude hook: $CLAUDE_GLOBAL/hooks/yeet-proxy.sh"
  FOUND+=("claude_hook")
else
  skip "Claude hook: $CLAUDE_GLOBAL/hooks/yeet-proxy.sh  (not found)"
fi

# Claude Code settings.json — yeet hook entries
CLAUDE_SETTINGS="$CLAUDE_GLOBAL/settings.json"
YEET_HOOKS_COUNT=0
if [ -f "$CLAUDE_SETTINGS" ]; then
  YEET_HOOKS_COUNT=$(jq '[.hooks.PreToolUse[]? | select(._yeet == true)] | length' "$CLAUDE_SETTINGS" 2>/dev/null || echo 0)
fi
if [ "$YEET_HOOKS_COUNT" -gt 0 ]; then
  info "Claude settings: $YEET_HOOKS_COUNT yeet hook entries in $CLAUDE_SETTINGS"
  FOUND+=("claude_settings")
else
  skip "Claude settings: no yeet hooks found in $CLAUDE_SETTINGS"
fi

# yeet-awareness.md
if [ -f "$CLAUDE_GLOBAL/yeet-awareness.md" ]; then
  info "Claude awareness: $CLAUDE_GLOBAL/yeet-awareness.md"
  FOUND+=("awareness")
else
  skip "Claude awareness: $CLAUDE_GLOBAL/yeet-awareness.md  (not found)"
fi

# CLAUDE.md reference
CLAUDE_MD="$CLAUDE_GLOBAL/CLAUDE.md"
if [ -f "$CLAUDE_MD" ] && grep -qF "@yeet-awareness.md" "$CLAUDE_MD"; then
  info "CLAUDE.md: @yeet-awareness.md reference in $CLAUDE_MD"
  FOUND+=("claude_md")
else
  skip "CLAUDE.md: no @yeet-awareness.md reference found"
fi

# Copilot instructions
COPILOT_INSTRUCTIONS="$HOME/.copilot/copilot-instructions.md"
if [ -f "$COPILOT_INSTRUCTIONS" ] && grep -qF "yeet" "$COPILOT_INSTRUCTIONS" 2>/dev/null; then
  info "Copilot instructions: $COPILOT_INSTRUCTIONS"
  FOUND+=("copilot_instructions")
else
  skip "Copilot instructions: $COPILOT_INSTRUCTIONS  (not found or not yeet)"
fi

# yeet data dir
if [ -d "$YEET_DATA_DIR" ]; then
  DATA_FILES=$(ls "$YEET_DATA_DIR" 2>/dev/null | tr '\n' ' ')
  info "Data dir: $YEET_DATA_DIR  ($DATA_FILES)"
  FOUND+=("data_dir")
else
  skip "Data dir: $YEET_DATA_DIR  (not found)"
fi

echo ""

if [ ${#FOUND[@]} -eq 0 ]; then
  echo -e "  ${YELLOW}Nothing to uninstall — yeet does not appear to be installed.${RESET}"
  echo ""
  exit 0
fi

# ─── Confirm ──────────────────────────────────────────────────────────────────
echo -e "  ${BOLD}The items above will be removed. This cannot be undone.${RESET}"
echo ""

CONFIRM=""
_read "  Proceed with uninstall? [y/N]: " CONFIRM
CONFIRM="${CONFIRM:-N}"

case "$CONFIRM" in
  [Yy]|[Yy][Ee][Ss]) ;;
  *) echo ""; echo -e "  ${DIM}Aborted — nothing was changed.${RESET}"; echo ""; exit 0 ;;
esac

echo ""
echo "$(printf '─%.0s' {1..60})"
echo ""

# ─── Remove binary ────────────────────────────────────────────────────────────
if [[ " ${FOUND[*]} " == *" binary "* ]]; then
  if [ -w "$INSTALL_DIR" ]; then
    rm -f "$INSTALL_DIR/yeet"
  else
    sudo rm -f "$INSTALL_DIR/yeet"
  fi
  ok "Removed binary: $INSTALL_DIR/yeet"
fi

# ─── Remove Claude hook ───────────────────────────────────────────────────────
if [[ " ${FOUND[*]} " == *" claude_hook "* ]]; then
  rm -f "$CLAUDE_GLOBAL/hooks/yeet-proxy.sh"
  ok "Removed Claude hook: $CLAUDE_GLOBAL/hooks/yeet-proxy.sh"
  if [ -d "$CLAUDE_GLOBAL/hooks" ] && [ -z "$(ls -A "$CLAUDE_GLOBAL/hooks" 2>/dev/null)" ]; then
    rmdir "$CLAUDE_GLOBAL/hooks"
    ok "Removed empty hooks dir"
  fi
fi

# ─── Remove yeet entries from Claude settings.json ───────────────────────────
if [[ " ${FOUND[*]} " == *" claude_settings "* ]]; then
  TMP_SETTINGS="$(mktemp)"
  jq '
    .hooks.PreToolUse  |= map(select(._yeet != true)) |
    if (.hooks.PreToolUse | length) == 0 then del(.hooks.PreToolUse) else . end |
    if (.hooks | length) == 0 then del(.hooks) else . end
  ' "$CLAUDE_SETTINGS" > "$TMP_SETTINGS" && mv "$TMP_SETTINGS" "$CLAUDE_SETTINGS"
  ok "Removed yeet hooks from $CLAUDE_SETTINGS"
fi

# ─── Remove yeet-awareness.md ─────────────────────────────────────────────────
if [[ " ${FOUND[*]} " == *" awareness "* ]]; then
  rm -f "$CLAUDE_GLOBAL/yeet-awareness.md"
  ok "Removed $CLAUDE_GLOBAL/yeet-awareness.md"
fi

# ─── Remove @yeet-awareness.md from CLAUDE.md ────────────────────────────────
if [[ " ${FOUND[*]} " == *" claude_md "* ]]; then
  TMP_MD="$(mktemp)"
  grep -vF "@yeet-awareness.md" "$CLAUDE_MD" > "$TMP_MD" || true
  if [ -s "$TMP_MD" ]; then
    mv "$TMP_MD" "$CLAUDE_MD"
    ok "Removed @yeet-awareness.md from $CLAUDE_MD"
  else
    rm -f "$CLAUDE_MD" "$TMP_MD"
    ok "Removed $CLAUDE_MD (was only yeet content)"
  fi
fi

# ─── Remove Copilot instructions ─────────────────────────────────────────────
if [[ " ${FOUND[*]} " == *" copilot_instructions "* ]]; then
  rm -f "$COPILOT_INSTRUCTIONS"
  ok "Removed $COPILOT_INSTRUCTIONS"
fi

# ─── Remove data dir ─────────────────────────────────────────────────────────
if [[ " ${FOUND[*]} " == *" data_dir "* ]]; then
  rm -rf "$YEET_DATA_DIR"
  ok "Removed data dir: $YEET_DATA_DIR"
fi

# ─── Verify ───────────────────────────────────────────────────────────────────
echo ""
echo "$(printf '─%.0s' {1..60})"
echo ""
echo -e "${BOLD}  Verifying cleanup...${RESET}"
echo ""

LEFTOVER=false

[ -f "$INSTALL_DIR/yeet" ]                                                             && { warn "Still present: $INSTALL_DIR/yeet"; LEFTOVER=true; }
[ -f "$CLAUDE_GLOBAL/hooks/yeet-proxy.sh" ]                                            && { warn "Still present: $CLAUDE_GLOBAL/hooks/yeet-proxy.sh"; LEFTOVER=true; }
[ -f "$CLAUDE_GLOBAL/yeet-awareness.md" ]                                              && { warn "Still present: $CLAUDE_GLOBAL/yeet-awareness.md"; LEFTOVER=true; }
[ -d "$YEET_DATA_DIR" ]                                                                && { warn "Still present: $YEET_DATA_DIR"; LEFTOVER=true; }
[ -f "$COPILOT_INSTRUCTIONS" ] && grep -qF "yeet" "$COPILOT_INSTRUCTIONS" 2>/dev/null && { warn "Still present: $COPILOT_INSTRUCTIONS"; LEFTOVER=true; }

if [ -f "$CLAUDE_SETTINGS" ]; then
  REMAINING=$(jq '[.hooks.PreToolUse[]? | select(._yeet == true)] | length' "$CLAUDE_SETTINGS" 2>/dev/null || echo 0)
  [ "$REMAINING" -gt 0 ] && { warn "Still in settings.json: $REMAINING yeet hook entries"; LEFTOVER=true; }
fi

if [ -f "$CLAUDE_MD" ] && grep -qF "@yeet-awareness.md" "$CLAUDE_MD"; then
  warn "Still in CLAUDE.md: @yeet-awareness.md reference"
  LEFTOVER=true
fi

if $LEFTOVER; then
  echo -e "  ${YELLOW}Some items could not be removed. Check the warnings above.${RESET}"
else
  ok "All clear — yeet has been fully removed"
fi

# ─── Done ─────────────────────────────────────────────────────────────────────
echo ""
echo -e "  ${BOLD}${GREEN}Done.${RESET}"
echo ""
