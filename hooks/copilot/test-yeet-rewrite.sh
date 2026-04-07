#!/usr/bin/env bash
# Test suite for yeet-rewrite.sh (GitHub Copilot PreToolUse hook).
# Feeds mock PreToolUse JSON through the hook and verifies rewrite/pass-through behaviour.
#
# Usage: bash hooks/copilot/test-yeet-rewrite.sh
#
# VS Code Copilot Chat input format:
#   {"tool_name":"Bash","tool_input":{"command":"..."}}
#   Output on intercept: {"updatedInput":{"command":"<rewritten>"}}
#   Output on pass-through: (empty, exit 0)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
YEET_REPO="$(cd "$SCRIPT_DIR/../.." && pwd)"
HOOK="${HOOK_SCRIPT:-$YEET_REPO/.github/hooks/yeet-rewrite.sh}"

if [ ! -f "$HOOK" ]; then
  echo "Hook not found: $HOOK" >&2
  exit 1
fi

if ! command -v jq &>/dev/null; then
  echo "jq is required. Install with: brew install jq" >&2
  exit 1
fi

PASS=0
FAIL=0
TOTAL=0

GREEN='\033[32m'
RED='\033[31m'
DIM='\033[2m'
RESET='\033[0m'

# Build VS Code Copilot Chat PreToolUse input
vscode_input() {
  local cmd="$1"
  jq -cn --arg cmd "$cmd" '{"tool_name":"Bash","tool_input":{"command":$cmd}}'
}

# Assert: hook rewrites command to exactly the expected string
test_rewrite() {
  local description="$1" input_cmd="$2" expected_cmd="$3"
  TOTAL=$((TOTAL + 1))

  local output updated_cmd
  output=$(vscode_input "$input_cmd" | bash "$HOOK" 2>/dev/null) || true
  updated_cmd=$(echo "$output" | jq -r '.updatedInput.command // empty' 2>/dev/null)

  if [ "$updated_cmd" = "$expected_cmd" ]; then
    printf "  ${GREEN}REWRITE${RESET} %s ${DIM}→ %s${RESET}\n" "$description" "$updated_cmd"
    PASS=$((PASS + 1))
  else
    printf "  ${RED}FAIL${RESET} %s\n" "$description"
    printf "       expected: %s\n" "$expected_cmd"
    printf "       actual:   %s\n" "$updated_cmd"
    FAIL=$((FAIL + 1))
  fi
}

# Assert: hook emits no output (pass-through)
test_passthrough() {
  local description="$1" input="$2"
  TOTAL=$((TOTAL + 1))

  local output
  output=$(echo "$input" | bash "$HOOK" 2>/dev/null) || true

  if [ -z "$output" ]; then
    printf "  ${GREEN}PASS${RESET}    %s ${DIM}→ (pass-through)${RESET}\n" "$description"
    PASS=$((PASS + 1))
  else
    printf "  ${RED}FAIL${RESET} %s\n" "$description"
    printf "       expected: (no output)\n"
    printf "       actual:   %s\n" "$output"
    FAIL=$((FAIL + 1))
  fi
}

echo "=============================================="
echo "  Yeet Rewrite Hook Test Suite"
printf "  Hook: ${DIM}%s${RESET}\n" "$HOOK"
echo "=============================================="
echo ""

# ── Rewrites ──────────────────────────────────────────────────────────────────
echo "--- Commands that should be rewritten ---"

test_rewrite "cat file.go" \
  "cat file.go" \
  "yeet read file.go"

test_rewrite "cat src/main.go" \
  "cat src/main.go" \
  "yeet read src/main.go"

test_rewrite "ls (bare)" \
  "ls" \
  "yeet ls"

test_rewrite "ls path/" \
  "ls src/" \
  "yeet ls src/"

test_rewrite "ls -la path/" \
  "ls -la src/" \
  "yeet ls src/"

test_rewrite "grep pattern src/" \
  "grep -rn pattern src/" \
  "yeet grep pattern src/"

test_rewrite "grep pattern (no path)" \
  "grep pattern" \
  "yeet grep pattern"

test_rewrite "diff a b" \
  "diff file1 file2" \
  "yeet diff file1 file2"

test_rewrite "find . -name '*.go'" \
  "find . -name '*.go'" \
  'yeet glob "**/*.go" .'

echo ""

# ── Pass-throughs ─────────────────────────────────────────────────────────────
echo "--- Commands that should pass through unchanged ---"

test_passthrough "already yeet read" \
  "$(vscode_input "yeet read file.go")"

test_passthrough "already yeet ls" \
  "$(vscode_input "yeet ls src/")"

test_passthrough "already yeet grep" \
  "$(vscode_input "yeet grep pattern src/")"

test_passthrough "make build" \
  "$(vscode_input "make build")"

test_passthrough "go test ./..." \
  "$(vscode_input "go test ./...")"

test_passthrough "echo hello" \
  "$(vscode_input "echo hello world")"

test_passthrough "non-bash tool" \
  "$(jq -cn '{"tool_name":"editFiles"}')"

test_passthrough "empty command" \
  "$(jq -cn '{"tool_name":"Bash","tool_input":{"command":""}}')"

echo ""

# ── Output format ─────────────────────────────────────────────────────────────
echo "--- Output format ---"

TOTAL=$((TOTAL + 1))
raw=$(vscode_input "cat file.go" | bash "$HOOK" 2>/dev/null)
if echo "$raw" | jq . >/dev/null 2>&1; then
  printf "  ${GREEN}PASS${RESET}    output is valid JSON\n"
  PASS=$((PASS + 1))
else
  printf "  ${RED}FAIL${RESET} output is not valid JSON: %s\n" "$raw"
  FAIL=$((FAIL + 1))
fi

TOTAL=$((TOTAL + 1))
updated=$(echo "$raw" | jq -r '.updatedInput.command // empty')
if echo "$updated" | grep -q "^yeet "; then
  printf "  ${GREEN}PASS${RESET}    updatedInput.command starts with yeet ${DIM}→ %s${RESET}\n" "$updated"
  PASS=$((PASS + 1))
else
  printf "  ${RED}FAIL${RESET} updatedInput.command should start with yeet: %s\n" "$updated"
  FAIL=$((FAIL + 1))
fi

echo ""
echo "=============================================="
if [ $FAIL -eq 0 ]; then
  printf "  ${GREEN}ALL $TOTAL TESTS PASSED${RESET}\n"
else
  printf "  ${RED}$FAIL FAILED${RESET} / $TOTAL total ($PASS passed)\n"
fi
echo "=============================================="
exit $FAIL
