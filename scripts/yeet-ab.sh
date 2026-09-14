#!/usr/bin/env bash
# yeet-ab.sh — run real Claude Code sessions with and without yeet and keep every
# measurement, so a week of ordinary work decides whether yeet pays off.
#
#   yeet-ab shell-init          print the shell snippet that gives you `claude --yeet`
#   yeet-ab start [--arm A] ..  launch a session directly (args pass through to claude)
#   yeet-ab report [--since 7d] aggregate what has been collected
#   yeet-ab export [--csv]      dump the datalake for outside analysis
#   yeet-ab status              what is recorded so far
#
# Interface: with the shell snippet installed, `claude --yeet` runs the yeet arm
# and a plain `claude` runs without it. Both are recorded.
#
# Why per-turn is the headline: session totals mostly track how long a task ran.
# The synthetic benchmark (docs/benchmark-live-ab.md) found yeet is worth ~0.8%
# per turn on search/read/edit work — tool output is only ~4% of billed context
# and yeet condenses it ~20%. Too small to see at n=3. Real sessions vary the
# workload, which is the variable that actually matters.
#
# Nothing modifies your real config. Arms are expressed with --settings, so auth
# keeps working: CLAUDE_CONFIG_DIR is never overridden.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
AB_HOME="${YEET_AB_HOME:-$HOME/.local/share/yeet/ab}"
LEDGER="$AB_HOME/ledger.jsonl"
LAKE="$AB_HOME/sessions"
mkdir -p "$AB_HOME" "$LAKE"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  BOLD='\033[1m'; GREEN='\033[32m'; YELLOW='\033[33m'; CYAN='\033[36m'; DIM='\033[2m'; RESET='\033[0m'
else BOLD=''; GREEN=''; YELLOW=''; CYAN=''; DIM=''; RESET=''; fi
say() { echo -e "$*"; }
die() { echo -e "  ${YELLOW}x${RESET} $*" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || die "jq is required"

# ─── arm construction ─────────────────────────────────────────────────────────
# Hooks are taken from the repo if the user has no global install, so the
# experiment never depends on install.sh having been run.
resolve_hook() {
  local name="$1"
  for c in "$HOME/.claude/hooks/$name" "$REPO_ROOT/hooks/$name"; do
    [ -f "$c" ] && { echo "$c"; return 0; }
  done
  return 1
}

build_arm() {
  local arm="$1" f="$AB_HOME/settings-$arm.json" rec="$AB_HOME/record.sh"
  cat > "$rec" <<'REC'
#!/usr/bin/env bash
IN=$(cat)
DIR="${YEET_AB_HOME:-$HOME/.local/share/yeet/ab}"
mkdir -p "$DIR"
printf '%s' "$IN" | jq -c \
  --arg arm "${YEET_AB_ARM:-unknown}" \
  --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg yv "${YEET_AB_YEETVER:-}" \
  '{arm:$arm, ts:$ts, yeet_version:$yv, session_id:.session_id,
    transcript:.transcript_path, cwd:.cwd, source:.source}' >> "$DIR/ledger.jsonl"
echo '{}'
REC
  chmod +x "$rec"
  local ss; ss="$(jq -n --arg c "bash \"$rec\"" \
    '{SessionStart:[{matcher:"startup",hooks:[{type:"command",command:$c}]}]}')"

  if [ "$arm" = "yeet" ]; then
    local proxy icept
    proxy="$(resolve_hook yeet-proxy.sh)"     || die "cannot find yeet-proxy.sh (repo or ~/.claude/hooks)"
    icept="$(resolve_hook yeet-intercept.sh)" || die "cannot find yeet-intercept.sh (repo or ~/.claude/hooks)"
    jq -n --argjson ss "$ss" --arg p "bash \"$proxy\"" --arg i "bash \"$icept\"" '
      {hooks: ($ss + {PreToolUse: [
        {matcher:"Grep", hooks:[{type:"command",command:$i}]},
        {matcher:"Glob", hooks:[{type:"command",command:$i}]},
        {matcher:"Bash", hooks:[{type:"command",command:$p}]}
      ]})}' > "$f"
  else
    jq -n --argjson ss "$ss" '{hooks: $ss}' > "$f"
  fi
  echo "$f"
}

