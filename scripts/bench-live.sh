#!/usr/bin/env bash
# bench-live.sh — end-to-end A/B: the same task, with and without yeet.
#
#   bash scripts/bench-live.sh                          # 2 reps per arm, this repo
#   bash scripts/bench-live.sh --reps 3 --target ~/app
#   bash scripts/bench-live.sh --task-file ./my-task.txt
#
# What makes this trustworthy:
#   * Your real ~/.claude is never touched. Each arm gets its own throwaway
#     CLAUDE_CONFIG_DIR, and --setting-sources user keeps project/local
#     settings from leaking in. The yeet arm is built by running install.sh
#     against that directory, so it tests the real install.
#   * Runs alternate (yeet, native, native, yeet, ...) so prompt-cache warmth
#     and API weather hit both arms evenly.
#   * Tokens come from the API's own usage numbers, not from guesswork.
#   * Every run is checked for arm purity (no yeet in the native arm, yeet
#     actually used in the yeet arm) and for task completion. A run that
#     failed either check is reported and excluded — a cheap run that did
#     less work is not a saving.
#
# Costs real money: it makes 2 x reps Claude Code sessions.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET="$REPO_ROOT"
REPS=2
MAX_TURNS=30
MODEL=""
TASK_FILE=""
OUT_DIR="$REPO_ROOT/scripts/bench-results"
KEEP=false
YES=false
# Claude Opus 5 list price, $ per million tokens.
PRICE_IN=5.00
PRICE_OUT=25.00

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  BOLD='\033[1m'; GREEN='\033[32m'; RED='\033[31m'; YELLOW='\033[33m'
  CYAN='\033[36m'; DIM='\033[2m'; RESET='\033[0m'
else
  BOLD=''; GREEN=''; RED=''; YELLOW=''; CYAN=''; DIM=''; RESET=''
fi
say()  { echo -e "$*"; }
info() { echo -e "  ${CYAN}→${RESET} $*"; }
ok()   { echo -e "  ${GREEN}✓${RESET} $*"; }
warn() { echo -e "  ${YELLOW}!${RESET} $*" >&2; }
die()  { echo -e "  ${RED}✗${RESET} $*" >&2; exit 1; }

usage() {
  cat <<'EOF'
bench-live.sh — measure what yeet saves on a real Claude Code task

Usage:
  bash scripts/bench-live.sh [options]

Options:
  --reps <N>         Runs per arm (default: 2). Total sessions = 2 x N
  --target <DIR>     Repo the task works on (default: the yeet repo)
  --task-file <F>    File containing the task prompt (default: built-in task)
  --max-turns <N>    Cap per session (default: 30)
  --model <NAME>     Model to pass to claude (default: your configured model)
  --out <DIR>        Where to write reports (default: scripts/bench-results)
  --price-in <USD>   $ per 1M input tokens  (default: 5.00)
  --price-out <USD>  $ per 1M output tokens (default: 25.00)
  --keep             Keep the throwaway config dirs for inspection
  -y, --yes          Skip the cost confirmation
  -h, --help         This message

Your ~/.claude is never read or modified.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --reps)      REPS="$2"; shift ;;
    --reps=*)    REPS="${1#*=}" ;;
    --target)    TARGET="$2"; shift ;;
    --target=*)  TARGET="${1#*=}" ;;
    --task-file) TASK_FILE="$2"; shift ;;
    --task-file=*) TASK_FILE="${1#*=}" ;;
    --max-turns) MAX_TURNS="$2"; shift ;;
    --max-turns=*) MAX_TURNS="${1#*=}" ;;
    --model)     MODEL="$2"; shift ;;
    --model=*)   MODEL="${1#*=}" ;;
    --out)       OUT_DIR="$2"; shift ;;
    --out=*)     OUT_DIR="${1#*=}" ;;
    --price-in)  PRICE_IN="$2"; shift ;;
    --price-out) PRICE_OUT="$2"; shift ;;
    --keep)      KEEP=true ;;
    -y|--yes)    YES=true ;;
    -h|--help)   usage; exit 0 ;;
    *) die "Unknown option: $1  (--help for usage)" ;;
  esac
  shift
