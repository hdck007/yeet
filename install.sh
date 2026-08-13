#!/usr/bin/env bash
# install.sh — installer for yeet
#
#   curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/hdck007/yeet/main/install.sh | bash -s -- --yes
#   ./install.sh --help
#
# Everything this script creates or modifies is recorded in an install manifest
# ($YEET_DATA_DIR/install-manifest.json) so that uninstall.sh can reverse it
# exactly — including restoring values it overwrote.
#
# Targets bash 3.2 (stock macOS /bin/bash). No associative arrays, no mapfile.

set -euo pipefail

INSTALLER_SCHEMA=2
REPO="${YEET_REPO:-hdck007/yeet}"
RAW_BASE="${YEET_RAW_BASE:-https://raw.githubusercontent.com/$REPO/main}"
CLAUDE_HOME="${YEET_CLAUDE_HOME:-$HOME/.claude}"
DATA_DIR="${YEET_DATA_DIR:-$HOME/.local/share/yeet}"
MANIFEST="$DATA_DIR/install-manifest.json"
COPILOT_HOME="${YEET_COPILOT_HOME:-$HOME/.copilot}"
STAMP="$(date -u +%Y%m%d%H%M%S)"

# ─── Output ───────────────────────────────────────────────────────────────────
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
say()  { $QUIET || echo -e "$*"; }
die()  { echo -e "  ${RED}✗${RESET} $*" >&2; exit 1; }
rule() { $QUIET || printf '  %s\n' "$(printf '─%.0s' $(seq 1 62))"; }

# ─── Defaults / flags ─────────────────────────────────────────────────────────
INSTALL_DIR="${YEET_INSTALL_DIR:-/usr/local/bin}"
WANT_VERSION="${YEET_VERSION:-}"
ASSET_DIR="${YEET_ASSET_DIR:-}"
BIN_SRC="${YEET_BIN_SRC:-}"
ASSUME_YES=false
[ "${YEET_YES:-0}" = "1" ] && ASSUME_YES=true
DO_CLAUDE=""
DO_COPILOT=""
AUTO_ALLOW=""
SKIP_BINARY=false
[ "${YEET_SKIP_BINARY:-0}" = "1" ] && SKIP_BINARY=true
FROM_SOURCE=false
DRY_RUN=false
NO_SUDO=false
[ "${YEET_NO_SUDO:-0}" = "1" ] && NO_SUDO=true

usage() {
  cat <<'EOF'
yeet installer

Usage:
  install.sh [options]
  curl -fsSL .../install.sh | bash -s -- [options]

Options:
  -y, --yes              Non-interactive. Installs binary + Claude Code
                         integration with auto-allow enabled.
      --claude           Claude Code integration (default when interactive)
      --copilot          GitHub Copilot integration
      --both             Both integrations
      --binary-only      Install the binary only, no editor integration
      --auto-allow       Let Claude Code run yeet without prompting
      --no-auto-allow    Keep Claude Code's normal permission prompts
      --dir <DIR>        Where to install the binary (default: /usr/local/bin,
                         falls back to ~/.local/bin when not writable)
      --version <TAG>    Install a specific release tag (default: latest)
      --from-source      Build from the local checkout with Go instead of
                         downloading a release binary
      --dry-run          Print every change without making it
      --quiet            Only print warnings and errors
  -h, --help             This message

Environment overrides (mostly for tests and offline installs):
  YEET_INSTALL_DIR  YEET_VERSION     YEET_RAW_BASE   YEET_ASSET_DIR
  YEET_BIN_SRC      YEET_SKIP_BINARY YEET_CLAUDE_HOME YEET_DATA_DIR
  YEET_COPILOT_HOME YEET_NO_SUDO     YEET_YES        NO_COLOR
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    -y|--yes)         ASSUME_YES=true ;;
    --claude)         DO_CLAUDE=true;  [ -z "$DO_COPILOT" ] && DO_COPILOT=false ;;
    --copilot)        DO_COPILOT=true; [ -z "$DO_CLAUDE" ]  && DO_CLAUDE=false ;;
    --both)           DO_CLAUDE=true;  DO_COPILOT=true ;;
    --binary-only)    DO_CLAUDE=false; DO_COPILOT=false ;;
    --auto-allow)     AUTO_ALLOW=true ;;
    --no-auto-allow)  AUTO_ALLOW=false ;;
    --dir)            [ $# -ge 2 ] || die "--dir needs a value"; INSTALL_DIR="$2"; shift ;;
    --dir=*)          INSTALL_DIR="${1#*=}" ;;
    --version)        [ $# -ge 2 ] || die "--version needs a value"; WANT_VERSION="$2"; shift ;;
    --version=*)      WANT_VERSION="${1#*=}" ;;
    --from-source)    FROM_SOURCE=true ;;
    --dry-run)        DRY_RUN=true ;;
    --quiet)          QUIET=true ;;
    --no-color)       BOLD=''; GREEN=''; YELLOW=''; RED=''; CYAN=''; DIM=''; RESET='' ;;
    -h|--help)        usage; exit 0 ;;
    *)                die "Unknown option: $1  (--help for usage)" ;;
  esac
  shift
done

# ─── Mutation helpers (respect --dry-run) ─────────────────────────────────────
_mkdir()  { if $DRY_RUN; then info "${DIM}[dry-run] mkdir -p $1${RESET}"; else mkdir -p "$1"; fi; }
_rm()     { if $DRY_RUN; then info "${DIM}[dry-run] rm -f $1${RESET}";    else rm -f "$1"; fi; }
_cp()     { if $DRY_RUN; then info "${DIM}[dry-run] cp $1 $2${RESET}";    else cp "$1" "$2"; fi; }
_chmodx() { $DRY_RUN || chmod +x "$1"; }

# Replace $2 with the contents of temp file $1, atomically.
_install_file() {
  local src="$1" dest="$2"
  if $DRY_RUN; then
    info "${DIM}[dry-run] write $dest${RESET}"
    rm -f "$src"
  else
    mv "$src" "$dest"
  fi
}

# ─── Rollback ─────────────────────────────────────────────────────────────────
# Backups are recorded as "original|backup" lines; on failure we put them back.
BACKUP_LOG="$(mktemp)"
NEW_FILE_LOG="$(mktemp)"
CLEAN_EXIT=false

