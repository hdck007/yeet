#!/usr/bin/env bash
# bench-offline.sh — deterministic, zero-cost measurement of what yeet saves.
#
#   bash scripts/bench-offline.sh                    # measure against this repo
#   bash scripts/bench-offline.sh --target ~/my-app  # measure against a real project
#   bash scripts/bench-offline.sh --json out.json --md out.md
#
# For each case it runs the native command, asks `yeet rewrite` what the
# PreToolUse hook would turn it into, runs that too, and compares the number of
# bytes each one puts into the context window. No API calls, no cost, and the
# same numbers every run — this is the benchmark to quote.
#
# Where a command has no rewrite rule, the case supplies the yeet equivalent
# explicitly (third column) so the comparison is still honest.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET="$REPO_ROOT"
YEET_BIN=""
JSON_OUT=""
MD_OUT=""
QUIET=false
# Claude Opus 5 list price, $ per million tokens. Tool output is input tokens.
PRICE_IN=5.00

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  BOLD='\033[1m'; GREEN='\033[32m'; RED='\033[31m'; YELLOW='\033[33m'
  CYAN='\033[36m'; DIM='\033[2m'; RESET='\033[0m'
else
  BOLD=''; GREEN=''; RED=''; YELLOW=''; CYAN=''; DIM=''; RESET=''
fi

usage() {
  cat <<'EOF'
bench-offline.sh — deterministic native-vs-yeet token comparison (no API, no cost)

Usage:
  bash scripts/bench-offline.sh [options]

Options:
  --target <DIR>    Repo to measure against (default: the yeet repo)
  --yeet <PATH>     yeet binary to use (default: build from source, else PATH)
  --price <USD>     $ per million input tokens for the cost column (default: 5.00)
  --json <FILE>     Write machine-readable results
  --md <FILE>       Write a markdown report
  --quiet           Only print the summary
  -h, --help        This message
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --target)  TARGET="$2"; shift ;;
    --target=*) TARGET="${1#*=}" ;;
    --yeet)    YEET_BIN="$2"; shift ;;
    --yeet=*)  YEET_BIN="${1#*=}" ;;
    --price)   PRICE_IN="$2"; shift ;;
    --price=*) PRICE_IN="${1#*=}" ;;
    --json)    JSON_OUT="$2"; shift ;;
    --json=*)  JSON_OUT="${1#*=}" ;;
    --md)      MD_OUT="$2"; shift ;;
    --md=*)    MD_OUT="${1#*=}" ;;
    --quiet)   QUIET=true ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
  shift
done

[ -d "$TARGET" ] || { echo "Target not found: $TARGET" >&2; exit 1; }
TARGET="$(cd "$TARGET" && pwd)"

# ─── Resolve the yeet binary ──────────────────────────────────────────────────
if [ -z "$YEET_BIN" ]; then
  if command -v go >/dev/null 2>&1 && [ -f "$REPO_ROOT/go.mod" ]; then
    $QUIET || echo "Building yeet from source..."
    YEET_BIN="$(mktemp -t yeet-bench-XXXXXX)"
    if ! (cd "$REPO_ROOT" && CGO_ENABLED=1 go build -o "$YEET_BIN" ./cmd/yeet/ 2>&1); then
      rm -f "$YEET_BIN"
      YEET_BIN="$(command -v yeet || echo "")"
    fi
  else
    YEET_BIN="$(command -v yeet || echo "")"
  fi
fi
[ -n "$YEET_BIN" ] && [ -x "$YEET_BIN" ] || { echo "No yeet binary. Pass --yeet <path>." >&2; exit 1; }

# ─── Measurement ──────────────────────────────────────────────────────────────
# Tokens: 4 chars/token, the same heuristic yeet's own estimator uses
# (internal/token/estimator.go), so these numbers line up with `yeet stats`.
est_tokens() { echo $(( ($1 + 3) / 4 )); }

# Run a command in the target dir; prints "<bytes> <exit-status>".
# A command that fails produces an error message, and counting that as a
# "saving" would be a lie — every caller must check the status.
measure() {
  local cmd="$1" out rc=0
  out="$(cd "$TARGET" && eval "$cmd" 2>&1)" || rc=$?
  printf '%s %s' "$(printf '%s' "$out" | wc -c | tr -d ' ')" "$rc"
}

TOTAL_NATIVE=0
TOTAL_YEET=0
CASES_RUN=0
CASES_WON=0
CASES_LOST=0
CASES_SKIPPED=0
CASES_BROKEN=0
BROKEN_ROWS=""
ROWS=""        # label|native_cmd|yeet_cmd|native_bytes|yeet_bytes|pct
JSON_ROWS=""

pct_saved() {
  local n=$1 y=$2
  [ "$n" -le 0 ] && { echo "0"; return; }
  echo $(( (n - y) * 100 / n ))
}

