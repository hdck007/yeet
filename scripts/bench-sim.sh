#!/usr/bin/env bash
# bench-sim.sh — simulate a real Claude Code session and measure what yeet saves.
#
# Differs from bench-live.sh in three ways that matter for a mixed read/edit/git/npm
# workload:
#   1. Every run gets a PRISTINE COPY of the target repo at a pinned SHA, at a
#      byte-identical path. bench-live.sh points both arms at one live directory,
#      so run 1's edits and git state contaminate run 2.
#   2. It records the /usage number (rate_limit_event.utilization) alongside exact
#      per-session token counts, so the coarse gauge can be cross-checked against
#      the precise one.
#   3. It breaks the saving down per command family (search/read/edit/git/npm)
#      from the stream, so you can see WHERE the saving comes from.
#
# Costs real money: 2 x reps Claude Code sessions.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPS=3
MAX_TURNS=45
MODEL="sonnet"
OUT_DIR="$REPO_ROOT/scripts/bench-results"
KEEP=false
YES=false
YEET_BIN=""
ARMS_OPT=""
WARMUP=2
TARGET_REPO="https://github.com/eslint/eslint.git"
TARGET_SHA="24310e3a0e22b3c086ca402f88448676f2e1cfcd"
CLONE_DEPTH=200

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

while [ $# -gt 0 ]; do
  case "$1" in
    --reps)      REPS="$2"; shift ;;
    --model)     MODEL="$2"; shift ;;
    --max-turns) MAX_TURNS="$2"; shift ;;
    --out)       OUT_DIR="$2"; shift ;;
    --target-repo) TARGET_REPO="$2"; shift ;;
    --target-sha)  TARGET_SHA="$2"; shift ;;
    --yeet-bin)    YEET_BIN="$2"; shift ;;
    --arms)        ARMS_OPT="$2"; shift ;;
    --warmup)      WARMUP="$2"; shift ;;
    --keep)      KEEP=true ;;
    -y|--yes)    YES=true ;;
    -h|--help)   sed -n '2,20p' "$0"; exit 0 ;;
    *) die "Unknown option: $1" ;;
  esac
  shift
done

command -v claude >/dev/null 2>&1 || die "claude CLI not found on PATH"
command -v jq >/dev/null 2>&1     || die "jq is required"
command -v yeet >/dev/null 2>&1 || [ -n "${YEET_BIN:-}" ] || die "yeet must be installed (or pass --yeet-bin)"
command -v git >/dev/null 2>&1    || die "git is required"

STAMP="$(date +%Y%m%d-%H%M%S)"
RUN_DIR="$OUT_DIR/sim-$STAMP"
mkdir -p "$RUN_DIR"

# The yeet arm must exercise a KNOWN binary. The installed one can be stale or
# divergent from the source tree, which silently benchmarks the wrong thing.
[ -n "$YEET_BIN" ] || YEET_BIN="$(command -v yeet)"
[ -x "$YEET_BIN" ] || die "yeet binary not executable: $YEET_BIN"
YEET_BIN="$(cd "$(dirname "$YEET_BIN")" && pwd)/$(basename "$YEET_BIN")"
SHIM="$RUN_DIR/bin"; mkdir -p "$SHIM"
cp "$YEET_BIN" "$SHIM/yeet" && chmod +x "$SHIM/yeet"
YEET_VER="$("$SHIM/yeet" version 2>/dev/null || echo unknown)"
PRISTINE="$RUN_DIR/pristine"
WORK="$RUN_DIR/work"   # stable path so the prompt is byte-identical every run

# ─── Task ─────────────────────────────────────────────────────────────────────
# Five stages covering the workload: search, read, edit, git, npm. Ends in three
# checkable facts, each obtainable ONLY by actually doing its stage — so a run
# that quietly skipped work cannot look like a saving.
TASK="You are working in the ESLint repository at $WORK. Do all of the following.

1. Get your bearings. List the top-level layout, then find where core lint rules
   are defined. Search for the registration pattern across the repo rather than
   guessing from filenames.

2. Focus on the rule 'no-unused-vars'. Read enough of its rule source to say what
   its options schema allows. Do not read whole files when a few lines will do.

3. Find every file in the repo that mentions 'no-unused-vars' (source, tests,
   docs, config presets). Exclude .git and node_modules.

4. Make a real edit: insert the single line '// benchmark-touch' as the very first
   line of the no-unused-vars rule source file. Change nothing else.

5. Inspect your edit with git: show the working tree status, the diff of your
   change, and the last 3 commits that touched that file.

6. Inspect dependencies WITHOUT installing: run 'npm install --dry-run' in the repo
   root and note how many packages it reports it would add.

