#!/usr/bin/env bash
# Test suite for yeet-proxy.sh
# Usage: bash hooks/test-yeet-proxy.sh

HOOK="${HOOK:-$(dirname "$0")/yeet-proxy.sh}"
PASS=0
FAIL=0
TOTAL=0

GREEN='\033[32m'
RED='\033[31m'
DIM='\033[2m'
RESET='\033[0m'

test_rewrite() {
  local description="$1"
  local input_cmd="$2"
  local expected_cmd="$3"  # empty = expect no rewrite
  TOTAL=$((TOTAL + 1))

  local input_json
  input_json=$(jq -n --arg cmd "$input_cmd" '{"tool_name":"Bash","tool_input":{"command":$cmd}}')
  local output
  output=$(echo "$input_json" | bash "$HOOK" 2>/dev/null) || true

  if [ -z "$expected_cmd" ]; then
    if [ -z "$output" ]; then
      printf "  ${GREEN}PASS${RESET} %s ${DIM}→ (no rewrite)${RESET}\n" "$description"
      PASS=$((PASS + 1))
    else
      local actual
      actual=$(echo "$output" | jq -r '.hookSpecificOutput.updatedInput.command // empty')
      printf "  ${RED}FAIL${RESET} %s\n" "$description"
      printf "       expected: (no rewrite)\n"
      printf "       actual:   %s\n" "$actual"
      FAIL=$((FAIL + 1))
    fi
  else
    local actual
    actual=$(echo "$output" | jq -r '.hookSpecificOutput.updatedInput.command // empty' 2>/dev/null)
    if [ "$actual" = "$expected_cmd" ]; then
      printf "  ${GREEN}PASS${RESET} %s ${DIM}→ %s${RESET}\n" "$description" "$actual"
      PASS=$((PASS + 1))
    else
      printf "  ${RED}FAIL${RESET} %s\n" "$description"
      printf "       expected: %s\n" "$expected_cmd"
      printf "       actual:   %s\n" "$actual"
      FAIL=$((FAIL + 1))
    fi
  fi
}

echo "============================================"
echo "  yeet Proxy Hook Test Suite"
echo "============================================"
echo ""

echo "--- Core rewrites ---"
test_rewrite "cat file"         "cat README.md"          "yeet read README.md"
test_rewrite "grep pattern"     "grep foo ."             "yeet grep foo ."
test_rewrite "grep with flags"  "grep -rn foo src/"      "yeet grep -rn foo src/"
test_rewrite "ls with path"     "ls src/"                "yeet ls src/"
test_rewrite "find pattern"     "find . -name '*.go'"    "yeet find . -name '*.go'"
test_rewrite "diff two files"   "diff a.go b.go"         "yeet diff a.go b.go"

echo ""
echo "--- Env var prefix handling ---"
test_rewrite "env + cat"        "DEBUG=1 cat foo.go"     "DEBUG=1 yeet read foo.go"
test_rewrite "env + grep"       "CI=1 grep foo ."        "CI=1 yeet grep foo ."

echo ""
echo "--- Should NOT rewrite ---"
test_rewrite "already yeet"     "yeet read foo.go"       ""
test_rewrite "heredoc"          "cat <<'EOF'
hello
EOF"                                                      ""
test_rewrite "echo"             "echo hello"             ""
test_rewrite "cd"               "cd /tmp"                ""
test_rewrite "make"             "make build"             ""
test_rewrite "go test"          "go test ./..."          ""

echo ""
echo "--- Audit logging (YEET_HOOK_AUDIT=1) ---"
AUDIT_TMPDIR=$(mktemp -d)
trap "rm -rf $AUDIT_TMPDIR" EXIT

input_json=$(jq -n --arg cmd "cat foo.go" '{"tool_name":"Bash","tool_input":{"command":$cmd}}')
echo "$input_json" | YEET_HOOK_AUDIT=1 YEET_AUDIT_DIR="$AUDIT_TMPDIR" bash "$HOOK" 2>/dev/null >/dev/null || true
TOTAL=$((TOTAL + 1))
log_line=$(cat "$AUDIT_TMPDIR/hook-audit.log" 2>/dev/null || echo "")
if echo "$log_line" | grep -q "rewrite"; then
  printf "  ${GREEN}PASS${RESET} audit: rewrite logged\n"
  PASS=$((PASS + 1))
else
  printf "  ${RED}FAIL${RESET} audit: rewrite not logged (got: %s)\n" "$log_line"
  FAIL=$((FAIL + 1))
fi

echo ""
echo "============================================"
if [ $FAIL -eq 0 ]; then
  printf "  ${GREEN}ALL $TOTAL TESTS PASSED${RESET}\n"
else
  printf "  ${RED}$FAIL FAILED${RESET} / $TOTAL total ($PASS passed)\n"
fi
echo "============================================"

exit $FAIL