run_case() {
  local label="$1" native="$2" yeet="$3"

  # No explicit yeet form? Ask the hook's own rewriter what it would run.
  if [ -z "$yeet" ]; then
    local rewritten rc=0
    rewritten="$("$YEET_BIN" rewrite "$native" 2>/dev/null)" || rc=$?
    if [ "$rc" -ne 0 ] || [ -z "$rewritten" ] || [ "$rewritten" = "$native" ]; then
      CASES_SKIPPED=$((CASES_SKIPPED+1))
      $QUIET || printf "  ${DIM}%-38s  %-9s  no rewrite rule — skipped${RESET}\n" "$label" "-"
      return
    fi
    yeet="$rewritten"
  fi
  # Route the yeet command at the binary under test, not whatever is on PATH.
  local yeet_run="${yeet/#yeet /$YEET_BIN }"

  local nb nrc yb yrc p
  set -- $(measure "$native");   nb="$1"; nrc="$2"
  set -- $(measure "$yeet_run"); yb="$1"; yrc="$2"

  # A yeet command that errors out is not a saving — it is a regression that
  # costs the agent a failed turn. Report it, never count it.
  if [ "$yrc" -ne 0 ]; then
    CASES_BROKEN=$((CASES_BROKEN+1))
    BROKEN_ROWS="$BROKEN_ROWS$label|$native|$yeet
"
    $QUIET || printf "  %-38s  %8s → ${RED}%-8s  FAILED (exit %s)${RESET}\n" \
      "$label" "$nb" "err" "$yrc"
    return
  fi
  if [ "$nrc" -ne 0 ] && [ "$nb" -eq 0 ]; then
    CASES_SKIPPED=$((CASES_SKIPPED+1))
    $QUIET || printf "  ${DIM}%-38s  %-9s  native command unavailable — skipped${RESET}\n" "$label" "-"
    return
  fi

  p="$(pct_saved "$nb" "$yb")"

  TOTAL_NATIVE=$((TOTAL_NATIVE + nb))
  TOTAL_YEET=$((TOTAL_YEET + yb))
  CASES_RUN=$((CASES_RUN + 1))
  if [ "$yb" -lt "$nb" ]; then CASES_WON=$((CASES_WON+1))
  elif [ "$yb" -gt "$nb" ]; then CASES_LOST=$((CASES_LOST+1)); fi

  ROWS="$ROWS$label|$native|$yeet|$nb|$yb|$p
"
  JSON_ROWS="$JSON_ROWS$(printf '{"label":%s,"native":%s,"yeet":%s,"native_bytes":%s,"yeet_bytes":%s,"native_tokens":%s,"yeet_tokens":%s,"pct_saved":%s}' \
    "$(printf '%s' "$label"  | jq -R .)" \
    "$(printf '%s' "$native" | jq -R .)" \
    "$(printf '%s' "$yeet"   | jq -R .)" \
    "$nb" "$yb" "$(est_tokens "$nb")" "$(est_tokens "$yb")" "$p"),"

  if ! $QUIET; then
    local color="$GREEN" mark="−"
    if [ "$yb" -ge "$nb" ]; then color="$RED"; mark="+"; p=$(( p * -1 )); fi
    printf "  %-38s  %8s → %-8s  ${color}%s%s%%${RESET}\n" \
      "$label" "$nb" "$yb" "$mark" "$p"
  fi
}

# ─── Cases ────────────────────────────────────────────────────────────────────
# Pick real files from the target so this works on any repo.
pick() {
  # pick <find-args...> — first match, target-relative, or empty
  ( cd "$TARGET" && find . -type f "$@" -not -path './.git/*' 2>/dev/null \
      | LC_ALL=C sort | head -1 | sed 's|^\./||' )
}
biggest() {
  ( cd "$TARGET" && find . -type f "$@" -not -path './.git/*' 2>/dev/null \
      | LC_ALL=C sort | while IFS= read -r f; do
          printf '%s %s\n' "$(wc -c < "$f" | tr -d ' ')" "$f"
        done | sort -rn | head -1 | cut -d' ' -f2- )
}

SRC_FILE="$(biggest -name '*.go' -o -name '*.ts' -o -name '*.js' -o -name '*.py' -o -name '*.rb')"
[ -z "$SRC_FILE" ] && SRC_FILE="$(biggest -name '*.md')"
MD_FILE="$(pick -name 'README.md')"
[ -z "$MD_FILE" ] && MD_FILE="$SRC_FILE"
SRC_DIR="$(dirname "${SRC_FILE:-.}")"
GREP_TERM="function"
case "$SRC_FILE" in
  *.go) GREP_TERM="func " ;;
  *.py) GREP_TERM="def " ;;
  *.rb) GREP_TERM="def " ;;