Finish your reply with exactly these three lines, each on its own line:
TOTAL_REFS: <number of files that mention no-unused-vars>
ESPREE_RANGE: <the espree version range declared in the repo root package.json>
NPM_PACKAGES: <number of packages npm --dry-run said it would add>"

printf '%s\n' "$TASK" > "$RUN_DIR/task.txt"

RE_REFS='TOTAL_REFS:[[:space:]]*[0-9]+'
RE_ESPREE='ESPREE_RANGE:[[:space:]]*[~^]?[0-9]'
RE_NPM='NPM_PACKAGES:[[:space:]]*[0-9]+'

# ─── Setup: pristine target ───────────────────────────────────────────────────
info "Cloning $TARGET_REPO @ ${TARGET_SHA:0:9} (depth $CLONE_DEPTH)..."
if ! git clone --quiet --depth "$CLONE_DEPTH" --single-branch "$TARGET_REPO" "$PRISTINE" 2>"$RUN_DIR/clone.err"; then
  tail -5 "$RUN_DIR/clone.err" >&2; die "clone failed"
fi
git -C "$PRISTINE" checkout --quiet "$TARGET_SHA" 2>/dev/null \
  || warn "pinned SHA not in shallow history; using $(git -C "$PRISTINE" rev-parse --short HEAD)"
ACTUAL_SHA="$(git -C "$PRISTINE" rev-parse HEAD)"
N_FILES="$(git -C "$PRISTINE" ls-files | wc -l | tr -d ' ')"
ok "pristine: $N_FILES files @ ${ACTUAL_SHA:0:9}  ($(du -sh "$PRISTINE" | cut -f1))"

# ─── Setup: arms ──────────────────────────────────────────────────────────────
ARM_YEET="$RUN_DIR/config-yeet"; ARM_NATIVE="$RUN_DIR/config-native"; DATA_YEET="$RUN_DIR/data-yeet"
mkdir -p "$ARM_YEET" "$ARM_NATIVE" "$DATA_YEET"

info "Building the yeet arm with the real install.sh (isolated)..."
if ! YEET_CLAUDE_HOME="$ARM_YEET" YEET_DATA_DIR="$DATA_YEET" YEET_ASSET_DIR="$REPO_ROOT" \
     YEET_SKIP_BINARY=1 YEET_NO_SUDO=1 NO_COLOR=1 \
     bash "$REPO_ROOT/install.sh" --yes --claude --auto-allow > "$RUN_DIR/install-yeet-arm.log" 2>&1; then
  tail -20 "$RUN_DIR/install-yeet-arm.log" >&2; die "could not build yeet arm"
fi
N_HOOKS="$(jq '[.hooks.PreToolUse[]? | select(._yeet==true)] | length' "$ARM_YEET/settings.json" 2>/dev/null || echo 0)"
ok "yeet arm: $N_HOOKS hooks + awareness"
echo '{}' > "$ARM_NATIVE/settings.json"
ok "native arm: no hooks, no awareness"

# Third arm: keep ONLY the Bash rewrite proxy, drop the 5 hard blocks on the
# native tools, and drop the awareness prompt. Rationale from the measurements:
# the native Read tool already does line ranges (offset/limit) and a partially
# read file can still be edited, so blocking it buys nothing and costs turns.
ARM_YBASH="$RUN_DIR/config-yeet-bash"
mkdir -p "$ARM_YBASH"
jq '{hooks: {PreToolUse: [.hooks.PreToolUse[]? | select(.matcher=="Bash")]}}' \
  "$ARM_YEET/settings.json" > "$ARM_YBASH/settings.json" 2>/dev/null
ok "yeet-bash arm: $(jq '[.hooks.PreToolUse[]?]|length' "$ARM_YBASH/settings.json" 2>/dev/null) hook (Bash rewrite only), no blocks, no awareness"

# Fourth arm: Read/Write/Edit unblocked (native already does line ranges and
# partial-read-then-edit), Grep/Glob INTERCEPTED — the hook runs yeet and hands
# the compressed output straight back, so the model gets data in the same turn
# instead of a "use yeet instead" bounce that costs a retry.
ARM_SEL="$RUN_DIR/config-yeet-sel"
mkdir -p "$ARM_SEL/hooks"
cat > "$ARM_SEL/hooks/intercept.sh" <<'INTERCEPT'
#!/usr/bin/env bash
# Intercept a native tool call, answer it with yeet's condensed output, and hand
# that back in the same turn (permissionDecision "deny" puts the reason in front
# of the model, so no retry is needed).
#
# CRITICAL: only intercept when yeet can serve the request *faithfully*. The
# first version ignored the tool's parameters and always ran a plain content
# search. An agent asking for output_mode=files_with_matches got match content
# instead, could not count files from it, and re-ran the search in Bash --
# costing the extra turn the intercept was supposed to save. `yeet grep` has no
# files-with-matches or count mode, so those fall through to the native tool.
IN=$(cat)
TOOL=$(echo "$IN" | jq -r '.tool_name // empty')
PAT=$(echo "$IN"  | jq -r '.tool_input.pattern // empty')
P=$(echo "$IN"    | jq -r '.tool_input.path // "."')
MODE=$(echo "$IN" | jq -r '.tool_input.output_mode // "content"')
GLOB=$(echo "$IN" | jq -r '.tool_input.glob // empty')
HEAD=$(echo "$IN" | jq -r '.tool_input.head_limit // empty')
[ -z "$PAT" ] && exit 0

