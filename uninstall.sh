#!/usr/bin/env bash
# uninstall.sh — remove yeet completely
#
#   curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/uninstall.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/uninstall.sh | bash -s -- --yes
#   ./uninstall.sh --help
#
# Two removal paths, always both attempted:
#   1. Manifest reversal — reads $YEET_DATA_DIR/install-manifest.json written by
#      install.sh and undoes exactly what it did, restoring overwritten values.
#   2. Legacy sweep — finds and removes anything left by older installers that
#      predate the manifest (unmarked hooks, old paths, stray binaries).
#
# Exits non-zero if anything yeet-related survives, so it is safe to assert on.
#
# Targets bash 3.2 (stock macOS /bin/bash).

set -euo pipefail

REPO="${YEET_REPO:-hdck007/yeet}"
CLAUDE_HOME="${YEET_CLAUDE_HOME:-$HOME/.claude}"
DATA_DIR="${YEET_DATA_DIR:-$HOME/.local/share/yeet}"
MANIFEST="$DATA_DIR/install-manifest.json"
COPILOT_HOME="${YEET_COPILOT_HOME:-$HOME/.copilot}"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  BOLD='\033[1m'; GREEN='\033[32m'; YELLOW='\033[33m'; RED='\033[31m'
  CYAN='\033[36m'; DIM='\033[2m'; RESET='\033[0m'
else
  BOLD=''; GREEN=''; YELLOW=''; RED=''; CYAN=''; DIM=''; RESET=''
fi

QUIET=false
ok()   { $QUIET || echo -e "  ${GREEN}✓${RESET} $*"; }
info() { $QUIET || echo -e "  ${CYAN}→${RESET} $*"; }
warn() { echo -e "  ${YELLOW}!${RESET} $*" >&2; }
skip() { $QUIET || echo -e "  ${DIM}·${RESET} ${DIM}$*${RESET}"; }
say()  { $QUIET || echo -e "$*"; }
die()  { echo -e "  ${RED}✗${RESET} $*" >&2; exit 1; }
rule() { $QUIET || printf '  %s\n' "$(printf '─%.0s' $(seq 1 62))"; }

ASSUME_YES=false
[ "${YEET_YES:-0}" = "1" ] && ASSUME_YES=true
KEEP_DATA=false
DRY_RUN=false
PURGE=false
NO_SUDO=false
[ "${YEET_NO_SUDO:-0}" = "1" ] && NO_SUDO=true
PROJECT_DIR="$PWD"

usage() {
  cat <<'EOF'
yeet uninstaller

Usage:
  uninstall.sh [options]
  curl -fsSL .../uninstall.sh | bash -s -- [options]

Options:
  -y, --yes           Do not ask for confirmation
      --keep-data     Keep ~/.local/share/yeet (your analytics history)
      --purge         Also remove project-level files (.github/hooks/yeet-*,
                      .vscode Copilot keys, project .claude yeet hooks) in
                      --project and in the dir recorded at install time
      --project <DIR> Project directory to clean with --purge (default: cwd)
      --dry-run       Show everything that would be removed, change nothing
      --quiet         Only print warnings and errors
  -h, --help          This message

Exit status:
  0  everything removed (or nothing was installed)
  1  something yeet-related is still present — details are printed

Environment overrides:
  YEET_CLAUDE_HOME  YEET_DATA_DIR  YEET_COPILOT_HOME  YEET_NO_SUDO  YEET_YES
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    -y|--yes)     ASSUME_YES=true ;;
    --keep-data)  KEEP_DATA=true ;;
    --purge)      PURGE=true ;;
    --project)    [ $# -ge 2 ] || die "--project needs a value"; PROJECT_DIR="$2"; shift ;;
    --project=*)  PROJECT_DIR="${1#*=}" ;;
    --dry-run)    DRY_RUN=true ;;
    --quiet)      QUIET=true ;;
    --no-color)   BOLD=''; GREEN=''; YELLOW=''; RED=''; CYAN=''; DIM=''; RESET='' ;;
    -h|--help)    usage; exit 0 ;;
    *)            die "Unknown option: $1  (--help for usage)" ;;
  esac
  shift
done

HAVE_JQ=false
command -v jq >/dev/null 2>&1 && HAVE_JQ=true

