#!/usr/bin/env bash
# Requires: jq

INPUT=$(cat)
CMD=$(echo "$INPUT" | jq -r '.tool_input.command // empty')

if [ -z "$CMD" ]; then exit 0; fi

REWRITTEN="$CMD"
SHOULD_REWRITE=false

# REWRITE RULES
# 1. Rewrite 'cat' to 'yeet read'
if [[ "$CMD" =~ ^cat\ (.*) ]]; then
    REWRITTEN="yeet read ${BASH_REMATCH[1]}"
    SHOULD_REWRITE=true
# 2. Rewrite 'grep' to 'yeet grep'
elif [[ "$CMD" =~ ^grep\ (.*) ]]; then
    REWRITTEN="yeet grep ${BASH_REMATCH[1]}"
    SHOULD_REWRITE=true
fi

if [ "$SHOULD_REWRITE" = true ]; then
    # Return the rewritten command with auto-allow permission
    jq -n \
      --argjson original "$INPUT" \
      --arg new_cmd "$REWRITTEN" \
      '{
        "hookSpecificOutput": {
          "hookEventName": "PreToolUse",
          "permissionDecision": "allow",
          "permissionDecisionReason": "Yeet auto-rewrite for performance",
          "updatedInput": ($original.tool_input | .command = $new_cmd)
        }
      }'
else
    # If no rewrite rule matches, let Claude run the original command
    exit 0
fi