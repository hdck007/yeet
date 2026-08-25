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
# yeet grep is always recursive and always numbers lines, so -r and -n are
# implied; forwarding them makes its flag parser reject the command.
test_rewrite "grep with flags"  "grep -rn foo src/"      "yeet grep foo src/"
test_rewrite "ls with path"     "ls src/"                "yeet ls src/"
# Native find is `find <path> -name <pattern>`; yeet find is
# `yeet find <pattern> [path]`, so the operands are swapped.
test_rewrite "find pattern"     "find . -name '*.go'"    "yeet find '*.go' ."
test_rewrite "diff two files"   "diff a.go b.go"         "yeet diff a.go b.go"

echo ""
echo "--- Env var prefix handling ---"
test_rewrite "env + cat"        "DEBUG=1 cat foo.go"     "DEBUG=1 yeet read foo.go"
test_rewrite "env + grep"       "CI=1 grep foo ."        "CI=1 yeet grep foo ."

echo ""
echo "--- Chained commands (each segment rewritten on its own) ---"
test_rewrite "semicolon chain"   "cat a.ts; cat b.ts"          "yeet read a.ts; yeet read b.ts"
test_rewrite "and chain"         "cat pkg.json && ls src"      "yeet read pkg.json && yeet ls src"
test_rewrite "or chain"          "cat a.ts || ls"              "yeet read a.ts || yeet ls"
test_rewrite "pipe to head"      "grep -rn foo src | head -20" "yeet grep foo src | head -20"
test_rewrite "three segments"    "ls && cat pkg.json && git status" \
                                 "yeet ls && yeet read pkg.json && yeet git status"
test_rewrite "spacing preserved" "cat a.ts   &&   ls   src"    "yeet read a.ts   &&   yeet ls   src"
test_rewrite "stderr redirect"   "grep foo src 2>/dev/null"    "yeet grep foo src 2>/dev/null"
test_rewrite "partial chain"     "cat a.ts b.ts && ls src"     "cat a.ts b.ts && yeet ls src"

echo ""
echo "--- Chains that must NOT be rewritten ---"
test_rewrite "pipe into jq"      "cat data.json | jq .name"    ""
test_rewrite "pipe into wc"      "ls | wc -l"                  ""
test_rewrite "grep as consumer"  "git log | grep fix"          ""
test_rewrite "stdout to file"    "cat a.ts > b.ts"             ""
test_rewrite "append to file"    "cat a.ts >> log"             ""
test_rewrite "cmd substitution"  "echo \$(cat version.txt)"     ""
test_rewrite "backgrounded"      "cat a.ts &"                  ""

echo ""
echo "--- Verb families ---"
test_rewrite "ps aux"            "ps aux"                      "yeet ps aux"
test_rewrite "du"                "du -sh node_modules"         "yeet du -sh node_modules"
test_rewrite "kubectl get"       "kubectl get pods -A"         "yeet kubectl get pods -A"
test_rewrite "docker ps"         "docker ps -a"                "yeet docker ps -a"
test_rewrite "npm query"         "npm outdated"                "yeet npm outdated"

echo ""
echo "--- Read-only boundary: mutations reach the real binary ---"
test_rewrite "kubectl apply"     "kubectl apply -f d.yaml"     ""
test_rewrite "kubectl delete"    "kubectl delete pod api-0"    ""
test_rewrite "kubectl exec"      "kubectl exec -it api-0 -- sh" ""
test_rewrite "docker run"        "docker run -it ubuntu"       ""
test_rewrite "docker rm"         "docker rm -f web"            ""
test_rewrite "docker compose up" "docker compose up -d"        ""

echo ""
echo "--- Should NOT rewrite ---"
# A bare read of a code file climbs the reading ladder to signatures-only in
# the same turn. Other yeet commands are left alone.
test_rewrite "read ladder"      "yeet read foo.go"       "yeet read foo.go -l aggressive"
test_rewrite "already yeet"     "yeet grep foo ."        ""
test_rewrite "read non-code"    "yeet read README.md"    ""
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