# ─── Primitives ───────────────────────────────────────────────────────────────
_rm() {
  if $DRY_RUN; then info "${DIM}[dry-run] rm -f $1${RESET}"; return 0; fi
  if rm -f "$1" 2>/dev/null; then return 0; fi
  if ! $NO_SUDO && command -v sudo >/dev/null 2>&1; then sudo rm -f "$1" 2>/dev/null && return 0; fi
  return 1
}
_rmrf() {
  if $DRY_RUN; then info "${DIM}[dry-run] rm -rf $1${RESET}"; return 0; fi
  if rm -rf "$1" 2>/dev/null; then return 0; fi
  if ! $NO_SUDO && command -v sudo >/dev/null 2>&1; then sudo rm -rf "$1" 2>/dev/null && return 0; fi
  return 1
}
_rmdir_if_empty() {
  [ -d "$1" ] || return 0
  [ -z "$(ls -A "$1" 2>/dev/null)" ] || return 0
  if $DRY_RUN; then info "${DIM}[dry-run] rmdir $1${RESET}"; return 0; fi
  rmdir "$1" 2>/dev/null && ok "Removed empty dir: $1" || true
}

# Walk PATH by hand — `command -v` only reports the first hit.
find_on_path() {
  local name="$1" d oldifs="$IFS"
  IFS=:
  for d in $PATH; do
    [ -n "$d" ] || continue
    if [ -f "$d/$name" ]; then IFS="$oldifs"; printf '%s\n' "$d/$name"; IFS=:; fi
  done
  IFS="$oldifs"
}

is_yeet_binary() {
  [ -f "$1" ] || return 1
  [ -x "$1" ] || return 1
  "$1" version 2>/dev/null | grep -qi yeet
}

ask() {
  local prompt="$1" default="$2" reply=""
  if $ASSUME_YES; then printf '%s' "$default"; return 0; fi
  if [ -t 0 ]; then read -r -p "$prompt" reply || true
  elif [ -e /dev/tty ]; then read -r -p "$prompt" reply </dev/tty || true
  fi
  printf '%s' "${reply:-$default}"
}
is_yes() { case "$1" in [Yy]|[Yy][Ee][Ss]) return 0 ;; *) return 1 ;; esac; }

mf() {
  # mf <jq-path> <default> — read a value out of the manifest
  $HAVE_JQ || { printf '%s' "$2"; return 0; }
  [ -f "$MANIFEST" ] || { printf '%s' "$2"; return 0; }
  local v
  v="$(jq -r "$1 // empty" "$MANIFEST" 2>/dev/null || echo "")"
  [ -n "$v" ] && printf '%s' "$v" || printf '%s' "$2"
}

say ""
say "${BOLD}  yeet uninstaller${RESET}"
say "  ${DIM}https://github.com/$REPO${RESET}"
$DRY_RUN && say "  ${YELLOW}dry run — nothing will be removed${RESET}"
say ""
say "  ${DIM}If something did not work, an issue helps a lot:${RESET}"
say "  ${CYAN}https://github.com/$REPO/issues/new${RESET}"

# ─── Discovery ────────────────────────────────────────────────────────────────
say ""
rule
say "${BOLD}  What is installed${RESET}"
say ""

FOUND_COUNT=0
found() { FOUND_COUNT=$((FOUND_COUNT+1)); info "$*"; }

HAVE_MANIFEST=false
if [ -f "$MANIFEST" ] && $HAVE_JQ && jq -e . "$MANIFEST" >/dev/null 2>&1; then
  HAVE_MANIFEST=true
  MF_VERSION="$(mf '.yeet_version' 'unknown')"
  MF_AT="$(mf '.installed_at' 'unknown')"
  found "Install manifest: $MANIFEST  ${DIM}($MF_VERSION, $MF_AT)${RESET}"
elif [ -f "$MANIFEST" ]; then
  warn "Manifest present but unreadable — falling back to a legacy sweep"
else
  skip "No install manifest — using the legacy sweep"
fi

# ── Binaries: manifest path, PATH walk, and every known install location ──────
BIN_CANDIDATES=""
add_bin() {
  local p="$1"
  [ -n "$p" ] || return 0
  case "
$BIN_CANDIDATES" in *"
$p"*) return 0 ;; esac
  BIN_CANDIDATES="$BIN_CANDIDATES
$p"
}

add_bin "$(mf '.binary.path' '')"
for p in $(find_on_path yeet); do add_bin "$p"; done
GOBIN_DIR="$(go env GOPATH 2>/dev/null || echo "")"
for p in \
  /usr/local/bin/yeet \
  /opt/homebrew/bin/yeet \
  /usr/bin/yeet \
  "$HOME/.local/bin/yeet" \
  "$HOME/bin/yeet" \
  "$HOME/go/bin/yeet" \
  ${GOBIN_DIR:+"$GOBIN_DIR/bin/yeet"} \
  "$(go env GOBIN 2>/dev/null || echo "")" \
; do
  case "$p" in */yeet) add_bin "$p" ;; esac
done

