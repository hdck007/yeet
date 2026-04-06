#!/usr/bin/env bash
# PostToolUse hook: when a yeet command fails, coach Claude to fix the source.
#
# Failure recording happens inside the yeet binary itself (Execute() in root.go).
# This hook only handles the coaching output to Claude.

if ! command -v jq &>/dev/null; then
  exit 0
fi

INPUT=$(cat)

TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // empty')
if [ "$TOOL_NAME" != "Bash" ]; then
  exit 0
fi

CMD=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
EXIT_CODE=$(echo "$INPUT" | jq -r '.tool_response.exit_code // 0')

# Only fire on yeet/./yeet commands that failed
if [[ ! "$CMD" =~ ^(yeet|./yeet)[[:space:]] ]] && [[ "$CMD" != "yeet" ]] && [[ "$CMD" != "./yeet" ]]; then
  exit 0
fi

if [ "$EXIT_CODE" = "0" ] || [ -z "$EXIT_CODE" ]; then
  exit 0
fi

SUBCMD=$(echo "$CMD" | sed 's|^\./yeet ||; s|^yeet ||' | awk '{print $1}')

echo "[yeet-failure] The command failed: $CMD (exit $EXIT_CODE)"
echo ""
echo "You MUST now:"
echo "1. Diagnose why it failed (missing binary, bad output format, unhandled edge case)"
echo "2. Update internal/cli/${SUBCMD}.go (or the relevant file) to add a proper fallback:"
echo "   - If the underlying tool is missing: print a clear error with install instructions"
echo "   - If the output format changed: handle it gracefully and fall back to raw passthrough"
echo "   - If it's a flag/arg issue: fix the argument construction"
echo "3. Run \`make build && make test\` to verify the fix"
echo "4. Re-run the original command to confirm it works"
echo ""
echo "Do not skip this — every yeet command must degrade gracefully."
echo "(Run \`yeet failures\` to review all recorded failures.)"
