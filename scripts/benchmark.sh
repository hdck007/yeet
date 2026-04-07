#!/usr/bin/env bash
# benchmark.sh — Comprehensive side-by-side token savings: yeet vs rtk
#
# Usage:
#   ./scripts/benchmark.sh              # auto-builds yeet from repo
#   ./scripts/benchmark.sh <yeet> <rtk> # use specific binaries
#
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
RTK_REPO="$REPO/rtk"
YARN_LOCK="$REPO/scripts/test-yarn.lock"

# ── colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GRN='\033[0;32m'; YLW='\033[0;33m'
BLU='\033[0;34m'; BLD='\033[1m'; DIM='\033[2m'; RST='\033[0m'

# ── binary resolution ─────────────────────────────────────────────────────────
if [ $# -ge 1 ]; then
  YEET="$1"
else
  echo -e "${DIM}Building yeet from source...${RST}"
  go build -o /tmp/yeet-bench "$REPO/cmd/yeet/" 2>&1 | sed 's/^/  /'
  YEET=/tmp/yeet-bench
fi
RTK="${2:-rtk}"

# ── state ─────────────────────────────────────────────────────────────────────
total_cases=0
total_yeet_chars=0
total_rtk_chars=0
yeet_wins=0; rtk_wins=0; ties=0

# ── helpers ───────────────────────────────────────────────────────────────────
chars() { wc -m 2>/dev/null | tr -d ' \t\n'; }

run_safe() {
  local bin=$1; shift
  local out
  out=$("$bin" "$@" 2>/dev/null) || true
  printf '%s' "$out" | wc -m | tr -d ' \t\n'
}

pct() {
  local a=$1 b=$2   # a=yeet  b=rtk
  if [ "$a" -eq 0 ] && [ "$b" -eq 0 ]; then echo "both 0"; return; fi
  local best=$(( a < b ? a : b ))
  local worst=$(( a > b ? a : b ))
  if [ "$worst" -eq 0 ]; then echo "n/a"; return; fi
  echo "$(( (worst - best) * 100 / worst ))% fewer"
}

section() { echo; echo -e "${BLD}${BLU}══ $1 ══${RST}"; }

# bench <label> <yeet-cmd+args...> -- <rtk-cmd+args...>
bench_split() {
  local label="$1"; shift
  local -a yargs=(); local -a rargs=()
  local mode=yeet
  for arg in "$@"; do
    if [ "$arg" = "--" ]; then mode=rtk; continue; fi
    if [ "$mode" = "yeet" ]; then yargs+=("$arg"); else rargs+=("$arg"); fi
  done

  local y r
  y=$(run_safe "$YEET" "${yargs[@]}")
  r=$(run_safe "$RTK"  "${rargs[@]}")

  local winner tag
  if   [ "$y" -lt "$r" ]; then winner="${GRN}yeet${RST}"; tag=yeet; yeet_wins=$(( yeet_wins+1 ))
  elif [ "$r" -lt "$y" ]; then winner="${RED} rtk${RST}"; tag=rtk;  rtk_wins=$(( rtk_wins+1 ))
  else                         winner="${YLW} tie${RST}"; tag=tie;  ties=$(( ties+1 ))
  fi

  local savings=""
  if [ "$tag" != "tie" ]; then
    savings=" ($(pct "$y" "$r"))"
  fi

  printf "  %-52s yeet=%-8s rtk=%-8s %b%s\n" \
    "$label" "$y" "$r" "$winner" "$savings"

  total_yeet_chars=$(( total_yeet_chars + y ))
  total_rtk_chars=$(( total_rtk_chars + r ))
  total_cases=$(( total_cases + 1 ))
}

# bench <label> <cmd> <args...>  — same cmd+args for both
bench() {
  local label="$1" cmd="$2"; shift 2
  bench_split "$label" "$cmd" "$@" -- "$cmd" "$@"
}

# yeet_only <label> <cmd+args...> — command rtk doesn't support
yeet_only() {
  local label="$1"; shift
  local y
  y=$(run_safe "$YEET" "$@")
  printf "  ${DIM}%-52s yeet=%-8s rtk=%-8s${RST}\n" "[yeet-only] $label" "$y" "n/a"
}

# ── preflight ─────────────────────────────────────────────────────────────────
echo -e "${BLD}yeet vs rtk — comprehensive benchmark${RST}"
echo -e "yeet: $("$YEET" version 2>/dev/null || echo '?')"
echo -e "rtk:  $("$RTK" --version 2>/dev/null || echo '?')"
echo -e "repo: $REPO"
echo "$(printf '─%.0s' {1..80})"

# ══════════════════════════════════════════════════════════════════════════════
section "ls — directory listing"
# ══════════════════════════════════════════════════════════════════════════════
bench "ls (repo root)"                     ls "$REPO"
bench "ls (rtk/src)"                       ls "$RTK_REPO/src"
bench "ls (rtk/src/cmds)"                  ls "$RTK_REPO/src/cmds"
bench "ls (yeet internal/cli)"             ls "$REPO/internal/cli"
bench "ls (yeet internal)"                 ls "$REPO/internal"
bench "ls -a (show hidden + noise)"        ls -a "$REPO"
bench "ls -laR (recursive, repo root)"     ls -laR "$REPO"
bench "ls -laR (recursive, rtk/src)"       ls -laR "$RTK_REPO/src"
bench "ls -laR (recursive, internal)"      ls -laR "$REPO/internal"
bench "ls -laR (recursive, cli)"           ls -laR "$REPO/internal/cli"

# ══════════════════════════════════════════════════════════════════════════════
section "read — file reading with filtering"
# ══════════════════════════════════════════════════════════════════════════════
# Note: rtk levels = none/minimal/aggressive (no 'moderate')
#       yeet levels = none/minimal/moderate/aggressive
#       rtk flag: --tail-lines N  vs  yeet: --tail N
bench "read go.mod (small)"                read "$REPO/go.mod"
bench "read wc.go (small Go)"              read "$REPO/internal/cli/wc.go"
bench "read grep.go (medium Go)"           read "$REPO/internal/cli/grep.go"
bench "read ls.go (medium Go)"             read "$REPO/internal/cli/ls.go"
bench "read Cargo.toml"                    read "$RTK_REPO/Cargo.toml"
bench "read grep_cmd.rs (Rust)"            read "$RTK_REPO/src/cmds/system/grep_cmd.rs"
bench "read Cargo.lock (huge)"             read "$RTK_REPO/Cargo.lock"
bench "read yarn.lock (huge)"              read "$YARN_LOCK"
bench_split "read -l minimal (grep.go)" \
  read "$REPO/internal/cli/grep.go" -l minimal -- \
  read "$REPO/internal/cli/grep.go" -l minimal
bench_split "read -l aggressive (grep.go)" \
  read "$REPO/internal/cli/grep.go" -l aggressive -- \
  read "$REPO/internal/cli/grep.go" -l aggressive
bench_split "read -l aggressive (Cargo.lock)" \
  read "$RTK_REPO/Cargo.lock" -l aggressive -- \
  read "$RTK_REPO/Cargo.lock" -l aggressive
bench_split "read -l aggressive (yarn.lock)" \
  read "$YARN_LOCK" -l aggressive -- \
  read "$YARN_LOCK" -l aggressive
bench "read -n (line numbers, grep.go)"    read "$REPO/internal/cli/grep.go" -n
bench "read --max-lines 50"               read "$REPO/internal/cli/grep.go" --max-lines 50
bench "read --max-lines 100 (Cargo.lock)" read "$RTK_REPO/Cargo.lock" --max-lines 100
bench_split "read --tail 20 (CHANGELOG)" \
  read "$RTK_REPO/CHANGELOG.md" --tail 20 -- \
  read "$RTK_REPO/CHANGELOG.md" --tail-lines 20

# ══════════════════════════════════════════════════════════════════════════════
section "grep — pattern search"
# ══════════════════════════════════════════════════════════════════════════════
bench "grep 'func' (cli pkg)"              grep "func" "$REPO/internal/cli"
bench "grep 'fn ' (rtk/src)"              grep "fn " "$RTK_REPO/src"
bench "grep 'import' (internal)"          grep "import" "$REPO/internal"
bench "grep 'error' (cli pkg)"            grep "error" "$REPO/internal/cli"
bench "grep 'TODO' (repo)"               grep "TODO" "$REPO"
bench "grep 'pub fn' (rtk src)"          grep "pub fn" "$RTK_REPO/src"
bench "grep 'use ' (rtk src)"            grep "use " "$RTK_REPO/src"
bench "grep 'return' (cli pkg)"          grep "return" "$REPO/internal/cli"
bench "grep 'struct' (rtk src)"          grep "struct" "$RTK_REPO/src"
bench "grep 'resolved' (yarn.lock)"      grep "resolved" "$YARN_LOCK"
bench "grep 'version' (yarn.lock)"       grep "version" "$YARN_LOCK"
bench "grep 'integrity' (yarn.lock)"     grep "integrity" "$YARN_LOCK"
bench "grep 'RecordUsage' (repo)"        grep "RecordUsage" "$REPO/internal"

# ══════════════════════════════════════════════════════════════════════════════
section "find — file search"
# ══════════════════════════════════════════════════════════════════════════════
bench "find '*.go' (internal/cli)"        find "*.go" "$REPO/internal/cli"
bench "find '*.go' (internal)"            find "*.go" "$REPO/internal"
bench "find '*.rs' (rtk/src)"            find "*.rs" "$RTK_REPO/src"
bench "find '*.toml' (repo)"             find "*.toml" "$REPO"
bench "find '*.md' (repo)"              find "*.md" "$REPO"
bench "find '*.md' (rtk)"               find "*.md" "$RTK_REPO"
bench "find '*.sh' (repo)"              find "*.sh" "$REPO"
bench "find '*.json' (repo)"            find "*.json" "$REPO"
bench "find '*.lock' (repo)"            find "*.lock" "$REPO"
bench "find '*_test.go' (internal)"      find "*_test.go" "$REPO/internal"

# ══════════════════════════════════════════════════════════════════════════════
section "wc — word / line / byte count"
# ══════════════════════════════════════════════════════════════════════════════
bench "wc (go.mod, small)"              wc "$REPO/go.mod"
bench "wc (grep.go, medium)"            wc "$REPO/internal/cli/grep.go"
bench "wc (Cargo.lock, large)"          wc "$RTK_REPO/Cargo.lock"
bench "wc (yarn.lock, huge)"            wc "$YARN_LOCK"
bench "wc -l (lines)"                   wc -l "$REPO/internal/cli/grep.go"
bench "wc -w (words)"                   wc -w "$REPO/internal/cli/grep.go"
bench "wc -c (bytes)"                   wc -c "$REPO/internal/cli/grep.go"

# ══════════════════════════════════════════════════════════════════════════════
section "tree — directory tree"
# ══════════════════════════════════════════════════════════════════════════════
bench "tree (repo root)"                tree "$REPO"
bench "tree (rtk/src)"                  tree "$RTK_REPO/src"
bench "tree (internal/cli)"             tree "$REPO/internal/cli"

# ══════════════════════════════════════════════════════════════════════════════
section "deps — dependency summary"
# ══════════════════════════════════════════════════════════════════════════════
bench "deps (yeet — go.mod)"           deps "$REPO"
bench "deps (rtk — Cargo.toml)"        deps "$RTK_REPO"

# ══════════════════════════════════════════════════════════════════════════════
section "env — environment variables"
# ══════════════════════════════════════════════════════════════════════════════
bench "env (no filter)"                 env
bench "env HOME"                        env HOME
bench "env GIT"                         env GIT
bench "env SHELL"                       env SHELL

# ══════════════════════════════════════════════════════════════════════════════
section "diff — compact diffs"
# ══════════════════════════════════════════════════════════════════════════════
bench "diff (two Go files)"             diff "$REPO/internal/cli/ls.go" "$REPO/internal/cli/find.go"
bench "diff (two Rust files)"           diff "$RTK_REPO/src/cmds/system/ls.rs" "$RTK_REPO/src/cmds/system/grep_cmd.rs"
bench "diff (two READMEs)"             diff "$REPO/README.md" "$RTK_REPO/README.md"

# ══════════════════════════════════════════════════════════════════════════════
section "json — JSON inspection"
# ══════════════════════════════════════════════════════════════════════════════
bench "json (hooks.json)"              json "$REPO/hooks/hooks.json"
bench "json (plugin.json)"             json "$REPO/.claude-plugin/plugin.json"
bench "json (settings.json)"           json "$REPO/.claude/settings.local.json"

# ══════════════════════════════════════════════════════════════════════════════
section "log — deduplication"
# ══════════════════════════════════════════════════════════════════════════════
LOGFILE="$(mktemp /tmp/bench-log.XXXX)"
for i in $(seq 1 50); do
  echo "2024-01-01T12:00:0${i}Z [INFO] Request received id=abc-${RANDOM} user=42" >> "$LOGFILE"
  echo "2024-01-01T12:00:0${i}Z [ERROR] Connection failed to db-${RANDOM} retrying..." >> "$LOGFILE"
  echo "2024-01-01T12:00:0${i}Z [WARN] Cache miss key=session-${RANDOM}" >> "$LOGFILE"
done
for i in $(seq 1 20); do
  echo "2024-01-01T12:00:0${i}Z [INFO] Server started on port 8080" >> "$LOGFILE"
  echo "2024-01-01T12:00:0${i}Z [DEBUG] Health check ok" >> "$LOGFILE"
done

bench "log (synthetic 170-line log)"   log "$LOGFILE"
bench "log (CHANGELOG — huge text)"    log "$RTK_REPO/CHANGELOG.md"
bench "log (yarn.lock — repetitive)"   log "$YARN_LOCK"
rm -f "$LOGFILE"

# ══════════════════════════════════════════════════════════════════════════════
section "yeet-only commands (no rtk equivalent)"
# ══════════════════════════════════════════════════════════════════════════════
yeet_only "glob **/*.go (repo)"         glob "**/*.go" "$REPO"
yeet_only "glob **/*.rs (rtk)"          glob "**/*.rs" "$RTK_REPO"
yeet_only "glob **/*.md (repo)"         glob "**/*.md" "$REPO"
yeet_only "glob **/*_test.go (repo)"    glob "**/*_test.go" "$REPO"
yeet_only "edit (dry-run)"              edit "$REPO/go.mod" --old "go 1.25.0" --new "go 1.25.0"

# ══════════════════════════════════════════════════════════════════════════════
# SUMMARY
# ══════════════════════════════════════════════════════════════════════════════
echo
echo "$(printf '═%.0s' {1..80})"
echo -e "${BLD}RESULTS  (${total_cases} comparable cases)${RST}"
echo "$(printf '─%.0s' {1..80})"

yeet_pct=0; rtk_pct=0
if [ "$total_rtk_chars" -gt 0 ] && [ "$total_yeet_chars" -lt "$total_rtk_chars" ]; then
  yeet_pct=$(( (total_rtk_chars - total_yeet_chars) * 100 / total_rtk_chars ))
fi
if [ "$total_yeet_chars" -gt 0 ] && [ "$total_rtk_chars" -lt "$total_yeet_chars" ]; then
  rtk_pct=$(( (total_yeet_chars - total_rtk_chars) * 100 / total_yeet_chars ))
fi

printf "  %-8s total chars output: %-12s wins: %d/%d  saves %d%% vs opponent\n" \
  "yeet" "$total_yeet_chars" "$yeet_wins" "$total_cases" "$yeet_pct"
printf "  %-8s total chars output: %-12s wins: %d/%d  saves %d%% vs opponent\n" \
  "rtk"  "$total_rtk_chars"  "$rtk_wins"  "$total_cases" "$rtk_pct"
printf "  ties: %d\n" "$ties"

echo
delta=$(( total_rtk_chars - total_yeet_chars ))
if   [ "$delta" -gt 0 ]; then
  echo -e "  ${GRN}${BLD}yeet outputs $delta fewer chars overall (${yeet_pct}% smaller)${RST}"
elif [ "$delta" -lt 0 ]; then
  delta=$(( -delta ))
  echo -e "  ${RED}${BLD}rtk outputs $delta fewer chars overall (${rtk_pct}% smaller)${RST}"
else
  echo -e "  ${YLW}${BLD}Both tools produce identical total output${RST}"
fi
echo "$(printf '═%.0s' {1..80})"
