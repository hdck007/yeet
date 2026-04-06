#!/usr/bin/env bash
# yeet-proxy.sh — PreToolUse hook for Claude Code
# Delegates all rewrite logic to `yeet rewrite`. Do not add rules here.
#
# Exit code protocol from `yeet rewrite`:
#   0  Rewrite found, no deny/ask rule matched → auto-allow
#   1  No yeet equivalent → pass through unchanged
#   2  Deny rule matched → pass through (Claude Code native deny handles it)
#   3  Ask rule matched → rewrite but let Claude Code prompt the user

# --- Audit logging (opt-in via YEET_HOOK_AUDIT=1) ---
_audit_log() {
  if [ "${YEET_HOOK_AUDIT:-0}" != "1" ]; then return; fi
  local action="$1" original="$2" rewritten="${3:--}"
  local dir="${YEET_AUDIT_DIR:-${HOME}/.local/share/yeet}"
  mkdir -p "$dir"
  printf '%s | %s | %s | %s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$action" "$original" "$rewritten" \
    >> "${dir}/hook-audit.log"
}

if ! command -v jq &>/dev/null; then
  echo "[yeet] WARNING: jq is not installed. Hook cannot rewrite commands." >&2
  exit 0
fi

if ! command -v yeet &>/dev/null; then
  echo "[yeet] WARNING: yeet is not installed or not in PATH. Hook cannot rewrite commands." >&2
  exit 0
fi

INPUT=$(cat)
CMD=$(echo "$INPUT" | jq -r '.tool_input.command // empty')

if [ -z "$CMD" ]; then
  _audit_log "skip:empty" "-"
  exit 0
fi

# Skip heredocs early
case "$CMD" in
  *'<<'*) _audit_log "skip:heredoc" "$CMD"; exit 0 ;;
esac

# Delegate all rewrite logic to the yeet binary.
EXIT_CODE=0
REWRITTEN=$(yeet rewrite "$CMD" 2>/dev/null) || EXIT_CODE=$?

case $EXIT_CODE in
  0)
    [ "$CMD" = "$REWRITTEN" ] && { _audit_log "skip:already_yeet" "$CMD"; exit 0; }
    ;;
  1)
    _audit_log "skip:no_match" "$CMD"
    exit 0
    ;;
  2)
    _audit_log "skip:deny_rule" "$CMD"
    exit 0
    ;;
  3)
    # Ask: rewrite but do not auto-allow
    ;;
  *)
    exit 0
    ;;
esac

_audit_log "rewrite" "$CMD" "$REWRITTEN"

ORIGINAL_INPUT=$(echo "$INPUT" | jq -c '.tool_input')
UPDATED_INPUT=$(echo "$ORIGINAL_INPUT" | jq --arg cmd "$REWRITTEN" '.command = $cmd')

if [ "$EXIT_CODE" -eq 3 ]; then
  jq -n \
    --argjson updated "$UPDATED_INPUT" \
    '{
      "hookSpecificOutput": {
        "hookEventName": "PreToolUse",
        "updatedInput": $updated
      }
    }'
else
  jq -n \
    --argjson updated "$UPDATED_INPUT" \
    '{
      "hookSpecificOutput": {
        "hookEventName": "PreToolUse",
        "permissionDecision": "allow",
        "permissionDecisionReason": "yeet auto-rewrite",
        "updatedInput": $updated
      }
    }'
fi