BINARIES=""
for p in $(printf '%s' "$BIN_CANDIDATES" | sed '/^$/d'); do
  [ -f "$p" ] || continue
  if is_yeet_binary "$p"; then
    BINARIES="$BINARIES $p"
    found "Binary: $p  ${DIM}($("$p" version 2>/dev/null || echo unknown))${RESET}"
  else
    warn "Skipping $p — exists but does not identify itself as yeet"
  fi
done
[ -z "$BINARIES" ] && skip "Binary: not found in any known location"

# ── Claude Code hook scripts ─────────────────────────────────────────────────
HOOK_FILES=""
for f in \
  "$CLAUDE_HOME/hooks/yeet-proxy.sh" \
  "$CLAUDE_HOME/hooks/yeet-rewrite.sh" \
  "$CLAUDE_HOME/hooks/yeet-failure.sh" \
  "$CLAUDE_HOME/yeet/yeet-proxy.sh" \
  "$HOME/.yeet/yeet-proxy.sh" \
  "$(mf '.claude.proxy' '')" \
; do
  [ -n "$f" ] || continue
  [ -f "$f" ] || continue
  case " $HOOK_FILES " in *" $f "*) continue ;; esac
  HOOK_FILES="$HOOK_FILES $f"
  found "Hook script: $f"
done
[ -z "$HOOK_FILES" ] && skip "Hook scripts: none found"