done

command -v claude >/dev/null 2>&1 || die "claude CLI not found on PATH"
command -v jq >/dev/null 2>&1     || die "jq is required"
command -v yeet >/dev/null 2>&1   || die "yeet must be installed for the yeet arm"
[ -d "$TARGET" ] || die "Target not found: $TARGET"
TARGET="$(cd "$TARGET" && pwd)"
case "$REPS" in ''|*[!0-9]*) die "--reps must be a number" ;; esac
[ "$REPS" -ge 1 ] || die "--reps must be at least 1"

STAMP="$(date +%Y%m%d-%H%M%S)"
RUN_DIR="$OUT_DIR/live-$STAMP"
mkdir -p "$RUN_DIR"

# ─── The task ─────────────────────────────────────────────────────────────────
# Deliberately shaped like real work: explore, search, read, then report. It
# must be answerable from the repo alone, and its answer must be checkable so
# a run that quietly did less work cannot masquerade as a saving.
if [ -n "$TASK_FILE" ]; then
  [ -f "$TASK_FILE" ] || die "Task file not found: $TASK_FILE"
  TASK="$(cat "$TASK_FILE")"
  COMPLETION_RE="${YEET_BENCH_COMPLETION_RE:-.}"
else
  TASK="Work in $TARGET. I need a map of this codebase's command surface.

First get your bearings: list the top-level directories, then find where
commands or entry points are defined. Search for the registration pattern
across the whole repo rather than guessing from filenames.

For every command you find, read enough of its definition to capture: the
command name, the file and line where it is registered, and its one-line
description or purpose. Do not read whole files when you only need a few lines.

Then check how these commands are tested: search the repo for test files that
reference them, and note which commands have no test coverage at all.

Finish with: (1) a table of every command with its file:line and purpose,
(2) the total count on a line of its own formatted exactly as
TOTAL_COMMANDS: <number>, and (3) one sentence naming the commands that have
no tests."
  COMPLETION_RE="TOTAL_COMMANDS:[[:space:]]*[0-9]+"
fi
printf '%s\n' "$TASK" > "$RUN_DIR/task.txt"

# ─── Arm config dirs ──────────────────────────────────────────────────────────
# Two throwaway CLAUDE_CONFIG_DIRs. The yeet arm is produced by the real
# installer so this measures the shipped setup, not a hand-rolled imitation.
ARM_YEET="$RUN_DIR/config-yeet"
ARM_NATIVE="$RUN_DIR/config-native"
DATA_YEET="$RUN_DIR/data-yeet"
mkdir -p "$ARM_YEET" "$ARM_NATIVE" "$DATA_YEET"

info "Building the yeet arm with install.sh (isolated, no sudo, no binary install)..."
if ! YEET_CLAUDE_HOME="$ARM_YEET" \
     YEET_DATA_DIR="$DATA_YEET" \
     YEET_ASSET_DIR="$REPO_ROOT" \
     YEET_SKIP_BINARY=1 \
     YEET_NO_SUDO=1 \
     NO_COLOR=1 \
     bash "$REPO_ROOT/install.sh" --yes --claude --auto-allow > "$RUN_DIR/install-yeet-arm.log" 2>&1; then
  tail -20 "$RUN_DIR/install-yeet-arm.log" >&2
  die "Could not build the yeet arm — see $RUN_DIR/install-yeet-arm.log"
fi
ok "yeet arm: $(jq '[.hooks.PreToolUse[] | select(._yeet==true)] | length' "$ARM_YEET/settings.json") hooks, awareness loaded"

# The native arm gets a valid but empty config: no hooks, no CLAUDE.md.
echo '{}' > "$ARM_NATIVE/settings.json"
ok "native arm: no hooks, no awareness"

