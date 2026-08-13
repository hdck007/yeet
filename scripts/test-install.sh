#!/usr/bin/env bash
# test-install.sh — hermetic tests for install.sh and uninstall.sh
#
#   bash scripts/test-install.sh            # run everything
#   bash scripts/test-install.sh roundtrip  # run tests whose name matches
#
# Every test runs in a throwaway sandbox with its own $HOME, its own install
# dir and a stub yeet binary. Nothing touches the real machine: no network, no
# sudo, no writes outside the sandbox.

set -uo pipefail   # deliberately not -e: a failing assertion must not stop the run

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FILTER="${1:-}"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  GREEN='\033[32m'; RED='\033[31m'; YELLOW='\033[33m'; CYAN='\033[36m'
  DIM='\033[2m'; BOLD='\033[1m'; RESET='\033[0m'
else
  GREEN=''; RED=''; YELLOW=''; CYAN=''; DIM=''; BOLD=''; RESET=''
fi

PASSED=0
FAILED=0
SKIPPED=0
FAILED_TESTS=""
CURRENT=""
SANDBOXES=""

# ─── Assertions ───────────────────────────────────────────────────────────────
pass() { PASSED=$((PASSED+1)); printf "    ${GREEN}✓${RESET} %s\n" "$1"; }
fail() {
  FAILED=$((FAILED+1))
  printf "    ${RED}✗${RESET} %s\n" "$1"
  [ $# -gt 1 ] && printf "      ${DIM}%s${RESET}\n" "$2"
  case "$FAILED_TESTS" in *"$CURRENT"*) ;; *) FAILED_TESTS="$FAILED_TESTS $CURRENT" ;; esac
}

assert_file()      { if [ -f "$1" ]; then pass "${2:-file exists: $1}"; else fail "${2:-file exists: $1}" "missing: $1"; fi; }
assert_no_file()   { if [ ! -f "$1" ]; then pass "${2:-file gone: $1}"; else fail "${2:-file gone: $1}" "still present: $1"; fi; }
assert_dir()       { if [ -d "$1" ]; then pass "${2:-dir exists: $1}"; else fail "${2:-dir exists: $1}" "missing: $1"; fi; }
assert_no_dir()    { if [ ! -d "$1" ]; then pass "${2:-dir gone: $1}"; else fail "${2:-dir gone: $1}" "still present: $1"; fi; }
assert_exec()      { if [ -x "$1" ]; then pass "${2:-executable: $1}"; else fail "${2:-executable: $1}" "not executable: $1"; fi; }
trunc() { printf '%s' "$1" | head -c 200 | tr '\n' '|'; }
assert_eq() {
  if [ "$1" = "$2" ]; then pass "${3:-values match}"
  else fail "${3:-values match}" "expected [$(trunc "$2")], got [$(trunc "$1")]"; fi
}
assert_contains() {
  if printf '%s' "$1" | grep -qF -- "$2"; then pass "${3:-output contains '$2'}"
  else fail "${3:-output contains '$2'}" "not found in output: $2"; fi
}
assert_not_contains() {
  if printf '%s' "$1" | grep -qF -- "$2"; then fail "${3:-output lacks '$2'}" "unexpectedly found: $2"
  else pass "${3:-output lacks '$2'}"; fi
}
assert_grep() {
  if [ -f "$1" ] && grep -qF -- "$2" "$1"; then pass "${3:-$1 contains '$2'}"
  else fail "${3:-$1 contains '$2'}" "'$2' not in $1"; fi
}
assert_no_grep() {
  if [ -f "$1" ] && grep -qF -- "$2" "$1"; then fail "${3:-$1 lacks '$2'}" "'$2' still in $1"
  else pass "${3:-$1 lacks '$2'}"; fi
}

# ─── Sandbox ──────────────────────────────────────────────────────────────────
SBX=""
HOME_DIR=""
BIN_DIR=""
CLAUDE_DIR=""
DATA_D=""
COPILOT_DIR=""
PROJECT=""
SAFE_PATH=""

build_safe_path() {
  # A minimal PATH: system tools plus wherever jq lives. Deliberately excludes
  # ~/go/bin and /usr/local/bin so a real yeet install can never be found.
  local p="/usr/bin:/bin:/usr/sbin:/sbin"
  local jq_bin awk_bin
  jq_bin="$(command -v jq 2>/dev/null || echo "")"
  [ -n "$jq_bin" ] && p="$(dirname "$jq_bin"):$p"
  SAFE_PATH="$p"
}

new_sandbox() {
  SBX="$(mktemp -d "${TMPDIR:-/tmp}/yeet-test-XXXXXX")"
  SANDBOXES="$SANDBOXES $SBX"
  HOME_DIR="$SBX/home"
  BIN_DIR="$HOME_DIR/.local/bin"
  CLAUDE_DIR="$HOME_DIR/.claude"
  DATA_D="$HOME_DIR/.local/share/yeet"
  COPILOT_DIR="$HOME_DIR/.copilot"
  PROJECT="$SBX/project"
  # Pre-create the dirs a real user would already have, so a clean uninstall
  # can be compared byte-for-byte against the starting state.
  mkdir -p "$BIN_DIR" "$CLAUDE_DIR" "$PROJECT" "$HOME_DIR/.local/share"

  # Stub yeet: identifies itself as yeet, which is what both scripts key on.
  cat > "$SBX/fake-yeet" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  version) echo "yeet vtest-0.0.0" ;;
  auto-allow)
    d="${HOME}/.local/share/yeet"; mkdir -p "$d"
    if [ -n "${2:-}" ]; then echo "$2" > "$d/auto-allow"; echo "auto-allow: $2"
    else echo "auto-allow: $(cat "$d/auto-allow" 2>/dev/null || echo false)"; fi ;;
  rewrite) exit 1 ;;
  *) echo "yeet stub: ${1:-}" ;;
esac
STUB
  chmod +x "$SBX/fake-yeet"
}