backup_file() {
  local f="$1" b
  [ -f "$f" ] || return 0
  b="$f.yeet-bak-$STAMP"
  if $DRY_RUN; then
    printf '%s\n' "$b"
    return 0
  fi
  cp "$f" "$b"
  printf '%s|%s\n' "$f" "$b" >> "$BACKUP_LOG"
  printf '%s\n' "$b"
}

track_new() { $DRY_RUN || printf '%s\n' "$1" >> "$NEW_FILE_LOG"; }

on_exit() {
  local code=$?
  if ! $CLEAN_EXIT && [ "$code" -ne 0 ] && ! $DRY_RUN; then
    echo "" >&2
    warn "Install failed — rolling back changes..."
    if [ -s "$NEW_FILE_LOG" ]; then
      while IFS= read -r f; do [ -n "$f" ] && rm -f "$f"; done < "$NEW_FILE_LOG"
    fi
    if [ -s "$BACKUP_LOG" ]; then
      while IFS='|' read -r orig bak; do
        [ -n "$orig" ] && [ -f "$bak" ] && mv "$bak" "$orig"
      done < "$BACKUP_LOG"
    fi
    warn "Rolled back. Nothing was left half-installed."
  elif [ -s "$BACKUP_LOG" ] && ! $DRY_RUN; then
    # Success: drop the rollback copies, they are noise on disk.
    while IFS='|' read -r orig bak; do
      [ -n "$bak" ] && rm -f "$bak"
    done < "$BACKUP_LOG"
  fi
  rm -f "$BACKUP_LOG" "$NEW_FILE_LOG" ${TMP_BIN:+"$TMP_BIN"}
}
trap on_exit EXIT

ask() {
  # ask <prompt> <default> ; echoes the answer
  local prompt="$1" default="$2" reply=""
  if $ASSUME_YES || $DRY_RUN; then printf '%s' "$default"; return 0; fi
  if [ -t 0 ]; then
    read -r -p "$prompt" reply || true
  elif [ -e /dev/tty ]; then
    read -r -p "$prompt" reply </dev/tty || true
  fi
  printf '%s' "${reply:-$default}"
}

is_yes() { case "$1" in [Yy]|[Yy][Ee][Ss]) return 0 ;; *) return 1 ;; esac; }

# Walk PATH by hand — `command -v` only ever reports the first hit.
find_on_path() {
  local name="$1" d oldifs="$IFS"
  IFS=:
  for d in $PATH; do
    [ -n "$d" ] || continue
    if [ -f "$d/$name" ]; then IFS="$oldifs"; printf '%s\n' "$d/$name"; IFS=:; fi
  done
  IFS="$oldifs"
}

json_array() {
  if [ $# -eq 0 ]; then echo '[]'; return; fi
  printf '%s\n' "$@" | jq -R . | jq -s -c .
}

sha256_of() {
  if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}'
  elif command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else echo ""; fi
}

# ─── Banner ───────────────────────────────────────────────────────────────────
say ""
say "${BOLD}  yeet installer${RESET}"
say "  ${DIM}https://github.com/$REPO${RESET}"
$DRY_RUN && say "  ${YELLOW}dry run — no changes will be made${RESET}"
say ""

# ─── 1. Platform ──────────────────────────────────────────────────────────────
OS="$(uname -s)"
ARCH="$(uname -m)"
case "$OS" in
  Darwin) ASSET="yeet-darwin-universal" ;;
  Linux)
    case "$ARCH" in
      x86_64|amd64)  ASSET="yeet-linux-amd64" ;;
      aarch64|arm64) ASSET="yeet-linux-arm64" ;;
      *) die "Unsupported architecture: $ARCH — build from source: https://github.com/$REPO#build-from-source" ;;
    esac
    ;;
  *) die "Unsupported OS: $OS — build from source: https://github.com/$REPO#build-from-source" ;;
esac

# Local asset dir: when the script is run from a checkout, prefer the files on
# disk over the network so `./install.sh` works offline and always matches HEAD.
if [ -z "$ASSET_DIR" ] && [ "${BASH_SOURCE[0]:-}" != "" ] && [ -f "${BASH_SOURCE[0]}" ]; then
  _sd="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  [ -f "$_sd/hooks/yeet-proxy.sh" ] && ASSET_DIR="$_sd"
fi

# ─── 2. Preflight ─────────────────────────────────────────────────────────────
need_curl=true
[ -n "$ASSET_DIR" ] && [ -n "$BIN_SRC" ] && need_curl=false
$SKIP_BINARY && [ -n "$ASSET_DIR" ] && need_curl=false
$FROM_SOURCE && [ -n "$ASSET_DIR" ] && need_curl=false
if $need_curl && ! command -v curl >/dev/null 2>&1; then
  die "curl is required. Install curl and re-run."
fi

if ! command -v jq >/dev/null 2>&1; then
  info "jq not found — installing (required to edit Claude Code settings safely)..."
  if $DRY_RUN; then
    info "${DIM}[dry-run] install jq${RESET}"
  elif [ "$OS" = "Darwin" ] && command -v brew >/dev/null 2>&1; then
    brew install jq >/dev/null 2>&1 || die "brew install jq failed"
  elif command -v apt-get >/dev/null 2>&1; then
    sudo apt-get update -qq >/dev/null 2>&1 || true
    sudo apt-get install -y jq >/dev/null 2>&1 || die "apt-get install jq failed"
  elif command -v dnf >/dev/null 2>&1; then
    sudo dnf install -y jq >/dev/null 2>&1 || die "dnf install jq failed"
  elif command -v yum >/dev/null 2>&1; then
    sudo yum install -y jq >/dev/null 2>&1 || die "yum install jq failed"
  elif command -v apk >/dev/null 2>&1; then
    sudo apk add jq >/dev/null 2>&1 || die "apk add jq failed"
  elif command -v pacman >/dev/null 2>&1; then
    sudo pacman -S --noconfirm jq >/dev/null 2>&1 || die "pacman -S jq failed"
  else
    die "Cannot auto-install jq. Install it manually: https://jqlang.github.io/jq/download/"
  fi
  command -v jq >/dev/null 2>&1 || $DRY_RUN || die "jq install reported success but jq is not on PATH"
  ok "jq installed"