cmd_shell_init() {
  cat <<'SNIP'
# ── yeet A/B ──────────────────────────────────────────────────────────────────
# `claude --yeet` runs with yeet; a plain `claude` runs without. Both recorded.
claude() {
  local _ab _arm=native _args=()
  _ab="$(command -v yeet-ab 2>/dev/null || echo "")"
  for a in "$@"; do
    if [ "$a" = "--yeet" ]; then _arm=yeet; else _args+=("$a"); fi
  done
  if [ -z "$_ab" ]; then command claude "$@"; return $?; fi
  "$_ab" start --arm "$_arm" "${_args[@]}"
}
SNIP
}

cmd_start() {
  local arm="auto"
  while [ $# -gt 0 ]; do
    case "$1" in
      --arm) arm="$2"; shift 2 ;;
      --yeet) arm="yeet"; shift ;;
      *) break ;;
    esac
  done
  if [ "$arm" = "auto" ]; then
    local n; n=$(grep -c . "$LEDGER" 2>/dev/null || echo 0)
    [ $((n % 2)) -eq 0 ] && arm=yeet || arm=native
  fi
  case "$arm" in yeet|native) ;; *) die "--arm must be yeet or native" ;; esac
  local settings; settings="$(build_arm "$arm")" || exit 1
  YEET_AB_ARM="$arm" YEET_AB_HOME="$AB_HOME" \
  YEET_AB_YEETVER="$(yeet version 2>/dev/null || echo unknown)" \
    exec claude --settings "$settings" "$@"
}