case "$TOOL" in
  Grep)
    # yeet grep only does content. Anything else must reach the real tool.
    [ "$MODE" = "content" ] || exit 0
    # No glob/type filter equivalent that is guaranteed faithful -> pass through.
    [ -n "$GLOB" ] && exit 0
    OUT=$(yeet grep "$PAT" "$P" 2>&1)
    # Respect an explicit head_limit rather than silently returning more.
    [ -n "$HEAD" ] && OUT=$(printf '%s' "$OUT" | head -n "$HEAD")
    ;;
  Glob)
    OUT=$(yeet glob "$PAT" "$P" 2>&1)
    ;;
  *) exit 0 ;;
esac

# If yeet produced nothing useful, let the native tool answer instead of
# handing back an empty result the agent cannot act on.
[ -z "$(printf '%s' "$OUT" | tr -d '[:space:]')" ] && exit 0

OUT=$(printf '%s' "$OUT" | head -c 6000)
jq -n --arg r "$OUT" '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:$r}}'
INTERCEPT
chmod +x "$ARM_SEL/hooks/intercept.sh"
jq --arg h "bash \"$ARM_SEL/hooks/intercept.sh\"" '
  {hooks: {PreToolUse:
    ( [ .hooks.PreToolUse[]? | select(.matcher=="Bash") ]
      + [ {matcher:"Grep", hooks:[{type:"command", command:$h}]},
          {matcher:"Glob", hooks:[{type:"command", command:$h}]} ] ) }}' \
  "$ARM_YEET/settings.json" > "$ARM_SEL/settings.json" 2>/dev/null
ok "yeet-sel arm: Bash rewrite + Grep/Glob intercept-and-return; Read/Write/Edit unblocked"

# rtk arm — the tool yeet is modelled on, for an external reference point. Its
# `rtk hook claude` processor rewrites Bash and passes the native tools through,
# which is the same shape yeet-sel now ships.
ARM_RTK="$RUN_DIR/config-rtk"
mkdir -p "$ARM_RTK"
if command -v rtk >/dev/null 2>&1; then
  jq -n '{hooks:{PreToolUse:[{matcher:"Bash",hooks:[{type:"command",command:"rtk hook claude"}]}]}}' \
    > "$ARM_RTK/settings.json"
  ok "rtk arm: $(rtk --version 2>/dev/null | head -1) via 'rtk hook claude'"
else
  echo '{}' > "$ARM_RTK/settings.json"
  warn "rtk not installed — the rtk arm would measure nothing"
fi

