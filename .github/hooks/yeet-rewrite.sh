#!/usr/bin/env bash
# PreToolUse hook for GitHub Copilot (VS Code agent mode)
# Intercepts bash tool calls and rewrites common commands to yeet equivalents.
#
# Input:  PreToolUse JSON on stdin
# Output: {"updatedInput": {"command": "..."}} to transparently rewrite, or exit 0 to pass through
#
# Silently exits 0 on any error (missing jq, missing yeet, no match).

set -euo pipefail

# Require jq
if ! command -v jq &>/dev/null; then
  exit 0
fi

# Require yeet
if ! command -v yeet &>/dev/null; then
  exit 0
fi

INPUT=$(cat)

# Extract the bash command from the tool input.
# VS Code Copilot Chat uses snake_case: tool_name / tool_input.command
TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // empty' 2>/dev/null)
if [ "$TOOL_NAME" != "bash" ] && [ "$TOOL_NAME" != "Bash" ]; then
  exit 0
fi

CMD=$(echo "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null)
if [ -z "$CMD" ]; then
  exit 0
fi

# Already using yeet — pass through
if echo "$CMD" | grep -qE '^(yeet|./yeet)[[:space:]]'; then
  exit 0
fi

NEW_CMD=""

# cat <file>  →  yeet read <file>
if echo "$CMD" | grep -qE '^cat[[:space:]]+[^<|>]'; then
  FILE=$(echo "$CMD" | sed 's/^cat[[:space:]]*//')
  NEW_CMD="yeet read $FILE"

# ls / ls -la / ls -laR  →  yeet ls [path]
elif echo "$CMD" | grep -qE '^ls([[:space:]]|$)'; then
  PATH_ARG=$(echo "$CMD" | sed 's/^ls[[:space:]]*//' | sed 's/^-[a-zA-Z]*[[:space:]]*//')
  if [ -z "$PATH_ARG" ]; then
    NEW_CMD="yeet ls"
  else
    NEW_CMD="yeet ls $PATH_ARG"
  fi

# find . -name "*.ext"  →  yeet glob "**/*.ext" [path]
elif echo "$CMD" | grep -qE '^find[[:space:]]'; then
  # Best-effort: extract -name pattern and base path
  BASE=$(echo "$CMD" | awk '{print $2}')
  # Portable: sed instead of grep -P (macOS/BSD-compatible)
  PATTERN=$(echo "$CMD" | sed -n "s/.*-name[[:space:]]*'\([^']*\)'.*/\1/p" | head -1)
  if [ -z "$PATTERN" ]; then
    PATTERN=$(echo "$CMD" | sed -n 's/.*-name[[:space:]]\+\([^[:space:]]*\)/\1/p' | tr -d "'\"" | head -1)
  fi
  if [ -n "$PATTERN" ]; then
    # Convert shell glob (*.ext) to doublestar (**/*.ext)
    GLOB_PATTERN=$(echo "$PATTERN" | sed 's/^\*\./**\/*./') 
    NEW_CMD="yeet glob \"$GLOB_PATTERN\" $BASE"
  fi

# grep -rn pattern path  →  yeet grep "pattern" [path]
elif echo "$CMD" | grep -qE '^grep[[:space:]]'; then
  PATTERN=$(echo "$CMD" | sed 's/^grep[[:space:]]*//' | sed 's/^-[a-zA-Z]*[[:space:]]*//' | awk '{print $1}')
  PATH_ARG=$(echo "$CMD" | sed 's/^grep[[:space:]]*//' | sed 's/^-[a-zA-Z]*[[:space:]]*//' | awk '{print $2}')
  if [ -n "$PATH_ARG" ]; then
    NEW_CMD="yeet grep $PATTERN $PATH_ARG"
  else
    NEW_CMD="yeet grep $PATTERN"
  fi

# diff file1 file2  →  yeet diff file1 file2
elif echo "$CMD" | grep -qE '^diff[[:space:]]'; then
  ARGS=$(echo "$CMD" | sed 's/^diff[[:space:]]*//' | sed 's/^-[a-zA-Z]*[[:space:]]*//')
  NEW_CMD="yeet diff $ARGS"
fi

# If no rewrite matched, pass through silently
if [ -z "$NEW_CMD" ]; then
  exit 0
fi

# Emit updatedInput response (VS Code Copilot Chat format)
jq -n --arg cmd "$NEW_CMD" '{"updatedInput": {"command": $cmd}}'