# ─── the lake ─────────────────────────────────────────────────────────────────
# One JSON document per session with totals, per-turn usage and every tool call.
# Built from the transcript after the fact, so a session is never slowed down by
# measurement, and re-running ingest is idempotent.
ingest_one() {
  local arm="$1" ts="$2" sid="$3" tp="$4" cwd="$5" yv="$6"
  local out="$LAKE/$sid.json"
  [ -f "$tp" ] || return 0
  # Re-ingest only if the transcript is newer than what we stored.
  if [ -f "$out" ] && [ "$out" -nt "$tp" ]; then return 0; fi
  jq -s --arg arm "$arm" --arg ts "$ts" --arg sid "$sid" --arg cwd "$cwd" \
        --arg yv "$yv" --arg tp "$tp" '
    [ .[] | select(.message.usage != null) | .message.usage ] as $u
    | [ .[] | .message.content[]? | select(.type=="tool_use") ] as $tu
    | ([ .[] | .message.content[]? | select(.type=="tool_result")
         | {id:.tool_use_id, bytes:(.content|tostring|length), text:(.content|tostring)} ]
       | INDEX(.id)) as $tr
    | [ $tu[] | {
        name: .name,
        arg:  ((.input.command // .input.pattern // .input.file_path // "") | tostring),
        bytes: ($tr[.id].bytes // 0),
        text:  ($tr[.id].text  // ""),
        yeet:  ((.input.command // "") | test("(^|[|;& ])yeet "))
      } ] as $calls
    | [ $calls[] | select(.text | test("BLOCKED|unknown flag|unknown shorthand|command not found|no such file|not found|permission denied|Traceback|^Usage:|error:|failed";"i")) ] as $fail
    | ([ $calls[] | (.name + " >>> " + .arg) ] | group_by(.) | map(select(length > 1))) as $rep
    | [ $calls[] | select(.arg | test("(^|[;&|] *)command +(git|npm|ls|grep|find)|which +(git|npm|yeet)|type +(git|npm|yeet)|alias +(git|npm)")) ] as $byp
    | [ $calls[] | select(.text | test("[+][0-9]+ more|[0-9]+ (lines|rows|warnings|files)[^.]*(omitted|suppressed|truncated)|--raw for all|--raw to see|persisted-output";"i")) ] as $trunc
    | {
        session_id: $sid, arm: $arm, started: $ts, cwd: $cwd,
        yeet_version: $yv, transcript: $tp,
        model: ([ .[] | .message.model? // empty ] | last),
        totals: {
          billed_input: ($u | map((.input_tokens//0)+(.cache_creation_input_tokens//0)+(.cache_read_input_tokens//0)) | add // 0),
          input:        ($u | map(.input_tokens//0)                | add // 0),
          cache_create: ($u | map(.cache_creation_input_tokens//0) | add // 0),
          cache_read:   ($u | map(.cache_read_input_tokens//0)     | add // 0),
          output:       ($u | map(.output_tokens//0)               | add // 0),
          turns:        ([ .[] | select(.message.role=="assistant") ] | length),
          tool_calls:   ($calls | length),
          tool_bytes:   ($calls | map(.bytes) | add // 0),
          yeet_calls:   ([ $calls[] | select(.yeet) ] | length)
        },
        per_turn: [ $u[] | {
          input:.input_tokens//0, cache_create:.cache_creation_input_tokens//0,
          cache_read:.cache_read_input_tokens//0, output:.output_tokens//0 } ],
        signals: {
          failures: {
            count: ($fail | length),
            samples: ([ $fail[] | {tool:.name, arg:(.arg|.[0:120]), snippet:(.text|.[0:200])} ] | .[0:5])
          },
          repeats: {
            groups: ($rep | length),
            repeated_calls: ($rep | map(length - 1) | add // 0),
            samples: ([ $rep[] | {call:(.[0]|.[0:160]), n:length} ] | .[0:5])
          },
          bypasses: {
            count: ($byp | length),
            samples: ([ $byp[] | (.arg|.[0:140]) ] | .[0:5])
          },
          truncation: {
            count: ($trunc | length),
            samples: ([ $trunc[] | {tool:.name, snippet:(.text|.[0:160])} ] | .[0:5])
          },
          outliers: {
            max_result_bytes: ($calls | map(.bytes) | max // 0),
            top: ([ $calls | sort_by(-.bytes) | .[0:5][] | {tool:.name, arg:(.arg|.[0:100]), bytes} ])
          }
        },
        tools: [ $calls[] | {name, arg:(.arg|.[0:200]), bytes, yeet} ],
        tool_mix: ($calls | group_by(.name) | map({key:.[0].name, value:length}) | from_entries)
      }
  ' "$tp" > "$out" 2>/dev/null || rm -f "$out"
}

ingest_all() {
  [ -f "$LEDGER" ] || return 0
  while IFS=$'\t' read -r arm ts sid tp cwd yv; do
    [ -n "${sid:-}" ] && ingest_one "$arm" "$ts" "$sid" "$tp" "$cwd" "$yv"
  done < <(jq -r '[.arm//"", .ts//"", .session_id//"", .transcript//"", .cwd//"", .yeet_version//""] | @tsv' "$LEDGER" 2>/dev/null)
}

lake_rows() {  # since_epoch -> arm billed output turns calls toolbytes
  local since="$1"
  for f in "$LAKE"/*.json; do
    [ -f "$f" ] || continue
    jq -r --arg since "$since" '
      if ($since == "" or (.started | sub("Z$";"Z") | fromdateiso8601? // 0) >= ($since|tonumber))
      then "\(.arm) \(.totals.billed_input) \(.totals.output) \(.totals.turns) \(.totals.tool_calls) \(.totals.tool_bytes)"
      else empty end' "$f" 2>/dev/null
  done
}

cmd_report() {
  local since="" since_epoch=""
  while [ $# -gt 0 ]; do case "$1" in --since) since="$2"; shift ;; esac; shift; done
  [ -n "$since" ] && since_epoch=$(( $(date +%s) - ${since%d}*86400 ))
  ingest_all
  local data; data="$(lake_rows "$since_epoch")"
  [ -n "$data" ] || { say "  nothing recorded yet — run some sessions first"; return 0; }

  say ""
  say "  ${BOLD}yeet A/B telemetry${RESET} ${DIM}${since:+last $since}${RESET}"
  say ""
  printf "  %-8s %4s %12s %12s %7s %7s %11s\n" "arm" "n" "billed/sess" "billed/turn" "turns" "calls" "toolB/sess"
  printf "  %s\n" "$(printf '─%.0s' $(seq 1 70))"
  printf '%s\n' "$data" | awk '
    {n[$1]++; b[$1]+=$2; t[$1]+=$4; c[$1]+=$5; tb[$1]+=$6}
    END { for (a in n) printf "  %-8s %4d %12.0f %12.0f %7.1f %7.1f %11.0f\n",
            a, n[a], b[a]/n[a], (t[a]?b[a]/t[a]:0), t[a]/n[a], c[a]/n[a], tb[a]/n[a] }' | sort
  say ""
  local ny nn
  ny=$(printf '%s\n' "$data" | awk '$1=="yeet"{n++}END{print n+0}')
  nn=$(printf '%s\n' "$data" | awk '$1=="native"{n++}END{print n+0}')
  if [ "$ny" -lt 10 ] || [ "$nn" -lt 10 ]; then
    say "  ${YELLOW}Too early to conclude${RESET} (yeet: $ny, native: $nn)."
    say "  ${DIM}Session cost swings >50% with the task. The synthetic benchmark put${RESET}"
    say "  ${DIM}yeet's edge at ~0.8%/turn, so you need 20+ per arm before the${RESET}"
    say "  ${DIM}billed/turn column means anything.${RESET}"
  else
    printf '%s\n' "$data" | awk '
      {n[$1]++; b[$1]+=$2; t[$1]+=$4}
      END { if (t["yeet"]>0 && t["native"]>0) {
              yp=b["yeet"]/t["yeet"]; np=b["native"]/t["native"]
              printf "  billed/turn: native %.0f vs yeet %.0f  (%+.1f%%)\n", np, yp, (np-yp)*100/np } }'
    say "  ${DIM}Per-turn is the fair comparison — session totals track task length.${RESET}"
  fi
  say ""
  say "  ${DIM}lake: $LAKE  ($(ls -1 "$LAKE" 2>/dev/null | wc -l | tr -d ' ') sessions)${RESET}"
  say ""
}

cmd_export() {
  ingest_all
  if [ "${1:-}" = "--csv" ]; then
    echo "session_id,arm,started,model,billed_input,output,turns,tool_calls,tool_bytes,cwd"
    for f in "$LAKE"/*.json; do
      [ -f "$f" ] || continue
      jq -r '[.session_id,.arm,.started,(.model//""),.totals.billed_input,.totals.output,
              .totals.turns,.totals.tool_calls,.totals.tool_bytes,.cwd] | @csv' "$f" 2>/dev/null
    done
  else
    jq -s '.' "$LAKE"/*.json 2>/dev/null
  fi
}

cmd_status() {
  ingest_all
  local n=0
  [ -f "$LEDGER" ] && n=$(grep -c . "$LEDGER" 2>/dev/null || echo 0)
  say "  ledger:   $LEDGER  ($n sessions)"
  say "  lake:     $LAKE  ($(ls -1 "$LAKE" 2>/dev/null | wc -l | tr -d ' ') ingested)"
  [ "$n" -gt 0 ] && jq -r '.arm' "$LEDGER" 2>/dev/null | sort | uniq -c | sed 's/^/    /'
}

case "${1:-}" in
  shell-init) shift; cmd_shell_init ;;
  start)      shift; cmd_start "$@" ;;
  report)     shift; cmd_report "$@" ;;
  export)     shift; cmd_export "$@" ;;
  status)     shift; cmd_status ;;
  *) sed -n '2,18p' "$0" ;;
esac