esac

$QUIET || {
  echo ""
  echo -e "${BOLD}  yeet offline benchmark${RESET}  ${DIM}deterministic · no API calls · no cost${RESET}"
  echo -e "  ${DIM}target: $TARGET${RESET}"
  echo -e "  ${DIM}yeet:   $("$YEET_BIN" version 2>/dev/null || echo '?')${RESET}"
  echo ""
  printf "  %-38s  %8s   %-8s  %s\n" "case" "native" "yeet" "saved"
  printf '  %s\n' "$(printf '─%.0s' $(seq 1 72))"
}

# Cases where the proxy hook does the rewriting (empty third column) —
# these measure the real production path.
run_case "cat source file"            "cat $SRC_FILE"                    ""
run_case "cat README"                 "cat $MD_FILE"                     ""
run_case "cat two files"              "cat $SRC_FILE $MD_FILE"           ""
run_case "ls -la (source dir)"        "ls -la $SRC_DIR"                  ""
run_case "ls -la (repo root)"         "ls -la ."                         ""
run_case "ls -laR (recursive)"        "ls -laR ."                        ""
run_case "grep -rn (common term)"     "grep -rn '$GREP_TERM' ."          ""
run_case "grep -rn (rare term)"       "grep -rn 'TODO' ."                ""
run_case "grep -r (no -n)"            "grep -r '$GREP_TERM' ."           ""
run_case "grep (bare)"                "grep '$GREP_TERM' $SRC_FILE"      ""
run_case "find by extension"          "find . -name '*.md'"              ""

# Cases with no rewrite rule — the yeet equivalent an agent would reach for.
run_case "read file (signatures)"     "cat $SRC_FILE"                    "yeet read $SRC_FILE -l aggressive"
run_case "read file (no comments)"    "cat $SRC_FILE"                    "yeet read $SRC_FILE -l minimal"
run_case "read line range"            "sed -n '1,60p' $SRC_FILE"         "yeet read $SRC_FILE --lines 1-60"
run_case "file overview"              "head -120 $SRC_FILE"              "yeet smart $SRC_FILE"
run_case "repo layout"                "find . -type f -not -path './.git/*'" "yeet tree ."
run_case "line counts"                "wc -l \$(find . -name '*.md')"    "yeet wc \$(find . -name '*.md')"
run_case "compare two files"          "diff $SRC_FILE $MD_FILE"          "yeet diff $SRC_FILE $MD_FILE"

# git — the largest outputs an agent reads. Only meaningful in a git repo.
if [ -d "$TARGET/.git" ]; then
  run_case "git status"               "git status"                       ""
  run_case "git diff (last commit)"   "git diff HEAD~1"                  ""
  run_case "git log -20"              "git log -20"                      ""
  run_case "git show HEAD"            "git show HEAD"                    ""
  run_case "git branch"               "git branch"                       ""
fi

# gh — skipped unless the CLI is installed and authenticated.
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  run_case "gh pr list"               "gh pr list"                       ""
  run_case "gh run list"              "gh run list -L 10"                ""
fi

# ─── Summary ──────────────────────────────────────────────────────────────────
NATIVE_TOK="$(est_tokens "$TOTAL_NATIVE")"
YEET_TOK="$(est_tokens "$TOTAL_YEET")"
SAVED_TOK=$((NATIVE_TOK - YEET_TOK))
SAVED_PCT="$(pct_saved "$TOTAL_NATIVE" "$TOTAL_YEET")"

# Cost per 1000 tool calls in this mix, at list price for input tokens.
cost_of() {
  awk -v t="$1" -v p="$PRICE_IN" -v n="$CASES_RUN" \
    'BEGIN { if (n == 0) { print "0.00"; exit } printf "%.2f", (t / n) * 1000 * p / 1000000 }'
}
COST_NATIVE="$(cost_of "$NATIVE_TOK")"
COST_YEET="$(cost_of "$YEET_TOK")"
COST_SAVED="$(awk -v a="$COST_NATIVE" -v b="$COST_YEET" 'BEGIN{printf "%.2f", a-b}')"

echo ""
printf '  %s\n' "$(printf '─%.0s' $(seq 1 72))"
echo ""
echo -e "${BOLD}  Results${RESET}  ${DIM}($CASES_RUN measured: $CASES_WON smaller with yeet, $CASES_LOST larger; $CASES_BROKEN broken, $CASES_SKIPPED skipped)${RESET}"
echo ""
if [ "$CASES_BROKEN" -gt 0 ]; then
  echo -e "  ${RED}${BOLD}$CASES_BROKEN rewrite(s) produced a command that fails${RESET} ${DIM}— excluded from the totals below.${RESET}"
  echo -e "  ${DIM}These are worse than no saving: the agent burns a turn on an error.${RESET}"
  printf '%s' "$BROKEN_ROWS" | while IFS='|' read -r label native yeet; do
    [ -n "$label" ] || continue
    echo -e "    ${RED}✗${RESET} $label: ${DIM}$native${RESET}  →  ${YELLOW}$yeet${RESET}"
  done
  echo ""