# ─── Runner ───────────────────────────────────────────────────────────────────
run_arm() {
  # run_arm <arm> <rep> ; writes <stream>.jsonl, echoes "tokens_in tokens_out cache_w cache_r turns cost secs"
  local arm="$1" rep="$2"
  local cfg data stream start end
  if [ "$arm" = "yeet" ]; then cfg="$ARM_YEET"; data="$DATA_YEET"; else cfg="$ARM_NATIVE"; data="$RUN_DIR/data-native"; fi
  mkdir -p "$data"
  stream="$RUN_DIR/$arm-rep$rep.jsonl"

  local -a args
  args=(--print --output-format stream-json --verbose
        --max-turns "$MAX_TURNS"
        --dangerously-skip-permissions
        --setting-sources user
        --add-dir "$TARGET")
  [ -n "$MODEL" ] && args+=(--model "$MODEL")

  start="$(date +%s)"
  printf '%s' "$TASK" | env \
    CLAUDE_CONFIG_DIR="$cfg" \
    YEET_DATA_DIR="$data" \
    claude "${args[@]}" > "$stream" 2>"$RUN_DIR/$arm-rep$rep.err" || true
  end="$(date +%s)"

  # The result event carries the authoritative usage totals.
  local u
  u="$(jq -s '
    ( [ .[] | select(.type=="result") ] | last ) as $r
    | ($r.usage // {}) as $usage
    | {
        input:  ($usage.input_tokens // 0),
        output: ($usage.output_tokens // 0),
        cache_w: ($usage.cache_creation_input_tokens // 0),
        cache_r: ($usage.cache_read_input_tokens // 0),
        turns:  ($r.num_turns // 0),
        cost:   ($r.total_cost_usd // 0)
      }
    | "\(.input) \(.output) \(.cache_w) \(.cache_r) \(.turns) \(.cost)"
  ' "$stream" 2>/dev/null || echo "0 0 0 0 0 0")"
  printf '%s %s' "$u" "$((end - start))"
}

# Bash commands the assistant actually issued.
bash_cmds() {
  jq -r 'select(.type=="assistant") | .message.content[]?
         | select(.type=="tool_use") | select(.name=="Bash") | .input.command' "$1" 2>/dev/null
}
# Names of native tools used (Read/Grep/Glob/Edit/Write).
native_tools() {
  jq -r 'select(.type=="assistant") | .message.content[]?
         | select(.type=="tool_use")
         | select(.name | test("^(Read|Grep|Glob|Edit|Write|MultiEdit)$")) | .name' "$1" 2>/dev/null
}
final_text() { jq -r 'select(.type=="result") | .result // empty' "$1" 2>/dev/null; }
tool_result_bytes() {
  jq -r 'select(.type=="user") | .message.content[]?
         | select(.type=="tool_result") | (.content | tostring | length)' "$1" 2>/dev/null \
    | awk '{s+=$1} END {print s+0}'
}

# yeet used as a command (not merely mentioned in a path or message).
uses_yeet() { bash_cmds "$1" | grep -qE '(^|\| *)yeet ' ; }

# ─── Confirm ──────────────────────────────────────────────────────────────────
TOTAL_RUNS=$((REPS * 2))
say ""
say "${BOLD}  yeet live benchmark${RESET}"
say "  ${DIM}target:   $TARGET${RESET}"
say "  ${DIM}runs:     $TOTAL_RUNS ($REPS per arm, alternating order)${RESET}"
say "  ${DIM}reports:  $RUN_DIR${RESET}"
say "  ${DIM}your ~/.claude is not touched${RESET}"
say ""
if ! $YES; then
  say "  ${YELLOW}This runs $TOTAL_RUNS real Claude Code sessions and will cost money.${RESET}"
  printf "  Proceed? [y/N]: "
  REPLY=""
  if [ -t 0 ]; then read -r REPLY; elif [ -e /dev/tty ]; then read -r REPLY </dev/tty; fi
  case "${REPLY:-N}" in [Yy]|[Yy][Ee][Ss]) ;; *) say "  Aborted."; exit 0 ;; esac
fi

# ─── Execute (alternating so cache warmth is shared) ──────────────────────────
say ""
ROWS=""
for rep in $(seq 1 "$REPS"); do
  if [ $((rep % 2)) -eq 1 ]; then ORDER="yeet native"; else ORDER="native yeet"; fi
  for arm in $ORDER; do
    printf "  ${CYAN}→${RESET} rep %s/%s  %-7s " "$rep" "$REPS" "$arm"
    RES="$(run_arm "$arm" "$rep")"
    set -- $RES
    IN="$1"; OUT="$2"; CW="$3"; CR="$4"; TURNS="$5"; COST="$6"; SECS="$7"
    STREAM="$RUN_DIR/$arm-rep$rep.jsonl"
    TR_BYTES="$(tool_result_bytes "$STREAM")"

    # Purity + completion. A run that fails either is not comparable.
    STATUS="ok"; NOTE=""
    if [ "$arm" = "native" ] && uses_yeet "$STREAM"; then
      STATUS="impure"; NOTE="yeet was used in the native arm"
    elif [ "$arm" = "yeet" ] && ! uses_yeet "$STREAM"; then
      STATUS="impure"; NOTE="yeet was never used in the yeet arm"
    elif ! final_text "$STREAM" | grep -qE "$COMPLETION_RE"; then
      STATUS="incomplete"; NOTE="answer did not match /$COMPLETION_RE/"
    elif [ "$((IN + CW + CR))" -eq 0 ]; then
      STATUS="nodata"; NOTE="no usage reported (session may have failed)"
    fi

    BILLED=$((IN + CW + CR))
    if [ "$STATUS" = "ok" ]; then
      printf "${GREEN}ok${RESET}   in=%-8s cache=%-9s out=%-6s turns=%-3s %ss\n" \
        "$IN" "$((CW + CR))" "$OUT" "$TURNS" "$SECS"
    else
      printf "${RED}%s${RESET}  %s\n" "$STATUS" "$NOTE"
    fi
    ROWS="$ROWS$arm|$rep|$STATUS|$IN|$OUT|$CW|$CR|$BILLED|$TURNS|$COST|$SECS|$TR_BYTES|$NOTE
"
  done
done

# ─── Aggregate ────────────────────────────────────────────────────────────────
# Median is the headline (robust to one weird run); mean is reported alongside.
stat_for() {
  # stat_for <arm> <field-index> ; echoes "n median mean"
  local arm="$1" idx="$2"
  printf '%s' "$ROWS" | awk -F'|' -v a="$arm" -v i="$idx" '
    $1 == a && $3 == "ok" { v[n++] = $i + 0; s += $i }
    END {
      if (n == 0) { print "0 0 0"; exit }
      # insertion sort — n is tiny
      for (x = 1; x < n; x++) { k = v[x]; y = x - 1; while (y >= 0 && v[y] > k) { v[y+1] = v[y]; y-- } v[y+1] = k }
      med = (n % 2) ? v[int(n/2)] : (v[n/2 - 1] + v[n/2]) / 2
      printf "%d %.0f %.0f", n, med, s / n
    }'
}
sum_cost() {
  printf '%s' "$ROWS" | awk -F'|' -v a="$1" '$1==a && $3=="ok" { s += $10; n++ } END { if(n==0){print "0"}else{printf "%.4f", s/n} }'
}

set -- $(stat_for yeet 8);   YN="$1"; Y_BILLED_MED="$2"; Y_BILLED_MEAN="$3"
set -- $(stat_for native 8); NN="$1"; N_BILLED_MED="$2"; N_BILLED_MEAN="$3"
set -- $(stat_for yeet 5);   Y_OUT_MED="$2"
set -- $(stat_for native 5); N_OUT_MED="$2"
set -- $(stat_for yeet 12);  Y_TR_MED="$2"
set -- $(stat_for native 12); N_TR_MED="$2"
set -- $(stat_for yeet 9);   Y_TURNS_MED="$2"
set -- $(stat_for native 9); N_TURNS_MED="$2"
set -- $(stat_for yeet 11);  Y_SECS_MED="$2"
set -- $(stat_for native 11); N_SECS_MED="$2"
Y_COST="$(sum_cost yeet)"
N_COST="$(sum_cost native)"

pct() { awk -v n="$1" -v y="$2" 'BEGIN { if (n <= 0) { print "n/a"; exit } printf "%.1f", (n - y) * 100 / n }'; }
SAVED_PCT="$(pct "$N_BILLED_MED" "$Y_BILLED_MED")"
TR_PCT="$(pct "$N_TR_MED" "$Y_TR_MED")"
COST_PCT="$(pct "$N_COST" "$Y_COST")"

say ""
printf '  %s\n' "$(printf '─%.0s' $(seq 1 70))"
say ""
if [ "$YN" -eq 0 ] || [ "$NN" -eq 0 ]; then
  warn "Not enough valid runs to compare (yeet: $YN, native: $NN)."
  warn "Check $RUN_DIR for the streams and the notes column in the report."
else
  say "${BOLD}  Results${RESET}  ${DIM}(medians over $YN valid yeet / $NN valid native runs)${RESET}"
  say ""
  printf "  %-30s %13s %13s %9s\n" "" "native" "yeet" "saved"
  printf "  %-30s %13s %13s %8s%%\n" "billed input tokens" "$N_BILLED_MED" "$Y_BILLED_MED" "$SAVED_PCT"
  printf "  %-30s %13s %13s %8s%%\n" "tool output (bytes)" "$N_TR_MED" "$Y_TR_MED" "$TR_PCT"
  printf "  %-30s %13s %13s %9s\n" "output tokens" "$N_OUT_MED" "$Y_OUT_MED" "-"
  printf "  %-30s %13s %13s %9s\n" "turns" "$N_TURNS_MED" "$Y_TURNS_MED" "-"
  printf "  %-30s %13s %13s %9s\n" "wall clock (s)" "$N_SECS_MED" "$Y_SECS_MED" "-"
  printf "  %-30s %13s %13s %8s%%\n" "cost per run (\$, reported)" "$N_COST" "$Y_COST" "$COST_PCT"
  say ""
  if [ "${SAVED_PCT%%.*}" -gt 0 ] 2>/dev/null; then
    say "  ${GREEN}${BOLD}yeet used $SAVED_PCT% fewer input tokens${RESET} on this task."
  else
    say "  ${YELLOW}${BOLD}No input-token saving on this task ($SAVED_PCT%).${RESET}"
  fi
  say ""
  say "  ${DIM}Billed input = input + cache-creation + cache-read tokens.${RESET}"
  say "  ${DIM}Cost is what the API reported per run, averaged.${RESET}"
fi

INVALID="$(printf '%s' "$ROWS" | awk -F'|' '$3 != "ok" && NF > 3' | wc -l | tr -d ' ')"
if [ "${INVALID:-0}" -gt 0 ]; then
  say ""
  warn "$INVALID run(s) excluded (impure, incomplete, or no data) — details in the report."
fi

# ─── Reports ──────────────────────────────────────────────────────────────────
MD="$RUN_DIR/report.md"
{
  echo "# yeet live benchmark — $STAMP"
  echo ""
  echo "- Target: \`$TARGET\`"
  echo "- yeet: \`$(yeet version 2>/dev/null || echo unknown)\`"
  echo "- Reps per arm: $REPS (alternating order)"
  echo "- Max turns: $MAX_TURNS"
  echo "- Isolation: per-arm \`CLAUDE_CONFIG_DIR\`, \`--setting-sources user\`; the real \`~/.claude\` untouched"
  echo "- yeet arm built by running \`install.sh\` against the throwaway config dir"
  echo ""
  echo "## Headline (medians over valid runs)"
  echo ""
  echo "| Metric | native | yeet | saved |"
  echo "|---|---:|---:|---:|"
  echo "| Billed input tokens | $N_BILLED_MED | $Y_BILLED_MED | ${SAVED_PCT}% |"
  echo "| Tool output (bytes) | $N_TR_MED | $Y_TR_MED | ${TR_PCT}% |"
  echo "| Output tokens | $N_OUT_MED | $Y_OUT_MED | – |"
  echo "| Turns | $N_TURNS_MED | $Y_TURNS_MED | – |"
  echo "| Wall clock (s) | $N_SECS_MED | $Y_SECS_MED | – |"
  echo "| Cost per run (\$) | $N_COST | $Y_COST | ${COST_PCT}% |"
  echo ""
  echo "Means (billed input): native $N_BILLED_MEAN, yeet $Y_BILLED_MEAN."
  echo ""
  echo "## Every run"
  echo ""
  echo "| Arm | Rep | Status | Input | Cache write | Cache read | Billed in | Output | Turns | Cost | Secs | Tool bytes | Note |"
  echo "|---|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|"
  printf '%s' "$ROWS" | while IFS='|' read -r arm rep status in out cw cr billed turns cost secs tr note; do
    [ -n "$arm" ] || continue
    echo "| $arm | $rep | $status | $in | $cw | $cr | $billed | $out | $turns | $cost | $secs | $tr | $note |"
  done
  echo ""
  echo "## Validity checks"
  echo ""
  echo "Each run must pass all of these or it is excluded:"
  echo ""
  echo "- **Arm purity**: the native arm issued no \`yeet\` command; the yeet arm issued at least one."
  echo "- **Task completion**: the final answer matched \`$COMPLETION_RE\`."
  echo "- **Usage present**: the API reported non-zero token usage."
  echo ""
  echo "## Task"
  echo ""
  echo '```'
  cat "$RUN_DIR/task.txt"
  echo '```'
} > "$MD"

JSON="$RUN_DIR/report.json"
{
  printf '{"benchmark":"live","stamp":%s,"target":%s,"reps":%s,"max_turns":%s,' \
    "$(printf '%s' "$STAMP" | jq -R .)" "$(printf '%s' "$TARGET" | jq -R .)" "$REPS" "$MAX_TURNS"
  printf '"medians":{"native_billed_input":%s,"yeet_billed_input":%s,"pct_saved":%s,' \
    "$N_BILLED_MED" "$Y_BILLED_MED" "$(printf '%s' "$SAVED_PCT" | sed 's/n\/a/null/')"
  printf '"native_tool_bytes":%s,"yeet_tool_bytes":%s,"native_turns":%s,"yeet_turns":%s},' \
    "$N_TR_MED" "$Y_TR_MED" "$N_TURNS_MED" "$Y_TURNS_MED"
  printf '"cost_per_run":{"native":%s,"yeet":%s},"runs":[' "$N_COST" "$Y_COST"
  FIRST=1
  printf '%s' "$ROWS" | while IFS='|' read -r arm rep status in out cw cr billed turns cost secs tr note; do
    [ -n "$arm" ] || continue
    [ "$FIRST" -eq 1 ] || printf ','
    FIRST=0
    printf '{"arm":"%s","rep":%s,"status":"%s","input":%s,"output":%s,"cache_write":%s,"cache_read":%s,"billed_input":%s,"turns":%s,"cost":%s,"seconds":%s,"tool_bytes":%s,"note":%s}' \
      "$arm" "$rep" "$status" "$in" "$out" "$cw" "$cr" "$billed" "$turns" "$cost" "$secs" "$tr" \
      "$(printf '%s' "$note" | jq -R .)"
  done
  printf ']}'
} | jq . > "$JSON" 2>/dev/null || rm -f "$JSON"

say ""
ok "Markdown → $MD"
[ -f "$JSON" ] && ok "JSON     → $JSON"
ok "Streams  → $RUN_DIR/*.jsonl"

if ! $KEEP; then
  rm -rf "$ARM_YEET" "$ARM_NATIVE" "$DATA_YEET" "$RUN_DIR/data-native"
else
  info "Kept config dirs: $ARM_YEET, $ARM_NATIVE"
fi
say ""