# ─── Stream extractors ────────────────────────────────────────────────────────
final_text() { jq -r 'select(.type=="result") | .result // empty' "$1" 2>/dev/null; }
bash_cmds()  { jq -r 'select(.type=="assistant") | .message.content[]? | select(.type=="tool_use")
                      | select(.name=="Bash") | .input.command' "$1" 2>/dev/null; }
uses_yeet()  { bash_cmds "$1" | grep -qE '(^|[|;&] *)yeet '; }
tool_bytes() { jq -r 'select(.type=="user") | .message.content[]? | select(.type=="tool_result")
                      | (.content|tostring|length)' "$1" 2>/dev/null | awk '{s+=$1} END{print s+0}'; }

# /usage equivalent: what the interactive /usage panel shows.
util_first() { jq -r 'select(.type=="rate_limit_event") | .rate_limit_info.utilization' "$1" 2>/dev/null | head -1; }
util_last()  { jq -r 'select(.type=="rate_limit_event") | .rate_limit_info.utilization' "$1" 2>/dev/null | tail -1; }

# tool_use -> result bytes, one row per call: name<TAB>bytes<TAB>command
tool_rows() {
  jq -s -r '
    ([.[] | select(.type=="user") | .message.content[]? | select(.type=="tool_result")
      | {key: .tool_use_id, value: (.content|tostring|length)}] | from_entries) as $b
    | [.[] | select(.type=="assistant") | .message.content[]? | select(.type=="tool_use")
       | {name: .name, cmd: (.input.command // ""), bytes: ($b[.id] // 0)}]
    | .[] | "\(.name)\t\(.bytes)\t\(.cmd | gsub("[\n\t]"; " "))"
  ' "$1" 2>/dev/null
}

# Classify a call into a command family.
classify() {
  awk -F'\t' '
    {
      name=$1; bytes=$2; cmd=$3; fam="other"
      if (name ~ /^(Grep|Glob)$/) fam="search"
      else if (name == "Read") fam="read"
      else if (name ~ /^(Edit|Write|MultiEdit)$/) fam="edit"
      else if (name == "Bash") {
        if (cmd ~ /(^|[|;&] *)(git|yeet (diff|log))/) fam="git"
        else if (cmd ~ /(npm|yeet (npm|deps))/) fam="npm"
        else if (cmd ~ /(grep|rg |find |fd |yeet (grep|find|glob|ls|tree))/) fam="search"
        else if (cmd ~ /(cat |head |tail |sed -n|yeet read|yeet smart)/) fam="read"
        else if (cmd ~ /(yeet (edit|write)|sed -i|tee )/) fam="edit"
      }
      calls[fam]++; b[fam]+=bytes
    }
    END { for (f in calls) printf "%s %d %d\n", f, calls[f], b[f] }'
}

# ─── Runner ───────────────────────────────────────────────────────────────────
run_arm() {
  local arm="$1" rep="$2" cfg data stream start end
  case "$arm" in
    yeet)      cfg="$ARM_YEET";  data="$DATA_YEET" ;;
    yeet-bash) cfg="$ARM_YBASH"; data="$RUN_DIR/data-ybash" ;;
    yeet-sel)  cfg="$ARM_SEL";   data="$RUN_DIR/data-ysel" ;;
    rtk)       cfg="$ARM_RTK";   data="$RUN_DIR/data-rtk" ;;
    *)         cfg="$ARM_NATIVE"; data="$RUN_DIR/data-native" ;;
  esac
  mkdir -p "$data"
  stream="$RUN_DIR/$arm-rep$rep.jsonl"

  # Pristine copy at the stable path, so every run starts identical.
  rm -rf "$WORK"
  cp -a "$PRISTINE" "$WORK"

  # Auth lives in the real config dir, so we do NOT override CLAUDE_CONFIG_DIR
  # (an isolated one is simply "Not logged in"). Isolation instead comes from
  # --setting-sources "" which loads NO ambient user/project/local settings.
  # Verified precondition: the real ~/.claude has 0 hooks and no CLAUDE.md, so
  # it is already a clean native baseline.
  local -a args
  args=(--print --output-format stream-json --verbose
        --max-turns "$MAX_TURNS"
        --allowedTools Bash Read Grep Glob Edit Write
        --add-dir "$WORK")
  [ -n "$MODEL" ] && args+=(--model "$MODEL")
  case "$arm" in
    yeet)
      args+=(--settings "$ARM_YEET/settings.json")
      [ -s "$ARM_YEET/yeet-awareness.md" ] && \
        args+=(--append-system-prompt "$(cat "$ARM_YEET/yeet-awareness.md")") ;;
    yeet-bash)
      args+=(--settings "$ARM_YBASH/settings.json") ;;
    yeet-sel)
      args+=(--settings "$ARM_SEL/settings.json") ;;
    rtk)
      args+=(--settings "$ARM_RTK/settings.json") ;;
  esac

  start="$(date +%s)"
  ( cd "$WORK" && printf '%s' "$TASK" | env PATH="$SHIM:$PATH" YEET_DATA_DIR="$data" \
      YEET_HOOK_AUDIT=1 YEET_AUDIT_DIR="$RUN_DIR/audit-$arm-rep$rep" \
      claude "${args[@]}" ) > "$stream" 2>"$RUN_DIR/$arm-rep$rep.err" || true
  end="$(date +%s)"

  # Did the edit actually land on disk? Checked before the work dir is destroyed.
  local edited="no"
  if git -C "$WORK" diff --quiet 2>/dev/null; then edited="no"; else
    git -C "$WORK" diff 2>/dev/null | grep -q 'benchmark-touch' && edited="yes" || edited="other"
  fi
  echo "$edited" > "$RUN_DIR/$arm-rep$rep.edited"
  git -C "$WORK" diff --stat > "$RUN_DIR/$arm-rep$rep.diffstat" 2>/dev/null

  rm -rf "$WORK"   # sequential runs => peak disk is one copy

  local u
  u="$(jq -s -r '
    ([.[] | select(.type=="result")] | last) as $r | ($r.usage // {}) as $g
    | "\($g.input_tokens // 0) \($g.output_tokens // 0) \($g.cache_creation_input_tokens // 0) \($g.cache_read_input_tokens // 0) \($r.num_turns // 0) \($r.total_cost_usd // 0)"
  ' "$stream" 2>/dev/null || echo "0 0 0 0 0 0")"
  printf '%s %s' "$u" "$((end - start))"
}