cleanup_sandboxes() {
  for d in $SANDBOXES; do
    case "$d" in /*yeet-test-*) rm -rf "$d" ;; esac
  done
}
trap cleanup_sandboxes EXIT

# Run a repo script inside the sandbox. GOPATH/GOBIN are pinned into the
# sandbox so the binary sweep can never reach a real ~/go/bin/yeet.
run_in_sandbox() {
  local script="$1"; shift
  HOME="$HOME_DIR" \
  PATH="$BIN_DIR:$SAFE_PATH" \
  GOPATH="$HOME_DIR/go" \
  GOBIN="" \
  TMPDIR="${TMPDIR:-/tmp}" \
  NO_COLOR=1 \
  YEET_ASSET_DIR="$REPO_ROOT" \
  YEET_BIN_SRC="$SBX/fake-yeet" \
  YEET_INSTALL_DIR="$BIN_DIR" \
  YEET_CLAUDE_HOME="$CLAUDE_DIR" \
  YEET_DATA_DIR="$DATA_D" \
  YEET_COPILOT_HOME="$COPILOT_DIR" \
  YEET_NO_SUDO=1 \
  bash "$REPO_ROOT/$script" "$@" 2>&1
}

install_yeet()   { run_in_sandbox install.sh "$@"; }
uninstall_yeet() { run_in_sandbox uninstall.sh "$@"; }

# Same as run_in_sandbox but from inside $PROJECT (Copilot writes to the cwd).
run_in_project() {
  local script="$1"; shift
  ( cd "$PROJECT" && \
    HOME="$HOME_DIR" \
    PATH="$BIN_DIR:$SAFE_PATH" \
    GOPATH="$HOME_DIR/go" GOBIN="" \
    NO_COLOR=1 \
    YEET_ASSET_DIR="$REPO_ROOT" \
    YEET_BIN_SRC="$SBX/fake-yeet" \
    YEET_INSTALL_DIR="$BIN_DIR" \
    YEET_CLAUDE_HOME="$CLAUDE_DIR" \
    YEET_DATA_DIR="$DATA_D" \
    YEET_COPILOT_HOME="$COPILOT_DIR" \
    YEET_NO_SUDO=1 \
    bash "$REPO_ROOT/$script" "$@" 2>&1 )
}

# ─── Helpers ──────────────────────────────────────────────────────────────────
hook_count() {
  jq '[.hooks.PreToolUse[]? | select(._yeet == true)] | length' "$1" 2>/dev/null || echo -1
}
any_yeet_hooks() {
  jq '[ (.hooks.PreToolUse // [])[]
        | select(._yeet == true
                 or ((.hooks // []) | map(.command // "") | any(test("yeet")))) ] | length' \
     "$1" 2>/dev/null || echo -1
}

# A deterministic fingerprint of a tree: every file with its hash, every dir.
snapshot() {
  local root="$1"
  [ -d "$root" ] || return 0
  ( cd "$root" || return 0
    find . -type d | LC_ALL=C sort | sed 's|^|D |'
    find . \( -type f -o -type l \) | LC_ALL=C sort | while IFS= read -r f; do
      if [ -L "$f" ]; then printf 'L %s -> %s\n' "$f" "$(readlink "$f")"
      else printf 'F %s %s\n' "$f" "$(shasum -a 256 "$f" 2>/dev/null | awk '{print $1}')"
      fi
    done )
}

# Reproduce what a pre-manifest installer left behind: unmarked hooks, files in
# the old places, and no install-manifest.json anywhere.
seed_legacy_install() {
  mkdir -p "$CLAUDE_DIR/hooks" "$DATA_D" "$COPILOT_DIR" "$BIN_DIR" "$HOME_DIR/.yeet"

  cp "$SBX/fake-yeet" "$BIN_DIR/yeet"; chmod +x "$BIN_DIR/yeet"
  cp "$REPO_ROOT/hooks/yeet-proxy.sh" "$CLAUDE_DIR/hooks/yeet-proxy.sh"
  chmod +x "$CLAUDE_DIR/hooks/yeet-proxy.sh"
  printf 'legacy failure hook\n' > "$CLAUDE_DIR/hooks/yeet-failure.sh"
  chmod +x "$CLAUDE_DIR/hooks/yeet-failure.sh"
  cp "$REPO_ROOT/hooks/claude/yeet-awareness.md" "$CLAUDE_DIR/yeet-awareness.md"
  printf '@yeet-awareness.md\n# My rules\nBe concise.\n' > "$CLAUDE_DIR/CLAUDE.md"
  cp "$REPO_ROOT/hooks/copilot/yeet-awareness.md" "$COPILOT_DIR/copilot-instructions.md"
  printf 'true\n' > "$DATA_D/auto-allow"
  printf 'fake sqlite\n' > "$DATA_D/analytics.db"
  printf 'stale\n' > "$HOME_DIR/.yeet/legacy-state"

  # Hook entries with NO _yeet marker — exactly what the old installer wrote,
  # including the single quotes and backticks in the real blocker messages.
  # Written via a quoted heredoc so the shell cannot reinterpret any of it.
  cat > "$SBX/legacy-settings.json" <<'LEGACY'
{
  "autoCompactThreshold": 100000,
  "permissions": { "allow": ["Bash(yeet:*)", "Bash(ls:*)"] },
  "hooks": { "PreToolUse": [
    {"matcher":"Read",  "hooks":[{"type":"command","command":"echo 'BLOCKED: Use `yeet read <file>` instead of the Read tool.' >&2; exit 2"}]},
    {"matcher":"Glob",  "hooks":[{"type":"command","command":"echo 'BLOCKED: Use `yeet glob \"<pattern>\"` instead of the Glob tool.' >&2; exit 2"}]},
    {"matcher":"Grep",  "hooks":[{"type":"command","command":"echo 'BLOCKED: Use `yeet grep \"<pattern>\"` instead of the Grep tool.' >&2; exit 2"}]},
    {"matcher":"Write", "hooks":[{"type":"command","command":"echo 'BLOCKED: Pipe content to `yeet write <file>` instead of the Write tool.' >&2; exit 2"}]},
    {"matcher":"Edit",  "hooks":[{"type":"command","command":"echo 'BLOCKED: Use `yeet edit <file>` instead of the Edit tool.' >&2; exit 2"}]},
    {"matcher":"Bash",  "hooks":[{"type":"command","command":"bash \"__PROXY__\""}]}
  ]}
}
LEGACY
  jq --arg proxy "$CLAUDE_DIR/hooks/yeet-proxy.sh" \
     '(.hooks.PreToolUse[] | select(.matcher == "Bash") | .hooks[0].command) |= (sub("__PROXY__"; $proxy))' \
     "$SBX/legacy-settings.json" > "$CLAUDE_DIR/settings.json"

  # Guard the fixture itself: a broken fixture would silently pass the tests.
  if [ "$(jq '[.hooks.PreToolUse[]] | length' "$CLAUDE_DIR/settings.json" 2>/dev/null)" != "6" ]; then
    fail "legacy fixture is valid" "seeded settings.json does not have 6 hook entries"
  fi
  if ! grep -q "$CLAUDE_DIR/hooks/yeet-proxy.sh" "$CLAUDE_DIR/settings.json"; then
    fail "legacy fixture is valid" "proxy path was not substituted into the Bash hook"
  fi
}

test_start() {
  CURRENT="$1"
  printf "\n  ${BOLD}%s${RESET}\n" "$1"
}

should_run() {
  [ -z "$FILTER" ] && return 0
  case "$1" in *"$FILTER"*) return 0 ;; *) return 1 ;; esac
}

# ══════════════════════════════════════════════════════════════════════════════
#  Tests
# ══════════════════════════════════════════════════════════════════════════════

t_syntax() {
  test_start "syntax — both scripts parse under bash 3.2 and 5.x"
  for s in install.sh uninstall.sh; do
    if bash -n "$REPO_ROOT/$s" 2>/dev/null; then pass "$s parses (bash $BASH_VERSION)"
    else fail "$s parses (bash $BASH_VERSION)"; fi
    if [ -x /bin/bash ]; then
      if /bin/bash -n "$REPO_ROOT/$s" 2>/dev/null; then pass "$s parses (/bin/bash)"
      else fail "$s parses (/bin/bash)"; fi
    fi
  done
  for s in install.sh uninstall.sh; do
    local out
    out="$(bash "$REPO_ROOT/$s" --help 2>&1)"
    assert_contains "$out" "Usage:" "$s --help prints usage"
    out="$(bash "$REPO_ROOT/$s" --bogus-flag 2>&1)"; local rc=$?
    assert_contains "$out" "Unknown option" "$s rejects unknown flags"
  done
}

t_install_fresh() {
  test_start "install — fresh machine, --yes"
  new_sandbox
  local out; out="$(install_yeet --yes)"

  assert_contains "$out" "installed." "reports success"
  assert_exec "$BIN_DIR/yeet"                          "binary installed and executable"
  assert_exec "$CLAUDE_DIR/hooks/yeet-proxy.sh"        "proxy hook installed and executable"
  assert_file "$CLAUDE_DIR/yeet-awareness.md"          "awareness installed"
  assert_file "$CLAUDE_DIR/settings.json"              "settings.json written"
  assert_file "$CLAUDE_DIR/CLAUDE.md"                  "CLAUDE.md written"
  assert_file "$DATA_D/install-manifest.json"          "manifest written"

  assert_eq "$(hook_count "$CLAUDE_DIR/settings.json")" "6" "exactly 6 marked yeet hooks"
  assert_eq "$(head -1 "$CLAUDE_DIR/CLAUDE.md")" "@yeet-awareness.md" "CLAUDE.md leads with the awareness ref"
  assert_eq "$(jq -r '.autoCompactThreshold' "$CLAUDE_DIR/settings.json")" "100000" "autoCompactThreshold set"
  assert_eq "$(jq -r '(.permissions.allow // []) | index("Bash(yeet:*)") != null' "$CLAUDE_DIR/settings.json")" \
            "true" "Bash(yeet:*) allowed"
  assert_eq "$(cat "$DATA_D/auto-allow" 2>/dev/null)" "true" "auto-allow enabled"

  # The Bash hook must reference the hook script we actually installed.
  assert_eq "$(jq -r '[.hooks.PreToolUse[] | select(._yeet==true and .matcher=="Bash") | .hooks[0].command] | first' \
              "$CLAUDE_DIR/settings.json")" \
            "bash \"$CLAUDE_DIR/hooks/yeet-proxy.sh\"" "Bash hook points at the installed script"

  # Every blocker must be a valid shell snippet that exits 2.
  local n=0
  for m in Read Glob Grep Write Edit; do
    local cmd; cmd="$(jq -r --arg m "$m" \
      '[.hooks.PreToolUse[] | select(._yeet==true and .matcher==$m) | .hooks[0].command] | first' \
      "$CLAUDE_DIR/settings.json")"
    if bash -n -c "$cmd" 2>/dev/null; then
      bash -c "$cmd" 2>/dev/null; [ $? -eq 2 ] && n=$((n+1))
    fi
  done
  assert_eq "$n" "5" "all 5 blocker hooks are valid shell and exit 2"

  # No integration should have been installed for Copilot.
  assert_no_file "$COPILOT_DIR/copilot-instructions.md" "no Copilot files with --yes (Claude is the default)"
  assert_no_file "$SBX/fake-yeet.bak"                   "no stray backups left behind"
  assert_eq "$(ls "$CLAUDE_DIR"/*.yeet-bak-* 2>/dev/null | wc -l | tr -d ' ')" "0" \
            "rollback backups cleaned up after success"
}

t_proxy_hook_runs() {
  test_start "install — the installed proxy hook actually runs"
  new_sandbox
  install_yeet --yes >/dev/null

  local payload out rc
  payload='{"tool_name":"Bash","tool_input":{"command":"echo hi"}}'
  out="$(printf '%s' "$payload" | HOME="$HOME_DIR" PATH="$BIN_DIR:$SAFE_PATH" \
        bash "$CLAUDE_DIR/hooks/yeet-proxy.sh" 2>&1)"; rc=$?
  assert_eq "$rc" "0" "hook exits 0 on a normal Bash payload"

  # An empty payload must not crash the hook (it runs on every single tool call).
  out="$(printf '%s' '{}' | HOME="$HOME_DIR" PATH="$BIN_DIR:$SAFE_PATH" \
        bash "$CLAUDE_DIR/hooks/yeet-proxy.sh" 2>&1)"; rc=$?
  assert_eq "$rc" "0" "hook exits 0 on an empty payload"

  # Auto-allow: a yeet command should be allowed outright.
  out="$(printf '%s' '{"tool_name":"Bash","tool_input":{"command":"yeet read foo.txt"}}' \
        | HOME="$HOME_DIR" PATH="$BIN_DIR:$SAFE_PATH" bash "$CLAUDE_DIR/hooks/yeet-proxy.sh" 2>&1)"
  assert_contains "$out" '"permissionDecision"' "yeet commands are auto-allowed"
  assert_contains "$out" 'allow' "auto-allow decision is 'allow'"
}

t_idempotent() {
  test_start "install — repeat installs never stack duplicates"
  new_sandbox
  install_yeet --yes >/dev/null
  install_yeet --yes >/dev/null
  local out; out="$(install_yeet --yes)"

  assert_eq "$(hook_count "$CLAUDE_DIR/settings.json")" "6" "still exactly 6 hooks after 3 installs"
  assert_eq "$(any_yeet_hooks "$CLAUDE_DIR/settings.json")" "6" "no unmarked yeet hooks accumulated"
  assert_eq "$(jq '(.permissions.allow // []) | length' "$CLAUDE_DIR/settings.json")" "1" \
            "permissions.allow not duplicated"
  assert_eq "$(grep -c '@yeet-awareness.md' "$CLAUDE_DIR/CLAUDE.md" | tr -d ' ')" "1" \
            "CLAUDE.md has exactly one awareness ref"
  assert_contains "$out" "previous install" "reports that it found the previous install"
  assert_eq "$(jq '[.hooks.PreToolUse[]] | length' "$CLAUDE_DIR/settings.json")" "6" \
            "no orphaned hook entries at all"
}

t_legacy_migration() {
  test_start "install — migrates a legacy (unmarked) install and keeps foreign hooks"
  new_sandbox
  seed_legacy_install
  # A hook the user added themselves, which must survive untouched.
  local tmp; tmp="$(mktemp)"
  jq '.hooks.PreToolUse += [{matcher:"Task",hooks:[{type:"command",command:"echo my-own-hook"}]}]
      | .model = "opus"' "$CLAUDE_DIR/settings.json" > "$tmp" && mv "$tmp" "$CLAUDE_DIR/settings.json"

  local out; out="$(install_yeet --yes)"

  assert_eq "$(hook_count "$CLAUDE_DIR/settings.json")" "6" "6 marked hooks after migration"
  assert_eq "$(any_yeet_hooks "$CLAUDE_DIR/settings.json")" "6" "no legacy unmarked hooks remain"
  assert_eq "$(jq '[.hooks.PreToolUse[] | select(.matcher=="Task")] | length' "$CLAUDE_DIR/settings.json")" \
            "1" "the user's own Task hook survived"
  assert_eq "$(jq -r '.model' "$CLAUDE_DIR/settings.json")" "opus" "unrelated settings keys survived"
  assert_eq "$(jq -r '(.permissions.allow // []) | index("Bash(ls:*)") != null' "$CLAUDE_DIR/settings.json")" \
            "true" "the user's own permission entry survived"
  assert_no_file "$CLAUDE_DIR/hooks/yeet-failure.sh" "legacy yeet-failure.sh removed"
  assert_contains "$out" "stale yeet hook entries" "reports the legacy cleanup"
}

t_uninstall_complete() {
  test_start "uninstall — removes everything it installed"
  new_sandbox
  install_yeet --yes >/dev/null
  local out rc
  out="$(uninstall_yeet --yes)"; rc=$?

  assert_eq "$rc" "0" "exits 0"
  assert_contains "$out" "no trace of yeet remains" "reports a clean removal"
  assert_no_file "$BIN_DIR/yeet"                   "binary removed"
  assert_no_file "$CLAUDE_DIR/hooks/yeet-proxy.sh" "proxy hook removed"
  assert_no_file "$CLAUDE_DIR/yeet-awareness.md"   "awareness removed"
  assert_no_dir  "$DATA_D"                         "data dir removed"
  assert_no_dir  "$CLAUDE_DIR/hooks"               "empty hooks dir removed"
  assert_no_file "$CLAUDE_DIR/CLAUDE.md"           "CLAUDE.md removed (held only the yeet ref)"
  if [ -f "$CLAUDE_DIR/settings.json" ]; then
    assert_eq "$(any_yeet_hooks "$CLAUDE_DIR/settings.json")" "0" "no yeet hooks left in settings.json"
  else
    pass "settings.json removed (it was created by the installer and is now empty)"
  fi
}

t_roundtrip() {
  test_start "round trip — HOME is byte-for-byte identical after install+uninstall"
  new_sandbox
  # Give the sandbox a realistic pre-existing Claude Code setup.
  jq -n '{
    model: "opus",
    autoCompactThreshold: 42,
    permissions: { allow: ["Bash(git:*)","Bash(ls:*)"], deny: ["Bash(rm:*)"] },
    hooks: { PreToolUse: [ {matcher:"Task",hooks:[{type:"command",command:"echo mine"}]} ],
             PostToolUse: [ {matcher:"Edit",hooks:[{type:"command",command:"echo edited"}]} ] },
    statusLine: { type: "command", command: "my-statusline" }
  }' > "$CLAUDE_DIR/settings.json"
  printf '# My global rules\n\n- Always write tests.\n- Prefer small diffs.\n' > "$CLAUDE_DIR/CLAUDE.md"
  printf '# My copilot rules\n\nBe brief.\n' > "$COPILOT_DIR/copilot-instructions.md" 2>/dev/null \
    || { mkdir -p "$COPILOT_DIR"; printf '# My copilot rules\n\nBe brief.\n' > "$COPILOT_DIR/copilot-instructions.md"; }
  mkdir -p "$HOME_DIR/notes"; printf 'unrelated\n' > "$HOME_DIR/notes/todo.txt"

  local before after
  before="$(snapshot "$HOME_DIR")"

  # Run from the sandbox project so Copilot's project files land there, not in
  # whatever directory the test suite happens to be invoked from.
  run_in_project install.sh --yes --both >/dev/null 2>&1
  # Sanity: the install must actually have changed something.
  if [ "$(snapshot "$HOME_DIR")" = "$before" ]; then
    fail "install changed the tree" "snapshot identical after install — nothing was installed"
  else
    pass "install changed the tree"
  fi

  local rc
  run_in_project uninstall.sh --yes --purge >/dev/null 2>&1; rc=$?
  assert_eq "$rc" "0" "uninstall exits 0"

  after="$(snapshot "$HOME_DIR")"
  if [ "$before" = "$after" ]; then
    pass "HOME is byte-for-byte identical to its pre-install state"
  else
    fail "HOME is byte-for-byte identical to its pre-install state" \
         "$(diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") | head -20 | tr '\n' '|')"
  fi

  # And the specific values that are easy to silently clobber:
  assert_eq "$(jq -r '.autoCompactThreshold' "$CLAUDE_DIR/settings.json")" "42" \
            "autoCompactThreshold restored to the user's value"
  assert_eq "$(jq -r '.statusLine.command' "$CLAUDE_DIR/settings.json")" "my-statusline" "statusLine intact"
  assert_eq "$(jq '(.permissions.allow // []) | length' "$CLAUDE_DIR/settings.json")" "2" \
            "permissions.allow back to 2 entries"
  assert_grep "$CLAUDE_DIR/CLAUDE.md" "Always write tests." "user's CLAUDE.md content intact"
  assert_no_grep "$CLAUDE_DIR/CLAUDE.md" "@yeet-awareness.md" "awareness ref gone from CLAUDE.md"
  assert_grep "$COPILOT_DIR/copilot-instructions.md" "Be brief." "user's Copilot content intact"
}

t_autocompact_absent() {
  test_start "uninstall — autoCompactThreshold is deleted when the user never had it"
  new_sandbox
  jq -n '{model:"sonnet"}' > "$CLAUDE_DIR/settings.json"
  install_yeet --yes >/dev/null
  assert_eq "$(jq -r '.autoCompactThreshold' "$CLAUDE_DIR/settings.json")" "100000" "installer set it"
  uninstall_yeet --yes >/dev/null
  assert_eq "$(jq -r 'has("autoCompactThreshold")' "$CLAUDE_DIR/settings.json")" "false" \
            "key removed entirely, not left at 100000"
  assert_eq "$(jq -r '.model' "$CLAUDE_DIR/settings.json")" "sonnet" "the rest of the file is untouched"
}

t_legacy_uninstall() {
  test_start "uninstall — removes a legacy install that has no manifest"
  new_sandbox
  seed_legacy_install
  assert_no_file "$DATA_D/install-manifest.json" "fixture has no manifest (as intended)"

  local out rc
  out="$(uninstall_yeet --yes)"; rc=$?

  assert_eq "$rc" "0" "exits 0"
  assert_contains "$out" "legacy sweep" "announces the legacy sweep"
  assert_no_file "$BIN_DIR/yeet"                     "legacy binary removed"
  assert_no_file "$CLAUDE_DIR/hooks/yeet-proxy.sh"   "legacy proxy hook removed"
  assert_no_file "$CLAUDE_DIR/hooks/yeet-failure.sh" "legacy failure hook removed"
  assert_no_file "$CLAUDE_DIR/yeet-awareness.md"     "legacy awareness removed"
  assert_no_dir  "$DATA_D"                           "legacy data dir removed"
  assert_no_dir  "$HOME_DIR/.yeet"                   "legacy ~/.yeet removed"
  assert_no_file "$COPILOT_DIR/copilot-instructions.md" "legacy Copilot instructions removed"
  assert_eq "$(any_yeet_hooks "$CLAUDE_DIR/settings.json")" "0" "unmarked legacy hooks removed"
  assert_eq "$(jq -r 'has("autoCompactThreshold")' "$CLAUDE_DIR/settings.json")" "false" \
            "legacy autoCompactThreshold removed"
  assert_eq "$(jq -r '(.permissions.allow // []) | index("Bash(yeet:*)")' "$CLAUDE_DIR/settings.json")" "null" \
            "legacy yeet permission removed"
  assert_eq "$(jq -r '(.permissions.allow // []) | index("Bash(ls:*)") != null' "$CLAUDE_DIR/settings.json")" \
            "true" "the user's own permission survived the legacy sweep"
  assert_grep "$CLAUDE_DIR/CLAUDE.md" "Be concise." "user content in CLAUDE.md survived"
  assert_no_grep "$CLAUDE_DIR/CLAUDE.md" "@yeet-awareness.md" "awareness ref removed from CLAUDE.md"
}

t_binary_outside_usr_local() {
  test_start "uninstall — finds the binary outside /usr/local/bin"
  new_sandbox
  # The old uninstaller only looked in /usr/local/bin, so an install anywhere
  # else was reported as "fully removed" while yeet still worked.
  local alt="$SBX/opt/bin"
  mkdir -p "$alt"
  HOME="$HOME_DIR" PATH="$alt:$SAFE_PATH" GOPATH="$HOME_DIR/go" GOBIN="" NO_COLOR=1 \
    YEET_ASSET_DIR="$REPO_ROOT" YEET_BIN_SRC="$SBX/fake-yeet" YEET_INSTALL_DIR="$alt" \
    YEET_CLAUDE_HOME="$CLAUDE_DIR" YEET_DATA_DIR="$DATA_D" YEET_COPILOT_HOME="$COPILOT_DIR" \
    YEET_NO_SUDO=1 bash "$REPO_ROOT/install.sh" --yes >/dev/null 2>&1
  assert_exec "$alt/yeet" "installed to a non-standard dir"

  local rc
  HOME="$HOME_DIR" PATH="$alt:$SAFE_PATH" GOPATH="$HOME_DIR/go" GOBIN="" NO_COLOR=1 \
    YEET_CLAUDE_HOME="$CLAUDE_DIR" YEET_DATA_DIR="$DATA_D" YEET_COPILOT_HOME="$COPILOT_DIR" \
    YEET_NO_SUDO=1 bash "$REPO_ROOT/uninstall.sh" --yes >/dev/null 2>&1; rc=$?
  assert_eq "$rc" "0" "uninstall exits 0"
  assert_no_file "$alt/yeet" "binary removed from the non-standard dir (manifest-tracked)"
}

t_shadow_removal() {
  test_start "install — removes an older yeet that shadows the new one on PATH"
  new_sandbox
  local shadow="$SBX/shadow-bin"
  mkdir -p "$shadow"
  cp "$SBX/fake-yeet" "$shadow/yeet"; chmod +x "$shadow/yeet"

  # $shadow comes first on PATH, so it would win over the real install.
  local out
  out="$(HOME="$HOME_DIR" PATH="$shadow:$BIN_DIR:$SAFE_PATH" GOPATH="$HOME_DIR/go" GOBIN="" NO_COLOR=1 \
    YEET_ASSET_DIR="$REPO_ROOT" YEET_BIN_SRC="$SBX/fake-yeet" YEET_INSTALL_DIR="$BIN_DIR" \
    YEET_CLAUDE_HOME="$CLAUDE_DIR" YEET_DATA_DIR="$DATA_D" YEET_COPILOT_HOME="$COPILOT_DIR" \
    YEET_NO_SUDO=1 bash "$REPO_ROOT/install.sh" --yes 2>&1)"

  assert_contains "$out" "Other yeet binaries found" "warns about the shadowing binary"
  assert_no_file "$shadow/yeet" "shadowing binary removed so the new install wins"
  assert_exec "$BIN_DIR/yeet" "new binary in place"
}

t_keep_data() {
  test_start "uninstall --keep-data — analytics survive, config does not"
  new_sandbox
  install_yeet --yes >/dev/null
  printf 'precious analytics\n' > "$DATA_D/analytics.db"

  local rc
  uninstall_yeet --yes --keep-data >/dev/null; rc=$?
  assert_eq "$rc" "0" "exits 0"
  assert_file "$DATA_D/analytics.db" "analytics.db kept"
  assert_eq "$(cat "$DATA_D/analytics.db")" "precious analytics" "analytics.db unchanged"
  assert_no_file "$DATA_D/install-manifest.json" "manifest removed"
  assert_no_file "$DATA_D/auto-allow" "auto-allow removed"
  assert_no_file "$BIN_DIR/yeet" "binary still removed"
  assert_no_file "$CLAUDE_DIR/hooks/yeet-proxy.sh" "hook still removed"
}

t_uninstall_nothing() {
  test_start "uninstall — clean machine, nothing installed"
  new_sandbox
  local out rc
  out="$(uninstall_yeet --yes)"; rc=$?
  assert_eq "$rc" "0" "exits 0"
  assert_contains "$out" "not installed" "says yeet is not installed"
  assert_not_contains "$out" "Removing" "does not enter the removal phase"
}

t_uninstall_twice() {
  test_start "uninstall — running it twice is harmless"
  new_sandbox
  install_yeet --yes >/dev/null
  uninstall_yeet --yes >/dev/null
  local out rc
  out="$(uninstall_yeet --yes)"; rc=$?
  assert_eq "$rc" "0" "second run exits 0"
  assert_contains "$out" "not installed" "second run reports nothing to do"
}

t_dry_run() {
  test_start "--dry-run — changes nothing, for both scripts"
  new_sandbox
  local before out
  before="$(snapshot "$HOME_DIR")"
  out="$(install_yeet --yes --dry-run)"
  assert_contains "$out" "dry run" "install announces the dry run"
  assert_eq "$(snapshot "$HOME_DIR")" "$before" "install --dry-run wrote nothing"
  assert_no_file "$BIN_DIR/yeet" "no binary installed"
  assert_no_file "$DATA_D/install-manifest.json" "no manifest written"

  install_yeet --yes >/dev/null
  before="$(snapshot "$HOME_DIR")"
  out="$(uninstall_yeet --dry-run)"
  assert_contains "$out" "Nothing was changed" "uninstall announces the dry run"
  assert_eq "$(snapshot "$HOME_DIR")" "$before" "uninstall --dry-run removed nothing"
  assert_exec "$BIN_DIR/yeet" "binary still there after a dry-run uninstall"
}

t_binary_only() {
  test_start "install --binary-only — no editor integration"
  new_sandbox
  install_yeet --yes --binary-only >/dev/null
  assert_exec "$BIN_DIR/yeet" "binary installed"
  assert_no_file "$CLAUDE_DIR/hooks/yeet-proxy.sh" "no Claude hook"
  assert_no_file "$CLAUDE_DIR/yeet-awareness.md"   "no awareness file"
  assert_no_file "$CLAUDE_DIR/CLAUDE.md"           "CLAUDE.md untouched"
  assert_file "$DATA_D/install-manifest.json"      "manifest still written"
  assert_eq "$(jq -r '.claude.enabled' "$DATA_D/install-manifest.json")" "false" "manifest records claude=false"
  local rc; uninstall_yeet --yes >/dev/null; rc=$?
  assert_eq "$rc" "0" "uninstall of a binary-only install exits 0"
  assert_no_file "$BIN_DIR/yeet" "binary removed"
}

t_no_auto_allow() {
  test_start "install --no-auto-allow — no permission entry is added"
  new_sandbox
  install_yeet --yes --no-auto-allow >/dev/null
  assert_eq "$(jq -r '(.permissions.allow // []) | index("Bash(yeet:*)")' "$CLAUDE_DIR/settings.json")" \
            "null" "Bash(yeet:*) not added"
  assert_eq "$(jq -r '.auto_allow' "$DATA_D/install-manifest.json")" "false" "manifest records auto_allow=false"
  assert_eq "$(hook_count "$CLAUDE_DIR/settings.json")" "6" "hooks still installed"
}

t_copilot() {
  test_start "install --copilot — project files, and a clean --purge removal"
  new_sandbox
  printf '# My copilot rules\n\nBe brief.\n' > "$SBX/orig-copilot.md"
  mkdir -p "$COPILOT_DIR"; cp "$SBX/orig-copilot.md" "$COPILOT_DIR/copilot-instructions.md"

  run_in_project install.sh --yes --copilot >/dev/null

  assert_file "$PROJECT/.github/hooks/yeet-rewrite.sh"   "rewrite hook installed"
  assert_exec "$PROJECT/.github/hooks/yeet-rewrite.sh"   "rewrite hook executable"
  assert_file "$PROJECT/.github/hooks/yeet-rewrite.json" "hook config installed"
  assert_file "$PROJECT/.vscode/settings.json"           "VS Code settings created"
  assert_grep "$COPILOT_DIR/copilot-instructions.md" "Be brief." "user's Copilot content preserved"
  assert_grep "$COPILOT_DIR/copilot-instructions.md" "yeet:begin" "yeet block added with markers"
  assert_no_file "$CLAUDE_DIR/hooks/yeet-proxy.sh" "no Claude hook with --copilot"

  # Re-installing must not append a second block.
  run_in_project install.sh --yes --copilot >/dev/null
  assert_eq "$(grep -c 'yeet:begin' "$COPILOT_DIR/copilot-instructions.md" | tr -d ' ')" "1" \
            "repeat install keeps exactly one yeet block"

  local rc
  run_in_project uninstall.sh --yes --purge >/dev/null; rc=$?
  assert_eq "$rc" "0" "uninstall --purge exits 0"
  assert_no_file "$PROJECT/.github/hooks/yeet-rewrite.sh"   "project rewrite hook removed"
  assert_no_file "$PROJECT/.github/hooks/yeet-rewrite.json" "project hook config removed"
  assert_no_file "$PROJECT/.vscode/settings.json"           "installer-created VS Code settings removed"
  if [ -f "$COPILOT_DIR/copilot-instructions.md" ]; then
    assert_eq "$(cat "$COPILOT_DIR/copilot-instructions.md")" "$(cat "$SBX/orig-copilot.md")" \
              "Copilot instructions restored to the user's original, byte for byte"
  else
    fail "Copilot instructions restored" "the user's file was deleted instead of unmerged"
  fi
}

t_vscode_merge() {
  test_start "install --copilot — merges into an existing .vscode/settings.json"
  new_sandbox
  mkdir -p "$PROJECT/.vscode"
  jq -n '{"editor.tabSize": 2, "files.eol": "\n"}' > "$PROJECT/.vscode/settings.json"

  run_in_project install.sh --yes --copilot >/dev/null
  assert_eq "$(jq -r '."editor.tabSize"' "$PROJECT/.vscode/settings.json")" "2" "user's keys preserved"
  assert_eq "$(jq -r '."github.copilot.chat.agent.enabled"' "$PROJECT/.vscode/settings.json")" "true" \
            "Copilot key added"

  run_in_project uninstall.sh --yes --purge >/dev/null
  assert_file "$PROJECT/.vscode/settings.json" "pre-existing settings.json kept, not deleted"
  assert_eq "$(jq -r '."editor.tabSize"' "$PROJECT/.vscode/settings.json")" "2" "user's keys still there"
  assert_eq "$(jq -r 'has("github.copilot.chat.agent.enabled")' "$PROJECT/.vscode/settings.json")" "false" \
            "only the Copilot keys were removed"
}

t_corrupt_settings() {
  test_start "install — refuses to touch a corrupt settings.json"
  new_sandbox
  printf '{ this is not json ,,, ' > "$CLAUDE_DIR/settings.json"
  local before out rc
  before="$(cat "$CLAUDE_DIR/settings.json")"
  out="$(install_yeet --yes)"; rc=$?
  assert_eq "$rc" "1" "exits non-zero"
  assert_contains "$out" "not valid JSON" "explains the problem"
  assert_eq "$(cat "$CLAUDE_DIR/settings.json")" "$before" "the corrupt file was left exactly as it was"
  assert_no_file "$DATA_D/install-manifest.json" "no manifest written for a failed install"
}

t_rollback() {
  test_start "install — rolls back cleanly when a step fails mid-way"
  new_sandbox
  jq -n '{model:"opus"}' > "$CLAUDE_DIR/settings.json"
  local before; before="$(snapshot "$HOME_DIR")"
  # A missing asset makes fetch_asset die after the hooks dir has been created.
  local out rc
  out="$(HOME="$HOME_DIR" PATH="$BIN_DIR:$SAFE_PATH" GOPATH="$HOME_DIR/go" GOBIN="" NO_COLOR=1 \
    YEET_ASSET_DIR="$SBX/empty-assets" YEET_RAW_BASE="http://127.0.0.1:1/nope" \
    YEET_BIN_SRC="$SBX/fake-yeet" YEET_INSTALL_DIR="$BIN_DIR" \
    YEET_CLAUDE_HOME="$CLAUDE_DIR" YEET_DATA_DIR="$DATA_D" YEET_COPILOT_HOME="$COPILOT_DIR" \
    YEET_NO_SUDO=1 bash "$REPO_ROOT/install.sh" --yes 2>&1)"; rc=$?
  assert_eq "$rc" "1" "exits non-zero when assets cannot be fetched"
  assert_contains "$out" "rolling back" "announces the rollback"
  assert_eq "$(jq -r '.model' "$CLAUDE_DIR/settings.json")" "opus" "settings.json still intact"
  assert_eq "$(any_yeet_hooks "$CLAUDE_DIR/settings.json")" "0" "no half-written yeet hooks"
  assert_no_file "$DATA_D/install-manifest.json" "no manifest for a failed install"
}

t_manifest_shape() {
  test_start "manifest — records everything uninstall needs"
  new_sandbox
  jq -n '{autoCompactThreshold: 7}' > "$CLAUDE_DIR/settings.json"
  install_yeet --yes >/dev/null
  local m="$DATA_D/install-manifest.json"
  assert_file "$m" "manifest exists"
  if jq -e . "$m" >/dev/null 2>&1; then pass "manifest is valid JSON"; else fail "manifest is valid JSON"; fi
  assert_eq "$(jq -r '.schema' "$m")" "2" "schema version recorded"
  assert_eq "$(jq -r '.binary.path' "$m")" "$BIN_DIR/yeet" "binary path recorded"
  assert_eq "$(jq -r '.claude.auto_compact_prior' "$m")" "7" "prior autoCompactThreshold recorded"
  assert_eq "$(jq -r '.claude.settings_existed' "$m")" "true" "notes that settings.json pre-existed"
  assert_eq "$(jq -r '.claude.hook_count' "$m")" "6" "hook count recorded"
  assert_eq "$(jq -r '.integrations | index("claude") != null' "$m")" "true" "integrations recorded"
  if [ -n "$(jq -r '.binary.sha256' "$m")" ]; then pass "binary sha256 recorded"; else fail "binary sha256 recorded"; fi
  # And the recorded prior value is what actually gets restored.
  uninstall_yeet --yes >/dev/null
  assert_eq "$(jq -r '.autoCompactThreshold' "$CLAUDE_DIR/settings.json")" "7" "prior value restored from manifest"
}

t_no_claude_dir() {
  test_start "install — works when ~/.claude does not exist yet"
  new_sandbox
  rm -rf "$CLAUDE_DIR"
  local rc
  install_yeet --yes >/dev/null; rc=$?
  assert_eq "$rc" "0" "install exits 0"
  assert_file "$CLAUDE_DIR/settings.json" "settings.json created from scratch"
  assert_eq "$(hook_count "$CLAUDE_DIR/settings.json")" "6" "6 hooks in the new file"
  uninstall_yeet --yes >/dev/null; rc=$?
  assert_eq "$rc" "0" "uninstall exits 0"
  assert_no_file "$CLAUDE_DIR/settings.json" "installer-created settings.json removed again"
}

t_leftover_detection() {
  test_start "uninstall — reports leftovers instead of claiming success"
  new_sandbox
  install_yeet --yes >/dev/null
  uninstall_yeet --yes >/dev/null
  # Put a yeet hook back by hand, then re-run: it must be found and removed.
  mkdir -p "$CLAUDE_DIR"
  jq -n '{hooks:{PreToolUse:[{matcher:"Read",_yeet:true,
          hooks:[{type:"command",command:"echo yeet >&2; exit 2"}]}]}}' > "$CLAUDE_DIR/settings.json"
  local out rc
  out="$(uninstall_yeet --yes)"; rc=$?
  assert_contains "$out" "yeet hook entries" "detects the reintroduced hook"
  assert_eq "$rc" "0" "removes it and exits 0"
  # Either the hook is stripped, or the file it was alone in is gone. Both count.
  if [ ! -f "$CLAUDE_DIR/settings.json" ]; then
    pass "hook removed (settings.json held nothing else and was removed)"
  else
    assert_eq "$(any_yeet_hooks "$CLAUDE_DIR/settings.json")" "0" "hook removed"
  fi
}

# ══════════════════════════════════════════════════════════════════════════════
#  Runner
# ══════════════════════════════════════════════════════════════════════════════
TESTS="
t_syntax
t_install_fresh
t_proxy_hook_runs
t_idempotent
t_legacy_migration
t_uninstall_complete
t_roundtrip
t_autocompact_absent
t_legacy_uninstall
t_binary_outside_usr_local
t_shadow_removal
t_keep_data
t_uninstall_nothing
t_uninstall_twice
t_dry_run
t_binary_only
t_no_auto_allow
t_copilot
t_vscode_merge
t_corrupt_settings
t_rollback
t_manifest_shape
t_no_claude_dir
t_leftover_detection
"

echo ""
echo -e "${BOLD}  yeet install/uninstall test suite${RESET}"
echo -e "  ${DIM}repo: $REPO_ROOT${RESET}"
echo -e "  ${DIM}bash: $BASH_VERSION${RESET}"
[ -n "$FILTER" ] && echo -e "  ${DIM}filter: $FILTER${RESET}"

command -v jq >/dev/null 2>&1 || { echo -e "  ${RED}jq is required to run these tests${RESET}"; exit 1; }
build_safe_path

for t in $TESTS; do
  if should_run "$t"; then
    "$t"
  else
    SKIPPED=$((SKIPPED+1))
  fi
done

echo ""
printf '  %s\n' "$(printf '─%.0s' $(seq 1 62))"
if [ "$FAILED" -eq 0 ]; then
  echo -e "  ${GREEN}${BOLD}$PASSED passed${RESET}${DIM}, 0 failed${RESET}"
  [ "$SKIPPED" -gt 0 ] && echo -e "  ${DIM}$SKIPPED test group(s) skipped by filter${RESET}"
  echo ""
  exit 0
else
  echo -e "  ${RED}${BOLD}$FAILED failed${RESET}, ${GREEN}$PASSED passed${RESET}"
  echo -e "  ${DIM}failing groups:${RESET}$FAILED_TESTS"
  echo ""
  exit 1
fi