fi
printf "  %-26s %14s %14s\n" "" "native" "yeet"
printf "  %-26s %14s %14s\n" "bytes into context" "$TOTAL_NATIVE" "$TOTAL_YEET"
printf "  %-26s %14s %14s\n" "tokens (est.)" "$NATIVE_TOK" "$YEET_TOK"
printf "  %-26s %14s %14s\n" "\$ per 1k tool calls" "\$$COST_NATIVE" "\$$COST_YEET"
echo ""
if [ "$SAVED_PCT" -gt 0 ]; then
  echo -e "  ${GREEN}${BOLD}$SAVED_PCT% fewer tokens${RESET}  ${DIM}($SAVED_TOK tokens, \$$COST_SAVED per 1k tool calls)${RESET}"
else
  echo -e "  ${YELLOW}${BOLD}No saving in this mix${RESET} ${DIM}($SAVED_PCT%)${RESET}"
fi
echo ""
echo -e "  ${DIM}Tokens are estimated at 4 chars/token (yeet's own estimator).${RESET}"
echo -e "  ${DIM}Cost assumes \$$PRICE_IN per 1M input tokens and this exact case mix.${RESET}"
echo -e "  ${DIM}For end-to-end savings on real work, see scripts/bench-live.sh${RESET}"
echo ""

# ─── Reports ──────────────────────────────────────────────────────────────────
if [ -n "$JSON_OUT" ]; then
  {
    printf '{"benchmark":"offline","target":%s,"yeet_version":%s,' \
      "$(printf '%s' "$TARGET" | jq -R .)" \
      "$(printf '%s' "$("$YEET_BIN" version 2>/dev/null || echo unknown)" | jq -R .)"
    printf '"price_per_mtok_input":%s,"cases_run":%s,"cases_smaller":%s,"cases_larger":%s,"cases_skipped":%s,' \
      "$PRICE_IN" "$CASES_RUN" "$CASES_WON" "$CASES_LOST" "$CASES_SKIPPED"
    printf '"totals":{"native_bytes":%s,"yeet_bytes":%s,"native_tokens":%s,"yeet_tokens":%s,"tokens_saved":%s,"pct_saved":%s},' \
      "$TOTAL_NATIVE" "$TOTAL_YEET" "$NATIVE_TOK" "$YEET_TOK" "$SAVED_TOK" "$SAVED_PCT"
    printf '"cases":[%s]}' "${JSON_ROWS%,}"
  } | jq . > "$JSON_OUT" 2>/dev/null || {
    echo "  (could not write valid JSON to $JSON_OUT)" >&2
  }
  echo "  JSON → $JSON_OUT"
fi

if [ -n "$MD_OUT" ]; then
  {
    echo "# yeet offline benchmark"
    echo ""
    echo "Deterministic comparison of bytes entering the context window, native"
    echo "shell commands vs the \`yeet\` equivalent the PreToolUse hook rewrites them to."
    echo "No API calls — reproducible on any machine."
    echo ""
    echo "- Target: \`$TARGET\`"
    echo "- yeet: \`$("$YEET_BIN" version 2>/dev/null || echo unknown)\`"
    echo "- Tokens estimated at 4 chars/token"
    echo ""
    echo "## Totals"
    echo ""
    echo "| | native | yeet | saved |"
    echo "|---|---:|---:|---:|"
    echo "| Bytes | $TOTAL_NATIVE | $TOTAL_YEET | ${SAVED_PCT}% |"
    echo "| Tokens (est.) | $NATIVE_TOK | $YEET_TOK | $SAVED_TOK |"
    echo "| \$ per 1k tool calls | \$$COST_NATIVE | \$$COST_YEET | \$$COST_SAVED |"
    echo ""
    echo "## Per case"
    echo ""
    echo "| Case | Native command | yeet command | Native | yeet | Saved |"
    echo "|---|---|---|---:|---:|---:|"
    printf '%s' "$ROWS" | while IFS='|' read -r label native yeet nb yb p; do
      [ -n "$label" ] || continue
      echo "| $label | \`$native\` | \`$yeet\` | $nb | $yb | ${p}% |"
    done
    echo ""
    echo "_$CASES_RUN cases measured; $CASES_WON smaller with yeet, $CASES_LOST larger, $CASES_SKIPPED skipped._"
  } > "$MD_OUT"
  echo "  Markdown → $MD_OUT"
fi

case "$YEET_BIN" in */yeet-bench-*) rm -f "$YEET_BIN" ;; esac
exit 0