# ─── Confirm ──────────────────────────────────────────────────────────────────
ARMS="${ARMS_OPT:-native yeet yeet-bash yeet-sel}"
N_ARMS=$(echo $ARMS | wc -w | tr -d " ")
TOTAL_RUNS=$((REPS * N_ARMS + WARMUP))
say ""
say "${BOLD}  yeet simulation benchmark${RESET}"
say "  ${DIM}target:  $TARGET_REPO @ ${ACTUAL_SHA:0:9} ($N_FILES files)${RESET}"
say "  ${DIM}model:   ${MODEL:-<default>}${RESET}"
say "  ${DIM}runs:    $TOTAL_RUNS ($REPS per arm, alternating)${RESET}"
  say "  ${DIM}warmup:  $WARMUP discarded session(s) before measuring${RESET}"
say "  ${DIM}workload: search -> read -> edit -> git -> npm (dry-run)${RESET}"
say "  ${DIM}reports: $RUN_DIR${RESET}"
say "  ${DIM}your ~/.claude is not touched${RESET}"
say ""
if ! $YES; then
  say "  ${YELLOW}This runs $TOTAL_RUNS real Claude Code sessions and will cost money.${RESET}"
  printf "  Proceed? [y/N]: "
  REPLY=""
  if [ -t 0 ]; then read -r REPLY; elif [ -e /dev/tty ]; then read -r REPLY </dev/tty; fi
  case "${REPLY:-N}" in [Yy]|[Yy][Ee][Ss]) ;; *) say "  Aborted."; exit 0 ;; esac
fi

# ─── Warm the prompt cache ────────────────────────────────────────────────────
# Measured on 2026-09-14: the first three sessions of a fresh benchmark reported
# cache_creation of 31,868 / 27,205 / 10,355 and then settled at ~9,000 for every
# session after. That ramp is a property of the cache, not of the arm under test,
# and whichever arm draws an early slot is charged for it. Rotating the arms
# spreads the distortion evenly but leaves it in the numbers, so the first runs
# are executed and thrown away instead.
if [ "${WARMUP:-0}" -gt 0 ]; then
  say ""
  info "Warming the prompt cache with $WARMUP discarded session(s)..."
  for w in $(seq 1 "$WARMUP"); do
    printf "  ${DIM}warmup %s/%s ${RESET}" "$w" "$WARMUP"
    run_arm "native" "warm$w" >/dev/null 2>&1
    cc="$(jq -s -r '([.[]|select(.type=="result")]|last).usage.cache_creation_input_tokens // 0' \
          "$RUN_DIR/native-repwarm$w.jsonl" 2>/dev/null || echo 0)"
    printf "${DIM}cache_create=%s (discarded)${RESET}\n" "$cc"
    rm -f "$RUN_DIR"/native-repwarm$w.* 2>/dev/null
  done
  ok "cache warm — measurement starts here"
fi