# ── Settings files carrying yeet hooks ───────────────────────────────────────
count_yeet_hooks() {
  local file="$1"
  $HAVE_JQ || { echo 0; return; }
  [ -f "$file" ] || { echo 0; return; }
  jq -e . "$file" >/dev/null 2>&1 || { echo 0; return; }
  jq '[ (.hooks.PreToolUse // [])[] | select(
        ._yeet == true
        or ((.matcher // "") as $m
            | ($m | test("^(Read|Glob|Grep|Write|Edit|MultiEdit|NotebookEdit)$"))
              and ((.hooks // []) | map(.command // "") | any(test("yeet"))))
        or ((.matcher // "") == "Bash"
            and ((.hooks // []) | map(.command // "")
                 | any(test("yeet-proxy|yeet-rewrite|yeet rewrite"))))
      ) ] | length' "$file" 2>/dev/null || echo 0
}

SETTINGS_FILES="$CLAUDE_HOME/settings.json
$CLAUDE_HOME/settings.local.json"
if $PURGE; then
  SETTINGS_FILES="$SETTINGS_FILES
$PROJECT_DIR/.claude/settings.json
$PROJECT_DIR/.claude/settings.local.json"
  MF_PROJ="$(mf '.copilot.project_dir' '')"
  if [ -n "$MF_PROJ" ] && [ "$MF_PROJ" != "$PROJECT_DIR" ]; then
    SETTINGS_FILES="$SETTINGS_FILES
$MF_PROJ/.claude/settings.json
$MF_PROJ/.claude/settings.local.json"
  fi
fi

DIRTY_SETTINGS=""
for f in $(printf '%s' "$SETTINGS_FILES" | sed '/^$/d'); do
  n="$(count_yeet_hooks "$f")"
  if [ "${n:-0}" -gt 0 ]; then
    DIRTY_SETTINGS="$DIRTY_SETTINGS $f"
    found "Settings: $n yeet hook entries in $f"
  fi
done
[ -z "$DIRTY_SETTINGS" ] && skip "Settings: no yeet hooks found"

# ── autoCompactThreshold / permissions written by the installer ───────────────
CL_SETTINGS="$(mf '.claude.settings_file' "$CLAUDE_HOME/settings.json")"
TOUCHED_AUTOCOMPACT=false
if $HAVE_JQ && [ -f "$CL_SETTINGS" ] && jq -e . "$CL_SETTINGS" >/dev/null 2>&1; then
  CUR_AC="$(jq -c '.autoCompactThreshold // null' "$CL_SETTINGS")"
  if [ "$CUR_AC" = "100000" ]; then
    TOUCHED_AUTOCOMPACT=true
    if $HAVE_MANIFEST; then
      PRIOR_AC="$(jq -c '.claude.auto_compact_prior // null' "$MANIFEST")"
      if [ "$PRIOR_AC" = "null" ]; then
        found "Setting: autoCompactThreshold=100000 in $CL_SETTINGS ${DIM}(added by installer)${RESET}"
      else
        found "Setting: autoCompactThreshold=100000 in $CL_SETTINGS ${DIM}(was $PRIOR_AC)${RESET}"
      fi
    else
      found "Setting: autoCompactThreshold=100000 in $CL_SETTINGS ${DIM}(installer default)${RESET}"
    fi
  fi
fi

PERMS_TO_DROP="[]"
if $HAVE_JQ && $HAVE_MANIFEST && [ -f "$CL_SETTINGS" ]; then
  PERMS_TO_DROP="$(jq -c '.claude.permissions_allow_added // []' "$MANIFEST" 2>/dev/null || echo '[]')"
elif $HAVE_JQ && [ -f "$CL_SETTINGS" ] && jq -e . "$CL_SETTINGS" >/dev/null 2>&1; then
  # Legacy: the installer only ever added this exact entry.
  if jq -e '(.permissions.allow // []) | index("Bash(yeet:*)")' "$CL_SETTINGS" >/dev/null 2>&1; then
    PERMS_TO_DROP='["Bash(yeet:*)"]'
  fi
fi
if $HAVE_JQ && [ "$(printf '%s' "$PERMS_TO_DROP" | jq 'length')" -gt 0 ]; then
  found "Permissions: $(printf '%s' "$PERMS_TO_DROP" | jq -r 'join(", ")') in $CL_SETTINGS"
fi

# ── Awareness + CLAUDE.md ────────────────────────────────────────────────────
AWARENESS_FILES=""
for f in "$CLAUDE_HOME/yeet-awareness.md" "$(mf '.claude.awareness' '')"; do
  [ -n "$f" ] || continue
  [ -f "$f" ] || continue
  case " $AWARENESS_FILES " in *" $f "*) continue ;; esac
  AWARENESS_FILES="$AWARENESS_FILES $f"
  found "Awareness: $f"
done
[ -z "$AWARENESS_FILES" ] && skip "Awareness: not found"

CL_MD="$(mf '.claude.claude_md' "$CLAUDE_HOME/CLAUDE.md")"
CL_MD_DIRTY=false
if [ -f "$CL_MD" ] && grep -q "yeet-awareness\.md" "$CL_MD" 2>/dev/null; then
  CL_MD_DIRTY=true
  found "CLAUDE.md: yeet-awareness reference in $CL_MD"
else
  skip "CLAUDE.md: no yeet-awareness reference"
fi

# ── Copilot ──────────────────────────────────────────────────────────────────
YEET_MARK_BEGIN='<!-- yeet:begin'
YEET_MARK_END='<!-- yeet:end -->'
CP_INSTRUCTIONS="$(mf '.copilot.instructions' "$COPILOT_HOME/copilot-instructions.md")"
CP_MODE="none"   # none | block | legacy
if [ -f "$CP_INSTRUCTIONS" ]; then
  if grep -qF "$YEET_MARK_BEGIN" "$CP_INSTRUCTIONS" 2>/dev/null; then
    CP_MODE="block"
    found "Copilot instructions: yeet block in $CP_INSTRUCTIONS"
  elif grep -qE 'yeet (read|grep|glob|smart|write|edit)' "$CP_INSTRUCTIONS" 2>/dev/null; then
    CP_MODE="legacy"
    found "Copilot instructions: legacy yeet file $CP_INSTRUCTIONS"
  fi
fi
[ "$CP_MODE" = "none" ] && skip "Copilot instructions: not found"

# ── Project-level files (only with --purge) ──────────────────────────────────
PROJECT_FILES=""
VSCODE_TARGETS=""
if $PURGE; then
  PROJ_DIRS="$PROJECT_DIR"
  MF_PROJ="$(mf '.copilot.project_dir' '')"
  [ -n "$MF_PROJ" ] && [ "$MF_PROJ" != "$PROJECT_DIR" ] && PROJ_DIRS="$PROJ_DIRS
$MF_PROJ"
  for d in $(printf '%s' "$PROJ_DIRS" | sed '/^$/d'); do
    for f in "$d/.github/hooks/yeet-rewrite.sh" "$d/.github/hooks/yeet-rewrite.json"; do
      [ -f "$f" ] || continue
      PROJECT_FILES="$PROJECT_FILES $f"
      found "Project hook: $f"
    done
    v="$d/.vscode/settings.json"
    if [ -f "$v" ] && $HAVE_JQ && jq -e '."github.copilot.chat.agent.enabled"' "$v" >/dev/null 2>&1; then
      VSCODE_TARGETS="$VSCODE_TARGETS $v"
      found "VS Code Copilot keys: $v"
    fi
  done
  [ -z "$PROJECT_FILES$VSCODE_TARGETS" ] && skip "Project files: none found"
fi

# ── Plugin / skill installs ──────────────────────────────────────────────────
PLUGIN_DIRS=""
for d in "$CLAUDE_HOME/plugins/yeet-integration" "$CLAUDE_HOME/plugins/repos/$REPO"; do
  [ -d "$d" ] || continue
  PLUGIN_DIRS="$PLUGIN_DIRS $d"
  found "Plugin dir: $d"
done
SKILL_DIRS=""
for d in "$CLAUDE_HOME/skills/yeet-install" "$CLAUDE_HOME/skills/yeet-uninstall" "$CLAUDE_HOME/skills/yeet-benchmark"; do
  if [ -d "$d" ] || [ -L "$d" ]; then
    SKILL_DIRS="$SKILL_DIRS $d"
    found "Skill: $d"
  fi
done

# ── Data ─────────────────────────────────────────────────────────────────────
DATA_DIRS=""
for d in "$DATA_DIR" "$HOME/.yeet" "$HOME/Library/Application Support/yeet"; do
  [ -d "$d" ] || continue
  case " $DATA_DIRS " in *" $d "*) continue ;; esac
  DATA_DIRS="$DATA_DIRS
$d"
done
if [ -n "$(printf '%s' "$DATA_DIRS" | sed '/^$/d')" ]; then
  if $KEEP_DATA; then
    skip "Data: keeping$(printf '%s' "$DATA_DIRS" | sed '/^$/d' | tr '\n' ' ') (--keep-data)"
  else
    for d in $(printf '%s' "$DATA_DIRS" | sed '/^$/d'); do
      sz="$(du -sh "$d" 2>/dev/null | awk '{print $1}')"
      found "Data: $d  ${DIM}(${sz:-?})${RESET}"
    done
  fi
else
  skip "Data: no data directory"
fi

# ── Installer backups ────────────────────────────────────────────────────────
BAK_FILES=""
for d in "$CLAUDE_HOME" "$COPILOT_HOME"; do
  [ -d "$d" ] || continue
  for f in "$d"/*.yeet-bak-* "$d"/hooks/*.yeet-bak-*; do
    [ -f "$f" ] || continue
    BAK_FILES="$BAK_FILES $f"
    found "Installer backup: $f"
  done
done

say ""
if [ "$FOUND_COUNT" -eq 0 ]; then
  say "  ${GREEN}Nothing to remove — yeet is not installed.${RESET}"
  say ""
  exit 0
fi

# ─── Confirm ──────────────────────────────────────────────────────────────────
# --yes answers this prompt; the interactive default stays N so a stray Enter
# never wipes anything.
if ! $DRY_RUN && ! $ASSUME_YES; then
  say "  ${BOLD}Everything listed above will be removed. This cannot be undone.${RESET}"
  $KEEP_DATA && say "  ${DIM}Your analytics history is kept (--keep-data).${RESET}"
  $PURGE || say "  ${DIM}Project files (.github/hooks, .vscode) are left alone — use --purge.${RESET}"
  say ""
  C="$(ask "  Proceed? [y/N]: " N)"
  if ! is_yes "$C"; then
    say ""
    say "  ${DIM}Aborted — nothing was changed.${RESET}"
    say ""
    exit 0
  fi
fi

say ""
rule
say "${BOLD}  Removing${RESET}"
say ""

FAILURES=0
fail() { warn "$*"; FAILURES=$((FAILURES+1)); }

# ─── 1. Binaries ──────────────────────────────────────────────────────────────
for b in $BINARIES; do
  if _rm "$b"; then ok "Removed binary: $b"; else fail "Could not remove $b (try: sudo rm -f $b)"; fi
done
$DRY_RUN || hash -r 2>/dev/null || true

# ─── 2. Hook scripts ──────────────────────────────────────────────────────────
for f in $HOOK_FILES; do
  if _rm "$f"; then ok "Removed hook script: $f"; else fail "Could not remove $f"; fi
done

# ─── 3. yeet hooks out of settings files ──────────────────────────────────────
for f in $DIRTY_SETTINGS; do
  if ! $HAVE_JQ; then fail "jq missing — cannot clean $f (install jq and re-run)"; continue; fi
  if $DRY_RUN; then
    info "${DIM}[dry-run] strip yeet hooks from $f${RESET}"
    continue
  fi
  TMP="$(mktemp)"
  if jq '
    .hooks.PreToolUse |= map(select(
      (._yeet == true
       or ((.matcher // "") as $m
           | ($m | test("^(Read|Glob|Grep|Write|Edit|MultiEdit|NotebookEdit)$"))
             and ((.hooks // []) | map(.command // "") | any(test("yeet"))))
       or ((.matcher // "") == "Bash"
           and ((.hooks // []) | map(.command // "")
                | any(test("yeet-proxy|yeet-rewrite|yeet rewrite"))))) | not))
    | if (.hooks.PreToolUse | length) == 0 then del(.hooks.PreToolUse) else . end
    | if (.hooks | length) == 0 then del(.hooks) else . end
  ' "$f" > "$TMP" 2>/dev/null && jq -e . "$TMP" >/dev/null 2>&1; then
    mv "$TMP" "$f"
    ok "Removed yeet hooks from $f"
  else
    rm -f "$TMP"
    fail "Failed to clean $f — edit it by hand"
  fi
done

# ─── 4. autoCompactThreshold + permissions ────────────────────────────────────
if $TOUCHED_AUTOCOMPACT && $HAVE_JQ && [ -f "$CL_SETTINGS" ]; then
  PRIOR_AC="null"
  $HAVE_MANIFEST && PRIOR_AC="$(jq -c '.claude.auto_compact_prior // null' "$MANIFEST" 2>/dev/null || echo null)"
  if $DRY_RUN; then
    info "${DIM}[dry-run] restore autoCompactThreshold (prior: $PRIOR_AC) in $CL_SETTINGS${RESET}"
  else
    TMP="$(mktemp)"
    if jq --argjson prior "$PRIOR_AC" '
        if $prior == null then del(.autoCompactThreshold)
        else .autoCompactThreshold = $prior end
      ' "$CL_SETTINGS" > "$TMP" 2>/dev/null && jq -e . "$TMP" >/dev/null 2>&1; then
      mv "$TMP" "$CL_SETTINGS"
      if [ "$PRIOR_AC" = "null" ]; then
        ok "Removed autoCompactThreshold from $CL_SETTINGS"
      else
        ok "Restored autoCompactThreshold=$PRIOR_AC in $CL_SETTINGS"
      fi
    else
      rm -f "$TMP"; fail "Could not restore autoCompactThreshold in $CL_SETTINGS"
    fi
  fi
fi

if $HAVE_JQ && [ -f "$CL_SETTINGS" ] \
   && [ "$(printf '%s' "$PERMS_TO_DROP" | jq 'length')" -gt 0 ]; then
  if $DRY_RUN; then
    info "${DIM}[dry-run] drop $(printf '%s' "$PERMS_TO_DROP" | jq -c .) from permissions.allow${RESET}"
  else
    TMP="$(mktemp)"
    if jq --argjson drop "$PERMS_TO_DROP" '
        if (.permissions.allow? | type) == "array" then
          .permissions.allow = (.permissions.allow - $drop)
          | if (.permissions.allow | length) == 0 then del(.permissions.allow) else . end
          | if (.permissions? | type) == "object" and (.permissions | length) == 0
            then del(.permissions) else . end
        else . end
      ' "$CL_SETTINGS" > "$TMP" 2>/dev/null && jq -e . "$TMP" >/dev/null 2>&1; then
      mv "$TMP" "$CL_SETTINGS"
      ok "Removed yeet permission entries from $CL_SETTINGS"
    else
      rm -f "$TMP"; fail "Could not clean permissions in $CL_SETTINGS"
    fi
  fi
fi

# Drop settings files that we created and that now hold nothing.
if $HAVE_JQ && [ -f "$CL_SETTINGS" ] && ! $DRY_RUN; then
  if [ "$(jq -c . "$CL_SETTINGS")" = "{}" ]; then
    CREATED_BY_US=true
    $HAVE_MANIFEST && [ "$(jq -r '.claude.settings_existed' "$MANIFEST" 2>/dev/null)" = "true" ] && CREATED_BY_US=false
    if $CREATED_BY_US; then
      _rm "$CL_SETTINGS" && ok "Removed empty $CL_SETTINGS"
    else
      skip "Left $CL_SETTINGS in place (empty, but it predates yeet)"
    fi
  fi
fi

# ─── 5. Awareness + CLAUDE.md ─────────────────────────────────────────────────
for f in $AWARENESS_FILES; do
  if _rm "$f"; then ok "Removed awareness: $f"; else fail "Could not remove $f"; fi
done

if $CL_MD_DIRTY; then
  if $DRY_RUN; then
    info "${DIM}[dry-run] remove yeet-awareness reference from $CL_MD${RESET}"
  else
    TMP="$(mktemp)"
    grep -v "yeet-awareness\.md" "$CL_MD" > "$TMP" 2>/dev/null || true
    if [ -s "$TMP" ] && [ -n "$(tr -d '[:space:]' < "$TMP")" ]; then
      mv "$TMP" "$CL_MD"
      ok "Removed yeet-awareness reference from $CL_MD"
    else
      rm -f "$TMP"
      if _rm "$CL_MD"; then ok "Removed $CL_MD ${DIM}(held nothing but the yeet reference)${RESET}"
      else fail "Could not remove $CL_MD"; fi
    fi
  fi
fi

# ─── 6. Copilot ───────────────────────────────────────────────────────────────
case "$CP_MODE" in
  block)
    if $DRY_RUN; then
      info "${DIM}[dry-run] strip the yeet block from $CP_INSTRUCTIONS${RESET}"
    else
      TMP="$(mktemp)"
      awk -v b="$YEET_MARK_BEGIN" -v e="$YEET_MARK_END" '
        index($0, b) == 1 { skip = 1; next }
        index($0, e) == 1 { skip = 0; next }
        !skip { print }
      ' "$CP_INSTRUCTIONS" > "$TMP" 2>/dev/null || true
      # Drop the blank line the installer added as a separator, so the file goes
      # back to exactly what the user had.
      awk 'BEGIN{n=0} {lines[NR]=$0}
           END{for(i=NR;i>0;i--){if(lines[i] ~ /[^[:space:]]/){n=i;break}}
               for(i=1;i<=n;i++) print lines[i]}' "$TMP" > "$TMP.trim" 2>/dev/null \
        && mv "$TMP.trim" "$TMP"
      if [ -n "$(tr -d '[:space:]' < "$TMP" 2>/dev/null)" ]; then
        mv "$TMP" "$CP_INSTRUCTIONS"
        ok "Removed the yeet block from $CP_INSTRUCTIONS ${DIM}(your content kept)${RESET}"
      else
        rm -f "$TMP"
        if _rm "$CP_INSTRUCTIONS"; then ok "Removed $CP_INSTRUCTIONS ${DIM}(held nothing but the yeet block)${RESET}"
        else fail "Could not remove $CP_INSTRUCTIONS"; fi
      fi
    fi
    ;;
  legacy)
    if _rm "$CP_INSTRUCTIONS"; then ok "Removed legacy $CP_INSTRUCTIONS"
    else fail "Could not remove $CP_INSTRUCTIONS"; fi
    ;;
esac

# ─── 7. Project files ─────────────────────────────────────────────────────────
for f in $PROJECT_FILES; do
  if _rm "$f"; then ok "Removed $f"; else fail "Could not remove $f"; fi
done
for v in $VSCODE_TARGETS; do
  [ -f "$v" ] || continue
  WAS_CREATED=false
  if $HAVE_MANIFEST \
     && [ "$(jq -r '.copilot.vscode_created' "$MANIFEST" 2>/dev/null)" = "true" ] \
     && [ "$(jq -r '.copilot.vscode_settings' "$MANIFEST" 2>/dev/null)" = "$v" ]; then
    WAS_CREATED=true
  fi
  if $WAS_CREATED; then
    if _rm "$v"; then ok "Removed $v ${DIM}(created by installer)${RESET}"; else fail "Could not remove $v"; fi
  elif $DRY_RUN; then
    info "${DIM}[dry-run] remove Copilot agent keys from $v${RESET}"
  else
    TMP="$(mktemp)"
    if jq 'del(."github.copilot.chat.agent.enabled")
         | del(."github.copilot.chat.agent.runTasks")
         | del(."github.copilot.chat.useProjectTemplates")' "$v" > "$TMP" 2>/dev/null \
       && jq -e . "$TMP" >/dev/null 2>&1; then
      if [ "$(jq -c . "$TMP")" = "{}" ]; then
        rm -f "$TMP"; _rm "$v" && ok "Removed $v (empty after cleanup)"
      else
        mv "$TMP" "$v"; ok "Removed Copilot agent keys from $v"
      fi
    else
      rm -f "$TMP"; fail "Could not clean $v"
    fi
  fi
done

# ─── 8. Plugins / skills ──────────────────────────────────────────────────────
for d in $PLUGIN_DIRS; do
  if _rmrf "$d"; then ok "Removed plugin dir: $d"; else fail "Could not remove $d"; fi
done
for d in $SKILL_DIRS; do
  if $DRY_RUN; then info "${DIM}[dry-run] rm -rf $d${RESET}"
  elif [ -L "$d" ]; then _rm "$d" && ok "Removed skill link: $d" || fail "Could not remove $d"
  else _rmrf "$d" && ok "Removed skill: $d" || fail "Could not remove $d"; fi
done

# ─── 9. Backups ───────────────────────────────────────────────────────────────
for f in $BAK_FILES; do
  if _rm "$f"; then ok "Removed backup: $f"; else fail "Could not remove $f"; fi
done

# ─── 10. Data ─────────────────────────────────────────────────────────────────
if $KEEP_DATA; then
  # The manifest describes an install that no longer exists — drop just that.
  if [ -f "$MANIFEST" ]; then
    if _rm "$MANIFEST"; then ok "Removed manifest (kept the rest of $DATA_DIR)"; else fail "Could not remove $MANIFEST"; fi
  fi
  [ -f "$DATA_DIR/auto-allow" ] && { _rm "$DATA_DIR/auto-allow" && ok "Removed $DATA_DIR/auto-allow"; }
  skip "Kept analytics data in $DATA_DIR"
else
  for d in $(printf '%s' "$DATA_DIRS" | sed '/^$/d'); do
    if _rmrf "$d"; then ok "Removed data dir: $d"; else fail "Could not remove $d"; fi
  done
fi

# ─── 11. Empty dirs left behind ───────────────────────────────────────────────
_rmdir_if_empty "$CLAUDE_HOME/hooks"
_rmdir_if_empty "$COPILOT_HOME"
_rmdir_if_empty "$CLAUDE_HOME/yeet"
_rmdir_if_empty "$HOME/.yeet"
if $PURGE; then
  _rmdir_if_empty "$PROJECT_DIR/.github/hooks"
  _rmdir_if_empty "$PROJECT_DIR/.vscode"
fi

# ─── Verify ───────────────────────────────────────────────────────────────────
say ""
rule
say "${BOLD}  Verifying${RESET}"
say ""

if $DRY_RUN; then
  say "  ${YELLOW}Dry run — skipping verification.${RESET}"
  say ""
  say "  ${BOLD}Nothing was changed.${RESET} Re-run without --dry-run to remove."
  say ""
  exit 0
fi

LEFTOVER=0
left() { warn "Still present: $*"; LEFTOVER=$((LEFTOVER+1)); }

for p in $(find_on_path yeet); do
  [ -f "$p" ] && left "$p ${DIM}(still on your PATH)${RESET}"
done
for p in /usr/local/bin/yeet /opt/homebrew/bin/yeet "$HOME/.local/bin/yeet" \
         "$HOME/bin/yeet" "$HOME/go/bin/yeet" ${GOBIN_DIR:+"$GOBIN_DIR/bin/yeet"}; do
  [ -f "$p" ] && left "$p"
done
for f in "$CLAUDE_HOME/hooks/yeet-proxy.sh" "$CLAUDE_HOME/hooks/yeet-rewrite.sh" \
         "$CLAUDE_HOME/hooks/yeet-failure.sh" "$CLAUDE_HOME/yeet-awareness.md"; do
  [ -f "$f" ] && left "$f"
done
for f in "$CLAUDE_HOME/settings.json" "$CLAUDE_HOME/settings.local.json"; do
  n="$(count_yeet_hooks "$f")"
  [ "${n:-0}" -gt 0 ] && left "$n yeet hook entries in $f"
done
if $HAVE_JQ && [ -f "$CL_SETTINGS" ] && jq -e . "$CL_SETTINGS" >/dev/null 2>&1; then
  jq -e '(.permissions.allow // []) | index("Bash(yeet:*)")' "$CL_SETTINGS" >/dev/null 2>&1 \
    && left "Bash(yeet:*) in $CL_SETTINGS permissions.allow"
fi
[ -f "$CL_MD" ] && grep -q "yeet-awareness\.md" "$CL_MD" 2>/dev/null && left "yeet-awareness reference in $CL_MD"
if [ -f "$CP_INSTRUCTIONS" ]; then
  grep -qF "$YEET_MARK_BEGIN" "$CP_INSTRUCTIONS" 2>/dev/null && left "yeet block in $CP_INSTRUCTIONS"
  grep -qE 'yeet (read|grep|glob|smart|write|edit)' "$CP_INSTRUCTIONS" 2>/dev/null \
    && left "yeet instructions in $CP_INSTRUCTIONS"
fi
if ! $KEEP_DATA; then
  for d in "$DATA_DIR" "$HOME/.yeet"; do
    [ -d "$d" ] && left "$d"
  done
fi
if $PURGE; then
  for f in "$PROJECT_DIR/.github/hooks/yeet-rewrite.sh" "$PROJECT_DIR/.github/hooks/yeet-rewrite.json"; do
    [ -f "$f" ] && left "$f"
  done
fi

if [ "$LEFTOVER" -eq 0 ] && [ "$FAILURES" -eq 0 ]; then
  ok "Clean — no trace of yeet remains"
  if $KEEP_DATA; then
    say ""
    say "  ${DIM}Analytics kept at $DATA_DIR (you asked for --keep-data).${RESET}"
  fi
  say ""
  say "  ${BOLD}${GREEN}yeet removed.${RESET}"
  if [ -n "${BASH:-}" ]; then
    say "  ${DIM}Open a new shell (or run 'hash -r') to clear the cached path.${RESET}"
  fi
  say ""
  exit 0
fi

say ""
warn "$LEFTOVER item(s) still present, $FAILURES removal failure(s)."
say ""
say "  ${DIM}Most common cause: a file owned by root. Re-run with sudo:${RESET}"
say "    ${CYAN}sudo ./uninstall.sh --yes${RESET}"
say "  ${DIM}Then please report it: https://github.com/$REPO/issues/new${RESET}"
say ""
exit 1