fi

# ─── 3. Pick integrations ─────────────────────────────────────────────────────
if [ -z "$DO_CLAUDE" ] && [ -z "$DO_COPILOT" ]; then
  if $ASSUME_YES || $DRY_RUN; then
    DO_CLAUDE=true; DO_COPILOT=false
  else
    say ""
    say "${BOLD}  Editor integration${RESET}"
    say ""
    say "  ${CYAN}1)${RESET} Claude Code            ${DIM}(recommended)${RESET}"
    say "  ${CYAN}2)${RESET} GitHub Copilot"
    say "  ${CYAN}3)${RESET} Both"
    say "  ${CYAN}4)${RESET} Neither — just the binary"
    say ""
    CHOICE="$(ask "  Choice [1]: " 1)"
    case "$CHOICE" in
      1) DO_CLAUDE=true;  DO_COPILOT=false ;;
      2) DO_CLAUDE=false; DO_COPILOT=true  ;;
      3) DO_CLAUDE=true;  DO_COPILOT=true  ;;
      4) DO_CLAUDE=false; DO_COPILOT=false ;;
      *) warn "Invalid choice '$CHOICE' — defaulting to Claude Code"; DO_CLAUDE=true; DO_COPILOT=false ;;
    esac
  fi
fi
[ -z "$DO_CLAUDE" ]  && DO_CLAUDE=false
[ -z "$DO_COPILOT" ] && DO_COPILOT=false

# ─── 4. Clean out any previous install (incl. legacy layouts) ─────────────────
# Ensures a repeat install never stacks duplicate hooks and never leaves a
# stale hook script pointing at an old path.
say ""
say "${BOLD}  Checking for an existing install${RESET}"

PRIOR_VERSION=""
if [ -f "$MANIFEST" ]; then
  PRIOR_VERSION="$(jq -r '.yeet_version // ""' "$MANIFEST" 2>/dev/null || echo "")"
  [ -n "$PRIOR_VERSION" ] && info "Found previous install ($PRIOR_VERSION) — upgrading in place"
fi