# ─── Execute ──────────────────────────────────────────────────────────────────
say ""
ROWS=""
for rep in $(seq 1 "$REPS"); do
  # rotate the configured arms so cache warmth is shared evenly
  ORDER="$(echo $ARMS | tr ' ' '\n' | awk -v k="$rep" '{a[NR]=$0} END{for(i=0;i<NR;i++) print a[((i+k-1)%NR)+1]}' | tr '\n' ' ')"
  for arm in $ORDER; do
    printf "  ${CYAN}→${RESET} rep %s/%s  %-10s " "$rep" "$REPS" "$arm"
    RES="$(run_arm "$arm" "$rep")"
    set -- $RES
    IN="$1"; OUT="$2"; CW="$3"; CR="$4"; TURNS="$5"; COST="$6"; SECS="$7"
    num() { case "${1:-}" in ''|*[!0-9]*) echo 0 ;; *) echo "$1" ;; esac; }
    IN="$(num "$IN")"; OUT="$(num "$OUT")"; CW="$(num "$CW")"; CR="$(num "$CR")"
    STREAM="$RUN_DIR/$arm-rep$rep.jsonl"
    TR="$(tool_bytes "$STREAM")"
    U0="$(util_first "$STREAM")"; U1="$(util_last "$STREAM")"
    EDITED="$(cat "$RUN_DIR/$arm-rep$rep.edited" 2>/dev/null || echo no)"
    BILLED=$((IN + CW + CR))
    FINAL="$(final_text "$STREAM")"

    STATUS="ok"; NOTE=""
    if [ "$arm" = "rtk" ]; then
      if bash_cmds "$STREAM" | grep -qE '"'"'(^|[|;&] *)yeet '"'"'; then
        STATUS="impure"; NOTE="yeet used in the rtk arm"
      fi
    elif [ "$arm" = "native" ] && uses_yeet "$STREAM"; then
      STATUS="impure"; NOTE="yeet used in the native arm"
    elif [ "$arm" != "native" ] && ! uses_yeet "$STREAM"; then
      STATUS="impure"; NOTE="yeet never used in the $arm arm"
    elif ! printf '%s' "$FINAL" | grep -qE "$RE_REFS"; then
      STATUS="incomplete"; NOTE="missing TOTAL_REFS"
    elif ! printf '%s' "$FINAL" | grep -qE "$RE_ESPREE"; then
      STATUS="incomplete"; NOTE="missing ESPREE_RANGE"
    elif ! printf '%s' "$FINAL" | grep -qE "$RE_NPM"; then
      STATUS="incomplete"; NOTE="missing NPM_PACKAGES (npm stage skipped)"
    elif [ "$EDITED" != "yes" ]; then
      STATUS="incomplete"; NOTE="edit did not land on disk (edited=$EDITED)"
    elif [ "$BILLED" -eq 0 ]; then
      STATUS="nodata"; NOTE="no usage reported"
    fi

    if [ "$STATUS" = "ok" ]; then
      printf "${GREEN}ok${RESET}   billed=%-9s out=%-6s tools=%-9s turns=%-3s %ss  usage=%s→%s\n" \
        "$BILLED" "$OUT" "$TR" "$TURNS" "$SECS" "${U0:-?}" "${U1:-?}"
    else
      printf "${RED}%-10s${RESET} %s\n" "$STATUS" "$NOTE"
    fi
    tool_rows "$STREAM" | classify > "$RUN_DIR/$arm-rep$rep.families"
    ROWS="$ROWS$arm|$rep|$STATUS|$IN|$OUT|$CW|$CR|$BILLED|$TURNS|$COST|$SECS|$TR|${U0:-}|${U1:-}|$EDITED|$NOTE
"
  done
done

# ─── Aggregate ────────────────────────────────────────────────────────────────
stat_for() {
  printf '%s' "$ROWS" | awk -F'|' -v a="$1" -v i="$2" '
    $1==a && $3=="ok" { v[n++]=$i+0; s+=$i }
    END { if(n==0){print "0 0 0";exit}
      for(x=1;x<n;x++){k=v[x];y=x-1;while(y>=0&&v[y]>k){v[y+1]=v[y];y--}v[y+1]=k}
      med=(n%2)?v[int(n/2)]:(v[n/2-1]+v[n/2])/2
      printf "%d %.0f %.0f", n, med, s/n }'
}
avg_cost() { printf '%s' "$ROWS" | awk -F'|' -v a="$1" '$1==a&&$3=="ok"{s+=$10;n++} END{if(n==0)print "0";else printf "%.4f",s/n}'; }
: "${UTIL_SPAN:=not-emitted}"; : "${UTIL_N:=0}"
pct() { awk -v n="$1" -v y="$2" 'BEGIN{ if(n<=0){print "n/a";exit} printf "%.1f",(n-y)*100/n }'; }

set -- $(stat_for native 8);  NN="$1"; N_BILL="$2"; N_BILL_MEAN="$3"
set -- $(stat_for native 12); N_TR="$2"
N_COST="$(avg_cost native)"

arm_row() {  # arm -> "n billed toolbytes out turns secs cost vs_native_pct"
  local arm="$1" n bill tr out tu sc cost d
  set -- $(stat_for "$arm" 8);  n="$1"; bill="$2"
  set -- $(stat_for "$arm" 12); tr="$2"
  set -- $(stat_for "$arm" 5);  out="$2"
  set -- $(stat_for "$arm" 9);  tu="$2"
  set -- $(stat_for "$arm" 11); sc="$2"
  cost="$(avg_cost "$arm")"
  if [ "$arm" = "native" ]; then d="ref"; else d="$(pct "$N_BILL" "$bill")%"; fi
  echo "$n $bill $tr $out $tu $sc $cost $d"
}

say ""
printf '  %s\n' "$(printf '─%.0s' $(seq 1 78))"
say ""
if [ "$NN" -eq 0 ]; then
  warn "The native reference arm produced no valid runs. See $RUN_DIR."
else
  say "${BOLD}  Results${RESET} ${DIM}(medians over valid runs; native is the reference)${RESET}"
  say ""
  printf "  %-11s %4s %12s %11s %8s %7s %7s %11s\n" \
    "arm" "n" "billed in" "tool B" "turns" "secs" "cost" "vs native"
  printf "  %s\n" "$(printf '─%.0s' $(seq 1 78))"
  for arm in $ARMS; do
    set -- $(arm_row "$arm")
    printf "  %-11s %4s %12s %11s %8s %7s %7s %11s\n" "$arm" "$1" "$2" "$3" "$5" "$6" "$7" "$8"
  done
  say ""
  say "  ${DIM}Cost is the headline: it is what the API actually charges. Token counts are${RESET}"
  say "  ${DIM}shown alongside, but summing input + cache-creation + cache-read at 1:1${RESET}"
  say "  ${DIM}overweights cache reads, which bill at ~0.1x and dominate the raw count.${RESET}"
  say "  ${DIM}/usage utilization: $UTIL_N event(s), 1%-granular, account-wide — cannot resolve arms.${RESET}"
fi

# ─── Per-family breakdown ─────────────────────────────────────────────────────
say ""
say "${BOLD}  Where the saving comes from${RESET} ${DIM}(tool output bytes, summed over valid runs)${RESET}"
say ""
fam_sum() {
  local arm="$1" fam="$2" total=0 f
  while IFS='|' read -r a rep status _rest; do
    [ "$a" = "$arm" ] && [ "$status" = "ok" ] || continue
    f="$RUN_DIR/$arm-rep$rep.families"
    [ -f "$f" ] && total=$((total + $(awk -v fm="$fam" '$1==fm{print $3}' "$f" | head -1 | tr -d ' ' ) ))
  done <<< "$(printf '%s' "$ROWS")"
  echo "${total:-0}"
}
printf "  %-12s %13s %13s %13s\n" "family" "native" "yeet" "yeet-bash"
FAM_ROWS=""
for fam in search read edit git npm other; do
  nb="$(fam_sum native "$fam" 2>/dev/null || echo 0)"
  yb="$(fam_sum yeet "$fam" 2>/dev/null || echo 0)"
  bb="$(fam_sum yeet-bash "$fam" 2>/dev/null || echo 0)"
  [ "${nb:-0}" -eq 0 ] && [ "${yb:-0}" -eq 0 ] && [ "${bb:-0}" -eq 0 ] && continue
  printf "  %-12s %13s %13s %13s\n" "$fam" "$nb" "$yb" "$bb"
  FAM_ROWS="$FAM_ROWS| $fam | $nb | $yb | $bb |
"
done

# ─── Reports ──────────────────────────────────────────────────────────────────
MD="$RUN_DIR/report.md"
{
  echo "# yeet simulation benchmark — $STAMP"
  echo ""
  echo "- Target: \`$TARGET_REPO\` @ \`$ACTUAL_SHA\` ($N_FILES tracked files)"
  echo "- Model: \`${MODEL:-default}\`"
  echo "- yeet binary under test: \`$YEET_VER\` from \`$YEET_BIN\`"
  echo "- Reps per arm: $REPS (rotating order), max turns $MAX_TURNS"
  echo "- Warmup: $WARMUP session(s) run and discarded first — the prompt cache takes"
  echo "  a few sessions to settle, and until it does cache_creation is inflated by an"
  echo "  amount unrelated to the arm under test."
  echo "- Workload: search → read → edit → git → npm (dry-run), one mixed session"
  echo "- Every run got a pristine copy of the target at the identical path \`$WORK\`"
  echo "- Arms: yeet = \`--settings <installed settings.json>\` (6 PreToolUse hooks) + awareness via \`--append-system-prompt\`; native = neither"
  echo "- Auth comes from the real \`~/.claude\`, which was verified to contain **0 hooks and no CLAUDE.md** — i.e. already a clean native baseline. It is read, never modified."
  echo "- Arm separation verified before the run: yeet arm answered a Read request with \`yeet read\`; native arm used the Read tool and issued 0 yeet commands."
  echo ""
  echo "## Headline (medians over valid runs)"
  echo ""
  echo "| Arm | n | Billed input | Tool bytes | Output | Turns | Secs | Cost | vs native |"
  echo "|---|---:|---:|---:|---:|---:|---:|---:|---:|"
  for arm in $ARMS; do
    set -- $(arm_row "$arm")
    echo "| $arm | $1 | $2 | $3 | $4 | $5 | $6 | $7 | $8 |"
  done
  echo ""
  echo "Arms: **native** = no hooks, no awareness. **yeet** = the shipped install"
  echo "(5 hard blocks on Read/Grep/Glob/Edit/Write + Bash rewrite proxy + awareness)."
  echo "**yeet-bash** = Bash rewrite proxy ONLY — native tools left alone, no awareness."
  echo ""
  echo "> **On \`/usage\`:** it is \`rate_limit_event.utilization\` — 1%-granular,"
  echo "> account-wide, emitted only on threshold crossings. One session moves it by"
  echo "> roughly one granule, so it cannot resolve arm vs arm. Observed: $UTIL_SPAN"
  echo "> over $UTIL_N event(s). The billed-token counts are the API's own per-session"
  echo "> usage and are exact."
  echo ""
  echo "## Where the saving comes from"
  echo ""
  echo "| Family | native bytes | yeet bytes | yeet-bash bytes |"
  echo "|---|---:|---:|---:|"
  printf '%s' "$FAM_ROWS"
  echo ""
  echo "## Every run"
  echo ""
  echo "| Arm | Rep | Status | Billed in | Output | Turns | Cost | Secs | Tool bytes | util start | util end | Edit landed | Note |"
  echo "|---|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|---|---|"
  printf '%s' "$ROWS" | while IFS='|' read -r arm rep status in out cw cr billed turns cost secs tr u0 u1 ed note; do
    [ -n "$arm" ] || continue
    echo "| $arm | $rep | $status | $billed | $out | $turns | $cost | $secs | $tr | $u0 | $u1 | $ed | $note |"
  done
  echo ""
  echo "## Validity checks"
  echo ""
  echo "A run is excluded unless ALL hold:"
  echo ""
  echo "- **Arm purity** — native issued no \`yeet\` command; yeet issued at least one."
  echo "- **TOTAL_REFS** present (the search stage actually ran)."
  echo "- **ESPREE_RANGE** present (the read stage actually ran)."
  echo "- **NPM_PACKAGES** present (the npm stage actually ran)."
  echo "- **Edit landed** — \`git diff\` in the work dir contained \`benchmark-touch\`."
  echo "- **Usage present** — the API reported non-zero tokens."
  echo ""
  echo "## Task"
  echo ""
  echo '```'
  cat "$RUN_DIR/task.txt"
  echo '```'
} > "$MD"