# Strip yeet hook entries from a settings file: both current (_yeet marker) and
# legacy (unmarked, identified by content) shapes.
strip_yeet_hooks() {
  local file="$1" tmp removed
  [ -f "$file" ] || return 0
  jq -e . "$file" >/dev/null 2>&1 || { warn "$file is not valid JSON — leaving it alone"; return 0; }
  removed="$(jq '
    [ (.hooks.PreToolUse // [])[] | select(
        ._yeet == true
        or ((.matcher // "") as $m
            | ($m | test("^(Read|Glob|Grep|Write|Edit|MultiEdit|NotebookEdit)$"))
              and ((.hooks // []) | map(.command // "") | any(test("yeet"))))
        or ((.matcher // "") == "Bash"
            and ((.hooks // []) | map(.command // "")
                 | any(test("yeet-proxy|yeet-rewrite|yeet rewrite"))))
      ) ] | length' "$file" 2>/dev/null || echo 0)"
  [ "${removed:-0}" -eq 0 ] && return 0
  backup_file "$file" >/dev/null
  if $DRY_RUN; then
    info "${DIM}[dry-run] remove $removed stale yeet hook entries from $file${RESET}"
    return 0
  fi
  tmp="$(mktemp)"
  jq '
    if .hooks.PreToolUse then
      .hooks.PreToolUse |= map(select(
        (._yeet == true
         or ((.matcher // "") as $m
             | ($m | test("^(Read|Glob|Grep|Write|Edit|MultiEdit|NotebookEdit)$"))
               and ((.hooks // []) | map(.command // "") | any(test("yeet"))))
         or ((.matcher // "") == "Bash"
             and ((.hooks // []) | map(.command // "")
                  | any(test("yeet-proxy|yeet-rewrite|yeet rewrite"))))) | not))
    else . end
    | if (.hooks.PreToolUse? | type == "array") and (.hooks.PreToolUse | length) == 0
      then del(.hooks.PreToolUse) else . end
    | if (.hooks? | type == "object") and (.hooks | length) == 0
      then del(.hooks) else . end
  ' "$file" > "$tmp" && mv "$tmp" "$file"
  ok "Removed $removed stale yeet hook entries from $file"
}

for f in "$CLAUDE_HOME/settings.json" "$CLAUDE_HOME/settings.local.json"; do
  strip_yeet_hooks "$f"
done

# Legacy hook scripts and awareness copies that older installers left behind.
for stale in \
  "$CLAUDE_HOME/hooks/yeet-rewrite.sh" \
  "$CLAUDE_HOME/hooks/yeet-failure.sh" \
  "$CLAUDE_HOME/yeet/yeet-proxy.sh" \
  "$HOME/.yeet/yeet-proxy.sh" \
; do
  if [ -f "$stale" ]; then
    _rm "$stale"
    ok "Removed legacy file: $stale"
  fi
done
for staledir in "$CLAUDE_HOME/yeet" "$HOME/.yeet"; do
  if [ -d "$staledir" ] && [ -z "$(ls -A "$staledir" 2>/dev/null)" ]; then
    $DRY_RUN || rmdir "$staledir" 2>/dev/null || true
    ok "Removed legacy dir: $staledir"
  fi
done

# ─── 5. Binary ────────────────────────────────────────────────────────────────
VERSION="${WANT_VERSION:-}"
BINARY_PATH=""
USED_SUDO=false
BINARY_SHA=""
TMP_BIN=""

resolve_install_dir() {
  # Prefer the requested dir; fall back to ~/.local/bin when it is neither
  # writable nor sudo-able.
  if [ -d "$INSTALL_DIR" ] && [ -w "$INSTALL_DIR" ]; then return 0; fi
  if [ ! -d "$INSTALL_DIR" ]; then
    local parent; parent="$(dirname "$INSTALL_DIR")"
    if [ -w "$parent" ]; then _mkdir "$INSTALL_DIR"; return 0; fi
  fi
  if ! $NO_SUDO && command -v sudo >/dev/null 2>&1; then
    USED_SUDO=true
    info "$INSTALL_DIR needs elevated permissions — you may be prompted for your password"
    return 0
  fi
  INSTALL_DIR="$HOME/.local/bin"
  _mkdir "$INSTALL_DIR"
  warn "Falling back to $INSTALL_DIR (no write access and no sudo)"
  return 0
}

if $SKIP_BINARY; then
  info "Skipping binary install (YEET_SKIP_BINARY=1)"
  BINARY_PATH="$(command -v yeet 2>/dev/null || echo "")"
else
  say ""
  say "${BOLD}  Binary${RESET}"
  resolve_install_dir
  DEST="$INSTALL_DIR/yeet"
  TMP_BIN="$(mktemp)"

  if $FROM_SOURCE; then
    command -v go >/dev/null 2>&1 || die "--from-source needs Go: https://go.dev/dl/"
    [ -n "$ASSET_DIR" ] || die "--from-source must be run from a yeet checkout"
    command -v gcc >/dev/null 2>&1 || command -v clang >/dev/null 2>&1 \
      || die "--from-source needs a C compiler (CGO/sqlite). macOS: xcode-select --install"
    VERSION="${VERSION:-$(cat "$ASSET_DIR/VERSION" 2>/dev/null || echo dev)}"
    info "Building from source ($VERSION)..."
    if $DRY_RUN; then
      info "${DIM}[dry-run] go build -o $TMP_BIN ./cmd/yeet/${RESET}"
      : > "$TMP_BIN"
    else
      ( cd "$ASSET_DIR" && CGO_ENABLED=1 go build \
          -ldflags "-X github.com/hdck007/yeet/internal/cli.Version=$VERSION" \
          -o "$TMP_BIN" ./cmd/yeet/ ) || die "build failed"
    fi
  elif [ -n "$BIN_SRC" ]; then
    [ -f "$BIN_SRC" ] || die "YEET_BIN_SRC not found: $BIN_SRC"
    info "Using local binary: $BIN_SRC"
    cp "$BIN_SRC" "$TMP_BIN"
    VERSION="${VERSION:-local}"
  else
    if [ -z "$VERSION" ] || [ "$VERSION" = "latest" ]; then
      info "Resolving latest release..."
      VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null \
        | jq -r '.tag_name // empty' 2>/dev/null || echo "")"
      if [ -z "$VERSION" ]; then
        # API rate-limited or unavailable — follow the /releases/latest redirect.
        VERSION="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
          "https://github.com/$REPO/releases/latest" 2>/dev/null | sed -n 's|.*/tag/||p')"
      fi
      [ -n "$VERSION" ] || die "Could not determine the latest version. See https://github.com/$REPO/releases"
    fi
    ok "Version: $VERSION"
    URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
    info "Downloading $ASSET..."
    curl -fsSL "$URL" -o "$TMP_BIN" || die "Download failed: $URL
  Check that $ASSET exists for $VERSION at https://github.com/$REPO/releases/tag/$VERSION"
    # Guard against a captured HTML error page landing here.
    if [ "$(wc -c < "$TMP_BIN" | tr -d ' ')" -lt 100000 ]; then
      die "Downloaded file is too small to be the yeet binary — the release asset may be missing"
    fi
  fi

  $DRY_RUN || chmod +x "$TMP_BIN"
  [ "$OS" = "Darwin" ] && ! $DRY_RUN && xattr -d com.apple.quarantine "$TMP_BIN" 2>/dev/null || true

  # Prove the binary runs on this machine *before* putting it on PATH.
  if ! $DRY_RUN; then
    if ! "$TMP_BIN" version >/dev/null 2>&1; then
      die "The downloaded binary does not run on this machine ($OS/$ARCH).
  Try building from source: git clone https://github.com/$REPO && cd yeet && ./install.sh --from-source"
    fi
  fi

  BINARY_SHA="$(sha256_of "$TMP_BIN")"
  info "Installing to $DEST..."
  if $DRY_RUN; then
    info "${DIM}[dry-run] install $TMP_BIN -> $DEST${RESET}"
  elif $USED_SUDO; then
    sudo mkdir -p "$INSTALL_DIR"
    sudo install -m 0755 "$TMP_BIN" "$DEST" || die "Failed to install to $DEST"
    rm -f "$TMP_BIN"
  else
    install -m 0755 "$TMP_BIN" "$DEST" || die "Failed to install to $DEST"
    rm -f "$TMP_BIN"
  fi
  TMP_BIN=""
  BINARY_PATH="$DEST"
  ok "Binary: $DEST  ($($DRY_RUN && echo "$VERSION" || "$DEST" version 2>/dev/null || echo "$VERSION"))"

  # ── PATH sanity: make sure `yeet` actually resolves to what we installed ────
  if ! $DRY_RUN; then
    case ":$PATH:" in
      *":$INSTALL_DIR:"*) ;;
      *)
        warn "$INSTALL_DIR is not on your PATH. Add it:"
        warn "  bash/zsh:  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc"
        warn "  fish:      fish_add_path $INSTALL_DIR"
        ;;
    esac

    # Remove shadowing copies of yeet earlier on PATH — otherwise the install
    # silently "works" while an old build keeps answering.
    SHADOWS=""
    for cand in $(find_on_path yeet); do
      [ "$cand" = "$DEST" ] && continue
      [ -f "$cand" ] || continue
      # Only ever touch something that identifies itself as yeet.
      if "$cand" version 2>/dev/null | grep -qi yeet; then
        SHADOWS="$SHADOWS $cand"
      fi
    done
    for extra in "/usr/local/bin/yeet" "/opt/homebrew/bin/yeet" "$HOME/.local/bin/yeet" \
                 "$HOME/bin/yeet" "$HOME/go/bin/yeet" "$(go env GOPATH 2>/dev/null || echo /nonexistent)/bin/yeet"; do
      [ "$extra" = "$DEST" ] && continue
      [ -f "$extra" ] || continue
      case " $SHADOWS " in *" $extra "*) continue ;; esac
      "$extra" version 2>/dev/null | grep -qi yeet && SHADOWS="$SHADOWS $extra"
    done

    if [ -n "$SHADOWS" ]; then
      warn "Other yeet binaries found:"
      for s in $SHADOWS; do warn "    $s"; done
      RMDUP="$(ask "  Remove them so '$DEST' is the only yeet? [Y/n]: " Y)"
      if is_yes "$RMDUP"; then
        for s in $SHADOWS; do
          if [ -w "$(dirname "$s")" ]; then rm -f "$s" && ok "Removed duplicate: $s"
          elif ! $NO_SUDO && command -v sudo >/dev/null 2>&1; then sudo rm -f "$s" && ok "Removed duplicate: $s"
          else warn "Could not remove $s (no permission)"; fi
        done
        hash -r 2>/dev/null || true
      else
        warn "Left in place — 'yeet' may resolve to an older build"
      fi
    fi

    RESOLVED="$(command -v yeet 2>/dev/null || echo "")"
    if [ -n "$RESOLVED" ] && [ "$RESOLVED" != "$DEST" ]; then
      warn "'yeet' currently resolves to $RESOLVED, not $DEST"
    fi
  fi
fi

YEET_BIN="${BINARY_PATH:-$(command -v yeet 2>/dev/null || echo yeet)}"

# ─── 6. Claude Code ───────────────────────────────────────────────────────────
CL_SETTINGS="$CLAUDE_HOME/settings.json"
CL_SETTINGS_EXISTED=false
CL_SETTINGS_BACKUP=""
CL_AUTOCOMPACT_PRIOR="null"
CL_PERMS_ADDED="[]"
CL_MD="$CLAUDE_HOME/CLAUDE.md"
CL_MD_EXISTED=false
CL_MD_BACKUP=""
CL_AWARENESS="$CLAUDE_HOME/yeet-awareness.md"
CL_PROXY="$CLAUDE_HOME/hooks/yeet-proxy.sh"
CL_HOOK_COUNT=0
CREATED_FILES=""
CREATED_DIRS=""

add_created_file() { CREATED_FILES="$CREATED_FILES
$1"; track_new "$1"; }
add_created_dir()  { CREATED_DIRS="$CREATED_DIRS
$1"; }

fetch_to_tmp() {
  # fetch_to_tmp <repo-relative-path> ; echoes the temp file path
  local rel="$1" tmp
  tmp="$(mktemp)"
  if [ -n "$ASSET_DIR" ] && [ -f "$ASSET_DIR/$rel" ]; then
    cp "$ASSET_DIR/$rel" "$tmp"
  else
    curl -fsSL "$RAW_BASE/$rel" -o "$tmp" || { rm -f "$tmp"; die "Failed to fetch $rel from $RAW_BASE"; }
    [ -s "$tmp" ] || { rm -f "$tmp"; die "Fetched $rel but it was empty"; }
  fi
  printf '%s' "$tmp"
}

fetch_asset() {
  # fetch_asset <repo-relative-path> <dest>
  local rel="$1" dest="$2" tmp
  tmp="$(fetch_to_tmp "$rel")"
  [ -f "$dest" ] || track_new "$dest"
  _install_file "$tmp" "$dest"
}

# Copilot has one global instructions file that users may already own, so the
# yeet block is delimited and merged in rather than overwriting the file.
YEET_MARK_BEGIN='<!-- yeet:begin - managed by the yeet installer, do not edit -->'
YEET_MARK_END='<!-- yeet:end -->'

strip_yeet_block() {
  # stdin → stdout, minus any previously installed yeet block
  awk -v b="$YEET_MARK_BEGIN" -v e="$YEET_MARK_END" '
    index($0, b) == 1 { skip = 1; next }
    index($0, e) == 1 { skip = 0; next }
    !skip { print }
  '
}

# The hook block, built with jq so quoting is never hand-rolled.
yeet_hooks_json() {
  jq -n --arg cmd "bash \"$CL_PROXY\"" --argjson schema "$INSTALLER_SCHEMA" '
    def block(msg): {"type":"command","command":("echo " + (msg|@sh) + " >&2; exit 2")};
    def entry(m; msg): {"matcher":m, "_yeet":true, "_yeetSchema":$schema, "hooks":[block(msg)]};
    [
      entry("Read";  "BLOCKED: Use `yeet read <file>` (or `yeet smart <file>`) instead of the Read tool."),
      entry("Glob";  "BLOCKED: Use `yeet glob \"<pattern>\" [path]` instead of the Glob tool."),
      entry("Grep";  "BLOCKED: Use `yeet grep \"<pattern>\" [path]` instead of the Grep tool."),
      entry("Write"; "BLOCKED: Use `cat <<'"'"'EOF'"'"' | yeet write <file>` instead of the Write tool."),
      entry("Edit";  "BLOCKED: Use `yeet edit <file> --old \"...\" --new \"...\"` instead of the Edit tool."),
      {"matcher":"Bash", "_yeet":true, "_yeetSchema":$schema,
       "hooks":[{"type":"command","command":$cmd}]}
    ]'
}

if $DO_CLAUDE; then
  say ""
  say "${BOLD}  Claude Code${RESET}"

  [ -d "$CLAUDE_HOME" ]         || add_created_dir "$CLAUDE_HOME"
  [ -d "$CLAUDE_HOME/hooks" ]   || add_created_dir "$CLAUDE_HOME/hooks"
  _mkdir "$CLAUDE_HOME/hooks"

  # 6a. proxy hook script
  fetch_asset "hooks/yeet-proxy.sh" "$CL_PROXY"
  _chmodx "$CL_PROXY"
  add_created_file "$CL_PROXY"
  ok "Proxy hook → $CL_PROXY"

  # 6b. awareness instructions (always refreshed so upgrades stay current)
  fetch_asset "hooks/claude/yeet-awareness.md" "$CL_AWARENESS"
  add_created_file "$CL_AWARENESS"
  ok "Awareness → $CL_AWARENESS"

  # 6c. auto-allow decision (needed before writing permissions)
  if [ -z "$AUTO_ALLOW" ]; then
    if $ASSUME_YES || $DRY_RUN; then
      AUTO_ALLOW=true
    else
      say ""
      say "  ${DIM}Auto-allow lets Claude Code run yeet commands without asking${RESET}"
      say "  ${DIM}permission each time. Change later: yeet auto-allow [true|false]${RESET}"
      A="$(ask "  Enable auto-allow? [Y/n]: " Y)"
      is_yes "$A" && AUTO_ALLOW=true || AUTO_ALLOW=false
    fi
  fi

  # 6d. settings.json
  HOOKS_JSON="$(yeet_hooks_json)"
  CL_HOOK_COUNT="$(printf '%s' "$HOOKS_JSON" | jq 'length')"

  if [ -f "$CL_SETTINGS" ]; then
    CL_SETTINGS_EXISTED=true
    jq -e . "$CL_SETTINGS" >/dev/null 2>&1 \
      || die "$CL_SETTINGS is not valid JSON. Fix or move it, then re-run."
    CL_SETTINGS_BACKUP="$(backup_file "$CL_SETTINGS")"
    CL_AUTOCOMPACT_PRIOR="$(jq -c '.autoCompactThreshold // null' "$CL_SETTINGS")"
  else
    [ -d "$CLAUDE_HOME" ] || _mkdir "$CLAUDE_HOME"
    track_new "$CL_SETTINGS"
  fi

  # Which permission entries are we adding that were not already there?
  PERM_WANT="Bash(yeet:*)"
  if [ "$AUTO_ALLOW" = "true" ]; then
    if [ -f "$CL_SETTINGS" ] \
       && jq -e --arg p "$PERM_WANT" '(.permissions.allow // []) | index($p)' "$CL_SETTINGS" >/dev/null 2>&1; then
      CL_PERMS_ADDED='[]'
    else
      CL_PERMS_ADDED="$(json_array "$PERM_WANT")"
    fi
  fi

  if $DRY_RUN; then
    info "${DIM}[dry-run] merge $CL_HOOK_COUNT yeet hooks into $CL_SETTINGS${RESET}"
  else
    TMP_S="$(mktemp)"
    BASE_S="$CL_SETTINGS"
    [ -f "$BASE_S" ] || { echo '{}' > "$TMP_S.base"; BASE_S="$TMP_S.base"; }
    jq --argjson hooks "$HOOKS_JSON" \
       --argjson addperms "$CL_PERMS_ADDED" '
      .hooks //= {} |
      .hooks.PreToolUse //= [] |
      # yeet hooks go first so blockers win over anything else the user has
      .hooks.PreToolUse = ($hooks + .hooks.PreToolUse) |
      .autoCompactThreshold = 100000 |
      if ($addperms | length) > 0 then
        .permissions //= {} |
        .permissions.allow //= [] |
        .permissions.allow = (.permissions.allow + $addperms | unique)
      else . end
    ' "$BASE_S" > "$TMP_S" || die "Failed to update $CL_SETTINGS"
    jq -e . "$TMP_S" >/dev/null 2>&1 || die "Refusing to write invalid JSON to $CL_SETTINGS"
    mv "$TMP_S" "$CL_SETTINGS"
    rm -f "$TMP_S.base"
  fi
  if $CL_SETTINGS_EXISTED; then
    ok "Updated $CL_SETTINGS  (+$CL_HOOK_COUNT hooks)"
  else
    ok "Created $CL_SETTINGS  ($CL_HOOK_COUNT hooks)"
  fi

  # 6e. CLAUDE.md — awareness must be the first line so it outranks other rules
  AWARENESS_REF="@yeet-awareness.md"
  if [ ! -f "$CL_MD" ]; then
    track_new "$CL_MD"
    if $DRY_RUN; then
      info "${DIM}[dry-run] create $CL_MD with $AWARENESS_REF${RESET}"
    else
      printf '%s\n' "$AWARENESS_REF" > "$CL_MD"
    fi
    ok "Created $CL_MD"
  else
    CL_MD_EXISTED=true
    if [ "$(head -1 "$CL_MD")" = "$AWARENESS_REF" ]; then
      ok "$CL_MD already leads with $AWARENESS_REF"
    else
      CL_MD_BACKUP="$(backup_file "$CL_MD")"
      if $DRY_RUN; then
        info "${DIM}[dry-run] hoist $AWARENESS_REF to the top of $CL_MD${RESET}"
      else
        TMP_MD="$(mktemp)"
        printf '%s\n' "$AWARENESS_REF" > "$TMP_MD"
        grep -vxF "$AWARENESS_REF" "$CL_MD" >> "$TMP_MD" || true
        mv "$TMP_MD" "$CL_MD"
      fi
      ok "Put $AWARENESS_REF at the top of $CL_MD"
    fi
  fi

  # 6f. auto-allow flag file
  if [ "$AUTO_ALLOW" = "true" ]; then
    _mkdir "$DATA_DIR"
    if $DRY_RUN; then
      info "${DIM}[dry-run] yeet auto-allow true${RESET}"
    else
      printf 'true\n' > "$DATA_DIR/auto-allow"
    fi
    ok "Auto-allow enabled"
  else
    if ! $DRY_RUN && [ -f "$DATA_DIR/auto-allow" ]; then
      printf 'false\n' > "$DATA_DIR/auto-allow"
    fi
    ok "Auto-allow disabled  ${DIM}(yeet auto-allow true to enable)${RESET}"
  fi
fi

# ─── 7. GitHub Copilot ────────────────────────────────────────────────────────
CP_INSTRUCTIONS="$COPILOT_HOME/copilot-instructions.md"
CP_INSTRUCTIONS_EXISTED=false
CP_INSTRUCTIONS_BACKUP=""
CP_PROJECT="$PWD"
CP_HOOKS=""
CP_VSCODE="$PWD/.vscode/settings.json"
CP_VSCODE_CREATED=false

if $DO_COPILOT; then
  say ""
  say "${BOLD}  GitHub Copilot${RESET}"

  [ -d "$COPILOT_HOME" ] || add_created_dir "$COPILOT_HOME"
  _mkdir "$COPILOT_HOME"

  CP_BLOCK="$(fetch_to_tmp "hooks/copilot/yeet-awareness.md")"
  if [ -f "$CP_INSTRUCTIONS" ]; then
    CP_INSTRUCTIONS_EXISTED=true
    CP_INSTRUCTIONS_BACKUP="$(backup_file "$CP_INSTRUCTIONS")"
  else
    track_new "$CP_INSTRUCTIONS"
    add_created_file "$CP_INSTRUCTIONS"
  fi
  if $DRY_RUN; then
    info "${DIM}[dry-run] merge yeet block into $CP_INSTRUCTIONS${RESET}"
    rm -f "$CP_BLOCK"
  else
    CP_TMP="$(mktemp)"
    if [ -f "$CP_INSTRUCTIONS" ]; then
      # Drop any block from a previous install, keep everything the user wrote.
      strip_yeet_block < "$CP_INSTRUCTIONS" > "$CP_TMP"
      # Trim trailing blank lines so repeat installs do not grow the file.
      awk 'BEGIN{n=0} {lines[NR]=$0} END{for(i=NR;i>0;i--){if(lines[i] ~ /[^[:space:]]/){n=i;break}} for(i=1;i<=n;i++) print lines[i]}' \
        "$CP_TMP" > "$CP_TMP.trim" && mv "$CP_TMP.trim" "$CP_TMP"
      [ -s "$CP_TMP" ] && printf '\n' >> "$CP_TMP"
    fi
    {
      printf '%s\n' "$YEET_MARK_BEGIN"
      cat "$CP_BLOCK"
      printf '%s\n' "$YEET_MARK_END"
    } >> "$CP_TMP"
    mv "$CP_TMP" "$CP_INSTRUCTIONS"
    rm -f "$CP_BLOCK"
  fi
  if $CP_INSTRUCTIONS_EXISTED; then
    ok "Merged yeet block into $CP_INSTRUCTIONS ${DIM}(your content kept)${RESET}"
  else
    ok "Instructions → $CP_INSTRUCTIONS"
  fi

  GH_HOOKS="$PWD/.github/hooks"
  [ -d "$PWD/.github" ]  || add_created_dir "$PWD/.github"
  [ -d "$GH_HOOKS" ]     || add_created_dir "$GH_HOOKS"
  _mkdir "$GH_HOOKS"
  fetch_asset ".github/hooks/yeet-rewrite.sh"   "$GH_HOOKS/yeet-rewrite.sh"
  _chmodx "$GH_HOOKS/yeet-rewrite.sh"
  fetch_asset ".github/hooks/yeet-rewrite.json" "$GH_HOOKS/yeet-rewrite.json"
  add_created_file "$GH_HOOKS/yeet-rewrite.sh"
  add_created_file "$GH_HOOKS/yeet-rewrite.json"
  CP_HOOKS="$GH_HOOKS/yeet-rewrite.sh
$GH_HOOKS/yeet-rewrite.json"
  ok "Rewrite hook → $GH_HOOKS/"

  _mkdir "$PWD/.vscode"
  if [ -f "$CP_VSCODE" ]; then
    if command -v jq >/dev/null 2>&1 && jq -e . "$CP_VSCODE" >/dev/null 2>&1; then
      backup_file "$CP_VSCODE" >/dev/null
      if $DRY_RUN; then
        info "${DIM}[dry-run] merge Copilot agent keys into $CP_VSCODE${RESET}"
      else
        TMP_V="$(mktemp)"
        jq '."github.copilot.chat.agent.enabled" = true
          | ."github.copilot.chat.agent.runTasks" = true' "$CP_VSCODE" > "$TMP_V" && mv "$TMP_V" "$CP_VSCODE"
      fi
      ok "Merged Copilot keys into $CP_VSCODE"
    else
      warn "$CP_VSCODE exists but is not plain JSON (comments?) — add manually:"
      warn '    "github.copilot.chat.agent.enabled": true'
      warn '    "github.copilot.chat.agent.runTasks": true'
    fi
  else
    CP_VSCODE_CREATED=true
    track_new "$CP_VSCODE"
    if $DRY_RUN; then
      info "${DIM}[dry-run] create $CP_VSCODE${RESET}"
    else
      cat > "$CP_VSCODE" <<'JSON'
{
  "github.copilot.chat.agent.enabled": true,
  "github.copilot.chat.agent.runTasks": true,
  "github.copilot.chat.useProjectTemplates": true
}
JSON
    fi
    add_created_file "$CP_VSCODE"
    ok "VS Code settings → $CP_VSCODE"
  fi
  say "  ${DIM}Commit .github/hooks/ so teammates get the rewrite hook too.${RESET}"
fi

# ─── 8. Manifest ──────────────────────────────────────────────────────────────
INTEGRATIONS=""
$DO_CLAUDE  && INTEGRATIONS="$INTEGRATIONS claude"
$DO_COPILOT && INTEGRATIONS="$INTEGRATIONS copilot"

if ! $DRY_RUN; then
  mkdir -p "$DATA_DIR"
  # shellcheck disable=SC2046  # word splitting is intentional for list building
  jq -n \
    --argjson schema "$INSTALLER_SCHEMA" \
    --arg     at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg     version "${VERSION:-unknown}" \
    --arg     platform "$OS/$ARCH" \
    --argjson integrations "$(json_array $INTEGRATIONS)" \
    --arg     bin "${BINARY_PATH:-}" \
    --arg     bin_sha "${BINARY_SHA:-}" \
    --argjson bin_sudo "$($USED_SUDO && echo true || echo false)" \
    --argjson skip_binary "$($SKIP_BINARY && echo true || echo false)" \
    --arg     data_dir "$DATA_DIR" \
    --argjson auto_allow "${AUTO_ALLOW:-false}" \
    --argjson created_files "$(json_array $(printf '%s' "$CREATED_FILES" | sed '/^$/d'))" \
    --argjson created_dirs  "$(json_array $(printf '%s' "$CREATED_DIRS"  | sed '/^$/d'))" \
    --argjson do_claude "$DO_CLAUDE" \
    --arg     cl_settings "$CL_SETTINGS" \
    --argjson cl_settings_existed "$CL_SETTINGS_EXISTED" \
    --argjson cl_autocompact_prior "$CL_AUTOCOMPACT_PRIOR" \
    --argjson cl_perms_added "$CL_PERMS_ADDED" \
    --argjson cl_hook_count "$CL_HOOK_COUNT" \
    --arg     cl_md "$CL_MD" \
    --argjson cl_md_existed "$CL_MD_EXISTED" \
    --arg     cl_awareness "$CL_AWARENESS" \
    --arg     cl_proxy "$CL_PROXY" \
    --argjson do_copilot "$DO_COPILOT" \
    --arg     cp_instructions "$CP_INSTRUCTIONS" \
    --argjson cp_instructions_existed "$CP_INSTRUCTIONS_EXISTED" \
    --arg     cp_project "$CP_PROJECT" \
    --argjson cp_hooks "$(json_array $(printf '%s' "$CP_HOOKS" | sed '/^$/d'))" \
    --arg     cp_vscode "$CP_VSCODE" \
    --argjson cp_vscode_created "$CP_VSCODE_CREATED" \
    '{
      schema: $schema,
      installer: "install.sh",
      installed_at: $at,
      yeet_version: $version,
      platform: $platform,
      integrations: $integrations,
      binary: { path: $bin, sha256: $bin_sha, used_sudo: $bin_sudo, skipped: $skip_binary },
      data_dir: $data_dir,
      auto_allow: $auto_allow,
      created_files: $created_files,
      created_dirs: $created_dirs,
      claude: (if $do_claude then {
        enabled: true,
        settings_file: $cl_settings,
        settings_existed: $cl_settings_existed,
        auto_compact_prior: $cl_autocompact_prior,
        permissions_allow_added: $cl_perms_added,
        hook_count: $cl_hook_count,
        claude_md: $cl_md,
        claude_md_existed: $cl_md_existed,
        awareness: $cl_awareness,
        proxy: $cl_proxy
      } else { enabled: false } end),
      copilot: (if $do_copilot then {
        enabled: true,
        instructions: $cp_instructions,
        instructions_existed: $cp_instructions_existed,
        project_dir: $cp_project,
        github_hooks: $cp_hooks,
        vscode_settings: $cp_vscode,
        vscode_created: $cp_vscode_created
      } else { enabled: false } end)
    }' > "$MANIFEST" || die "Failed to write manifest $MANIFEST"
  ok "Manifest → $MANIFEST"
else
  info "${DIM}[dry-run] write manifest $MANIFEST${RESET}"
fi

# ─── 9. Verify ────────────────────────────────────────────────────────────────
say ""
rule
say "${BOLD}  Verifying${RESET}"

FAILED=0
check() {
  # check <label> <condition-exit-code>
  if [ "$2" -eq 0 ]; then ok "$1"; else warn "FAILED: $1"; FAILED=$((FAILED+1)); fi
}

if $DRY_RUN; then
  info "${DIM}[dry-run] skipping verification${RESET}"
else
  if ! $SKIP_BINARY; then
    "$BINARY_PATH" version >/dev/null 2>&1; check "binary runs: $BINARY_PATH" $?
    [ -x "$BINARY_PATH" ]; check "binary is executable" $?
  fi
  if $DO_CLAUDE; then
    [ -x "$CL_PROXY" ]; check "proxy hook is executable" $?
    [ -s "$CL_AWARENESS" ]; check "awareness file is non-empty" $?
    jq -e . "$CL_SETTINGS" >/dev/null 2>&1; check "settings.json is valid JSON" $?
    N="$(jq '[.hooks.PreToolUse[]? | select(._yeet == true)] | length' "$CL_SETTINGS" 2>/dev/null || echo 0)"
    [ "$N" -eq "$CL_HOOK_COUNT" ]; check "settings.json has exactly $CL_HOOK_COUNT yeet hooks (found $N)" $?
    HOOKPATH="$(jq -r '[.hooks.PreToolUse[]? | select(._yeet == true and .matcher == "Bash")
                        | .hooks[0].command] | first // ""' "$CL_SETTINGS" 2>/dev/null)"
    case "$HOOKPATH" in *"$CL_PROXY"*) true ;; *) false ;; esac
    check "Bash hook points at $CL_PROXY" $?
    [ "$(head -1 "$CL_MD")" = "@yeet-awareness.md" ]; check "CLAUDE.md leads with @yeet-awareness.md" $?
    # The hook must survive a real invocation with an empty payload.
    printf '%s' '{"tool_name":"Bash","tool_input":{"command":"true"}}' \
      | bash "$CL_PROXY" >/dev/null 2>&1; check "proxy hook executes cleanly" $?
  fi
  if $DO_COPILOT; then
    [ -s "$CP_INSTRUCTIONS" ]; check "copilot instructions present" $?
    [ -x "$PWD/.github/hooks/yeet-rewrite.sh" ]; check "copilot rewrite hook executable" $?
  fi
  [ -f "$MANIFEST" ] && jq -e . "$MANIFEST" >/dev/null 2>&1; check "manifest is valid JSON" $?
fi

if [ "$FAILED" -gt 0 ]; then
  say ""
  die "$FAILED verification check(s) failed. Run './uninstall.sh' and open an issue:
  https://github.com/$REPO/issues/new"
fi

CLEAN_EXIT=true

# ─── Done ─────────────────────────────────────────────────────────────────────
say ""
rule
say ""
if $DRY_RUN; then
  say "  ${BOLD}${YELLOW}Dry run complete — nothing was changed.${RESET}"
else
  say "  ${BOLD}${GREEN}yeet ${VERSION:-} installed.${RESET}"
fi
say ""
if $DO_CLAUDE; then
  say "  ${DIM}Claude Code: hooks active globally, awareness loaded.${RESET}"
  say "  ${DIM}Restart Claude Code to pick up the new settings.${RESET}"
fi
$DO_COPILOT && say "  ${DIM}Copilot: instructions installed, rewrite hook in $PWD/.github/hooks/${RESET}"
say ""
say "  Next:"
say "    ${CYAN}yeet version${RESET}   ${DIM}confirm the install${RESET}"
say "    ${CYAN}yeet stats${RESET}     ${DIM}see how many tokens you have saved${RESET}"
say "    ${CYAN}yeet --help${RESET}    ${DIM}all commands${RESET}"
say ""
say "  ${DIM}Uninstall any time:${RESET}"
say "    ${CYAN}curl -fsSL $RAW_BASE/uninstall.sh | bash${RESET}"
say ""