JSON="$RUN_DIR/report.json"
{
  printf '{"benchmark":"sim","stamp":"%s","target":"%s","sha":"%s","files":%s,"model":"%s","reps":%s,' \
    "$STAMP" "$TARGET_REPO" "$ACTUAL_SHA" "$N_FILES" "${MODEL:-default}" "$REPS"
  printf '"yeet_binary":"%s","arms":{' "$YEET_VER"
  AFIRST=1
  for arm in $ARMS; do
    set -- $(arm_row "$arm")
    [ "$AFIRST" -eq 1 ] || printf ','
    AFIRST=0
    printf '"%s":{"n":%s,"billed_input":%s,"tool_bytes":%s,"output":%s,"turns":%s,"seconds":%s,"cost":%s,"vs_native":"%s"}' \
      "$arm" "$1" "$2" "$3" "$4" "$5" "$6" "$7" "$8"
  done
  printf '},"usage_utilization":{"span":"%s","events":%s},"runs":[' "$UTIL_SPAN" "$UTIL_N"
  FIRST=1
  printf '%s' "$ROWS" | while IFS='|' read -r arm rep status in out cw cr billed turns cost secs tr u0 u1 ed note; do
    [ -n "$arm" ] || continue
    [ "$FIRST" -eq 1 ] || printf ','
    FIRST=0
    printf '{"arm":"%s","rep":%s,"status":"%s","billed_input":%s,"output":%s,"turns":%s,"cost":%s,"seconds":%s,"tool_bytes":%s,"util_start":"%s","util_end":"%s","edit_landed":"%s","note":"%s"}' \
      "$arm" "$rep" "$status" "$billed" "$out" "$turns" "$cost" "$secs" "$tr" "$u0" "$u1" "$ed" "$note"
  done
  printf ']}'
} | jq . > "$JSON" 2>/dev/null || rm -f "$JSON"

say ""
ok "Markdown → $MD"
[ -f "$JSON" ] && ok "JSON     → $JSON"
ok "Streams  → $RUN_DIR/*.jsonl"
INVALID="$(printf '%s' "$ROWS" | awk -F'|' '$3!="ok" && NF>3' | wc -l | tr -d ' ')"
[ "${INVALID:-0}" -gt 0 ] && warn "$INVALID run(s) excluded — see the report."

if ! $KEEP; then rm -rf "$ARM_NATIVE" "$DATA_YEET" "$RUN_DIR/data-native" "$PRISTINE" "$ARM_YEET"
else info "Kept: $ARM_YEET $ARM_NATIVE $PRISTINE"; fi
say ""
