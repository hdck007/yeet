#!/bin/bash
set -e

# ============================================================
# yeet demo — Token savings analysis
# ============================================================
# All yeet calls use --no-analytics so demo doesn't
# pollute real usage data.
# ============================================================

NA="--no-analytics"

BOLD='\033[1m'
DIM='\033[2m'
GREEN='\033[32m'
CYAN='\033[36m'
YELLOW='\033[33m'
RED='\033[31m'
RESET='\033[0m'

divider() {
  echo -e "${DIM}$(printf '─%.0s' {1..60})${RESET}"
}

header() {
  echo ""
  divider
  echo -e "${BOLD}${CYAN}  $1${RESET}"
  divider
}

# Accumulators for final table
declare -a TABLE_LABELS=()
declare -a TABLE_RAW_CHARS=()
declare -a TABLE_YEET_CHARS=()
declare -a TABLE_SAVED=()
declare -a TABLE_PCT=()
declare -a TABLE_TOKENS=()

compare() {
  local label="$1"
  local raw_file="$2"
  local yeet_file="$3"

  raw_chars=$(wc -c < "$raw_file" | tr -d ' ')
  yeet_chars=$(wc -c < "$yeet_file" | tr -d ' ')
  raw_lines=$(wc -l < "$raw_file" | tr -d ' ')
  yeet_lines=$(wc -l < "$yeet_file" | tr -d ' ')

  if [ "$raw_chars" -gt 0 ]; then
    saved=$(( raw_chars - yeet_chars ))
    pct=$(echo "scale=1; $saved * 100 / $raw_chars" | bc)
    tokens_saved=$(( (saved + 3) / 4 ))
  else
    saved=0
    pct="0.0"
    tokens_saved=0
  fi

  # Store for summary table
  TABLE_LABELS+=("$label")
  TABLE_RAW_CHARS+=("$raw_chars")
  TABLE_YEET_CHARS+=("$yeet_chars")
  TABLE_SAVED+=("$saved")
  TABLE_PCT+=("$pct")
  TABLE_TOKENS+=("$tokens_saved")

  echo -e "  ${BOLD}$label${RESET}"
  echo -e "    Raw:    ${RED}${raw_chars} chars${RESET} (${raw_lines} lines)"
  echo -e "    Yeet:   ${GREEN}${yeet_chars} chars${RESET} (${yeet_lines} lines)"
  echo -e "    Saved:  ${YELLOW}${saved} chars (${pct}%) ≈ ${tokens_saved} tokens${RESET}"
  echo ""
}

TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

# ============================================================
header "1. yeet ls vs ls -laR"
# ============================================================

ls -laR . > "$TMPDIR/raw_ls.txt" 2>/dev/null
yeet $NA ls . > "$TMPDIR/yeet_ls.txt"

echo -e "\n${DIM}  Raw (ls -laR) first 8 lines:${RESET}"
head -8 "$TMPDIR/raw_ls.txt" | sed 's/^/    /'
echo -e "    ${DIM}... ($(wc -l < "$TMPDIR/raw_ls.txt" | tr -d ' ') lines total)${RESET}"
echo -e "\n${DIM}  Yeet output:${RESET}"
cat "$TMPDIR/yeet_ls.txt" | sed 's/^/    /'

compare "ls" "$TMPDIR/raw_ls.txt" "$TMPDIR/yeet_ls.txt"

# ============================================================
header "2. yeet read vs cat"
# ============================================================

TARGET_FILE="internal/cli/root.go"
cat "$TARGET_FILE" > "$TMPDIR/raw_read.txt"
yeet $NA read "$TARGET_FILE" > "$TMPDIR/yeet_read.txt"

echo -e "\n${DIM}  Raw (cat) first 5 lines:${RESET}"
head -5 "$TMPDIR/raw_read.txt" | sed 's/^/    /'
echo -e "    ${DIM}...${RESET}"
echo -e "\n${DIM}  Yeet output first 5 lines:${RESET}"
head -5 "$TMPDIR/yeet_read.txt" | sed 's/^/    /'
echo -e "    ${DIM}...${RESET}"

compare "read" "$TMPDIR/raw_read.txt" "$TMPDIR/yeet_read.txt"

# ============================================================
header "3. yeet read -l aggressive vs full file"
# ============================================================

cat "$TARGET_FILE" > "$TMPDIR/raw_read_full.txt"
yeet $NA read "$TARGET_FILE" -l aggressive > "$TMPDIR/yeet_read_agg.txt"

echo -e "\n${DIM}  Raw (full file): $(wc -l < "$TMPDIR/raw_read_full.txt" | tr -d ' ') lines${RESET}"
echo -e "\n${DIM}  Yeet aggressive output:${RESET}"
cat "$TMPDIR/yeet_read_agg.txt" | sed 's/^/    /'

compare "read -l agg" "$TMPDIR/raw_read_full.txt" "$TMPDIR/yeet_read_agg.txt"

# ============================================================
header "4. yeet smart vs full file"
# ============================================================

cat "$TARGET_FILE" > "$TMPDIR/raw_smart.txt"
yeet $NA smart "$TARGET_FILE" > "$TMPDIR/yeet_smart.txt"

echo -e "\n${DIM}  Raw: $(wc -c < "$TMPDIR/raw_smart.txt" | tr -d ' ') chars of full file content${RESET}"
echo -e "\n${DIM}  Yeet smart output:${RESET}"
cat "$TMPDIR/yeet_smart.txt" | sed 's/^/    /'

compare "smart" "$TMPDIR/raw_smart.txt" "$TMPDIR/yeet_smart.txt"

# ============================================================
header "5. yeet glob vs find"
# ============================================================

find . -name "*.go" -not -path "./.git/*" > "$TMPDIR/raw_glob.txt" 2>/dev/null
yeet $NA glob "**/*.go" . > "$TMPDIR/yeet_glob.txt"

echo -e "\n${DIM}  Raw (find): $(wc -l < "$TMPDIR/raw_glob.txt" | tr -d ' ') lines${RESET}"
echo -e "\n${DIM}  Yeet glob (last 5):${RESET}"
tail -5 "$TMPDIR/yeet_glob.txt" | sed 's/^/    /'

compare "glob" "$TMPDIR/raw_glob.txt" "$TMPDIR/yeet_glob.txt"

# ============================================================
header "6. yeet find vs find -ls"
# ============================================================

find . -name "*.go" -not -path "./.git/*" -ls > "$TMPDIR/raw_find.txt" 2>/dev/null
yeet $NA find "*.go" . > "$TMPDIR/yeet_find.txt"

echo -e "\n${DIM}  Raw (find -ls) first 3 lines:${RESET}"
head -3 "$TMPDIR/raw_find.txt" | sed 's/^/    /'
echo -e "    ${DIM}...${RESET}"
echo -e "\n${DIM}  Yeet find (last 3):${RESET}"
tail -3 "$TMPDIR/yeet_find.txt" | sed 's/^/    /'

compare "find" "$TMPDIR/raw_find.txt" "$TMPDIR/yeet_find.txt"

# ============================================================
header "7. yeet grep vs grep"
# ============================================================

grep -rn "func " . --include="*.go" > "$TMPDIR/raw_grep.txt" 2>/dev/null
yeet $NA grep "func " . > "$TMPDIR/yeet_grep.txt"

echo -e "\n${DIM}  Raw (grep -rn): $(wc -l < "$TMPDIR/raw_grep.txt" | tr -d ' ') lines${RESET}"
echo -e "\n${DIM}  Yeet grep first 8 lines:${RESET}"
head -8 "$TMPDIR/yeet_grep.txt" | sed 's/^/    /'
echo -e "    ${DIM}...${RESET}"

compare "grep" "$TMPDIR/raw_grep.txt" "$TMPDIR/yeet_grep.txt"

# ============================================================
header "8. yeet diff vs diff"
# ============================================================

diff -u internal/token/estimator.go internal/exec/runner.go > "$TMPDIR/raw_diff.txt" 2>/dev/null || true
yeet $NA diff internal/token/estimator.go internal/exec/runner.go > "$TMPDIR/yeet_diff.txt"

echo -e "\n${DIM}  Raw: $(wc -l < "$TMPDIR/raw_diff.txt" | tr -d ' ') lines | Yeet: $(wc -l < "$TMPDIR/yeet_diff.txt" | tr -d ' ') lines${RESET}"

compare "diff" "$TMPDIR/raw_diff.txt" "$TMPDIR/yeet_diff.txt"

# ============================================================
header "9. yeet write vs echo (realistic file)"
# ============================================================

# Generate a realistic Go source file
cat > "$TMPDIR/sample_server.go" <<'SRCEOF'
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type Config struct {
	Addr         string        `json:"addr"`
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
	IdleTimeout  time.Duration `json:"idle_timeout"`
	MaxConns     int           `json:"max_conns"`
}

type Server struct {
	config   Config
	handler  http.Handler
	mu       sync.RWMutex
	started  bool
	shutdown chan struct{}
	conns    int64
}

func NewServer(cfg Config, handler http.Handler) *Server {
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 15 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 15 * time.Second
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 60 * time.Second
	}
	return &Server{
		config:   cfg,
		handler:  handler,
		shutdown: make(chan struct{}),
	}
}

func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("server already started on %s", s.config.Addr)
	}
	s.started = true
	s.mu.Unlock()

	srv := &http.Server{
		Addr:         s.config.Addr,
		Handler:      s.handler,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}

	go func() {
		<-ctx.Done()
		log.Println("Shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server shutdown error: %v\n", err)
		}
		close(s.shutdown)
	}()

	log.Printf("Server listening on %s\n", s.config.Addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

func (s *Server) WaitForShutdown() {
	<-s.shutdown
}

func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started
}

func (s *Server) ActiveConnections() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.conns
}

func (s *Server) HealthCheck(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	running := s.started
	conns := s.conns
	s.mu.RUnlock()

	status := map[string]interface{}{
		"status":      "ok",
		"running":     running,
		"connections": conns,
		"uptime":      time.Since(time.Now()).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
SRCEOF

SAMPLE_CONTENT=$(cat "$TMPDIR/sample_server.go")

# Raw: echo/cat echoes full content back
echo "$SAMPLE_CONTENT" > "$TMPDIR/raw_write_echo.txt"

# Yeet: compact confirmation only
echo "$SAMPLE_CONTENT" | yeet $NA write "$TMPDIR/yeet_written_server.go" > "$TMPDIR/yeet_write.txt"

echo -e "\n${DIM}  Raw: echoes full file ($(wc -l < "$TMPDIR/raw_write_echo.txt" | tr -d ' ') lines, $(wc -c < "$TMPDIR/raw_write_echo.txt" | tr -d ' ') chars)${RESET}"
echo -e "\n${DIM}  Yeet write:${RESET}"
cat "$TMPDIR/yeet_write.txt" | sed 's/^/    /'

compare "write" "$TMPDIR/raw_write_echo.txt" "$TMPDIR/yeet_write.txt"

# ============================================================
header "10. yeet edit vs sed (realistic file)"
# ============================================================

# Use the same server file for edit
cp "$TMPDIR/sample_server.go" "$TMPDIR/edit_target.go"

# Raw: sed outputs the entire modified file
sed 's/15 \* time\.Second/30 * time.Second/g' "$TMPDIR/edit_target.go" > "$TMPDIR/raw_edit_out.txt"

# Yeet: compact confirmation
cp "$TMPDIR/sample_server.go" "$TMPDIR/yeet_edit_target.go"
yeet $NA edit "$TMPDIR/yeet_edit_target.go" --old "15 * time.Second" --new "30 * time.Second" --all > "$TMPDIR/yeet_edit_out.txt"

echo -e "\n${DIM}  Raw (sed): outputs entire $(wc -l < "$TMPDIR/raw_edit_out.txt" | tr -d ' ')-line file ($(wc -c < "$TMPDIR/raw_edit_out.txt" | tr -d ' ') chars)${RESET}"
echo -e "\n${DIM}  Yeet edit:${RESET}"
cat "$TMPDIR/yeet_edit_out.txt" | sed 's/^/    /'

compare "edit" "$TMPDIR/raw_edit_out.txt" "$TMPDIR/yeet_edit_out.txt"


# ============================================================
header "11. yeet env (filter environment variables)"
# ============================================================

env > "$TMPDIR/raw_env.txt"
yeet $NA env > "$TMPDIR/yeet_env.txt"

echo -e "\n${DIM}  Raw (env): $(wc -l < "$TMPDIR/raw_env.txt" | tr -d ' ') vars${RESET}"
echo -e "\n${DIM}  Yeet env (first 10 lines):${RESET}"
head -10 "$TMPDIR/yeet_env.txt" | sed 's/^/    /'
echo -e "    ${DIM}...${RESET}"

compare "env" "$TMPDIR/raw_env.txt" "$TMPDIR/yeet_env.txt"

# ============================================================
header "12. yeet json (inspect JSON structure)"
# ============================================================

cat > "$TMPDIR/sample.json" << 'JSONEOF'
{
  "name": "yeet",
  "version": "1.0.0",
  "config": {
    "timeout": 30,
    "retries": 3,
    "endpoints": ["https://api.example.com", "https://backup.example.com"],
    "auth": {"token": "secret123", "expires": 1234567890}
  },
  "metadata": {"created": "2024-01-01", "owner": "team-infra", "tags": ["cli", "tools"]}
}
JSONEOF

cat "$TMPDIR/sample.json" > "$TMPDIR/raw_json.txt"
yeet $NA json "$TMPDIR/sample.json" > "$TMPDIR/yeet_json.txt"

echo -e "\n${DIM}  Raw JSON: $(wc -c < "$TMPDIR/raw_json.txt" | tr -d ' ') chars${RESET}"
echo -e "\n${DIM}  Yeet json output:${RESET}"
cat "$TMPDIR/yeet_json.txt" | sed 's/^/    /'

compare "json" "$TMPDIR/raw_json.txt" "$TMPDIR/yeet_json.txt"

# ============================================================
header "13. yeet log (deduplicate log output)"
# ============================================================

printf "2024-01-15T12:00:00 ERROR: database connection timeout\n" > "$TMPDIR/raw_log.txt"
printf "2024-01-15T12:00:01 INFO: request processed successfully\n" >> "$TMPDIR/raw_log.txt"
printf "2024-01-15T12:00:02 ERROR: database connection timeout\n" >> "$TMPDIR/raw_log.txt"
printf "2024-01-15T12:00:03 ERROR: database connection timeout\n" >> "$TMPDIR/raw_log.txt"
printf "2024-01-15T12:00:04 INFO: request processed successfully\n" >> "$TMPDIR/raw_log.txt"
printf "2024-01-15T12:00:05 DEBUG: cache hit for key user_123\n" >> "$TMPDIR/raw_log.txt"
printf "2024-01-15T12:00:06 ERROR: database connection timeout\n" >> "$TMPDIR/raw_log.txt"

yeet $NA log "$TMPDIR/raw_log.txt" > "$TMPDIR/yeet_log.txt"

echo -e "\n${DIM}  Raw log: $(wc -l < "$TMPDIR/raw_log.txt" | tr -d ' ') lines${RESET}"
echo -e "\n${DIM}  Yeet log output:${RESET}"
cat "$TMPDIR/yeet_log.txt" | sed 's/^/    /'

compare "log" "$TMPDIR/raw_log.txt" "$TMPDIR/yeet_log.txt"

# ============================================================
header "14. yeet deps (project dependencies)"
# ============================================================

cat go.mod > "$TMPDIR/raw_deps.txt"
yeet $NA deps . > "$TMPDIR/yeet_deps.txt"

echo -e "\n${DIM}  Raw (go.mod): $(wc -l < "$TMPDIR/raw_deps.txt" | tr -d ' ') lines${RESET}"
echo -e "\n${DIM}  Yeet deps output:${RESET}"
cat "$TMPDIR/yeet_deps.txt" | sed 's/^/    /'

compare "deps" "$TMPDIR/raw_deps.txt" "$TMPDIR/yeet_deps.txt"

# ============================================================
header "15. yeet tree vs tree/find"
# ============================================================

if command -v tree &>/dev/null; then
  tree . --noreport > "$TMPDIR/raw_tree.txt" 2>/dev/null
else
  find . -not -path "./.git/*" | sort > "$TMPDIR/raw_tree.txt"
fi
yeet $NA tree . > "$TMPDIR/yeet_tree.txt"

echo -e "\n${DIM}  Raw: $(wc -l < "$TMPDIR/raw_tree.txt" | tr -d ' ') lines${RESET}"
echo -e "\n${DIM}  Yeet tree (first 8 lines):${RESET}"
head -8 "$TMPDIR/yeet_tree.txt" | sed 's/^/    /'
echo -e "    ${DIM}...${RESET}"

compare "tree" "$TMPDIR/raw_tree.txt" "$TMPDIR/yeet_tree.txt"

# ============================================================
header "RESULTS TABLE"
# ============================================================

echo ""
printf "${BOLD}  %-14s %10s %10s %10s %8s %12s${RESET}\n" \
  "Command" "Raw" "Yeet" "Saved" "%" "~Tokens"
echo -e "  ${DIM}$(printf '─%.0s' {1..68})${RESET}"

total_raw=0
total_yeet=0
total_saved=0
total_tokens=0

for i in "${!TABLE_LABELS[@]}"; do
  label="${TABLE_LABELS[$i]}"
  raw="${TABLE_RAW_CHARS[$i]}"
  yeet_c="${TABLE_YEET_CHARS[$i]}"
  saved="${TABLE_SAVED[$i]}"
  pct="${TABLE_PCT[$i]}"
  tokens="${TABLE_TOKENS[$i]}"

  color="$GREEN"
  if [ "$saved" -lt 0 ]; then
    color="$RED"
  fi

  printf "  %-14s %10s %10s ${color}%10s %7s%%${RESET} %12s\n" \
    "$label" "$raw" "$yeet_c" "$saved" "$pct" "$tokens"

  total_raw=$(( total_raw + raw ))
  total_yeet=$(( total_yeet + yeet_c ))
  total_saved=$(( total_saved + saved ))
  total_tokens=$(( total_tokens + tokens ))
done

echo -e "  ${DIM}$(printf '─%.0s' {1..68})${RESET}"

if [ "$total_raw" -gt 0 ]; then
  total_pct=$(echo "scale=1; $total_saved * 100 / $total_raw" | bc)
else
  total_pct="0.0"
fi

printf "  ${BOLD}%-14s %10s %10s ${GREEN}%10s %7s%%${RESET} ${BOLD}%12s${RESET}\n" \
  "TOTAL" "$total_raw" "$total_yeet" "$total_saved" "$total_pct" "$total_tokens"

echo ""
echo -e "  ${BOLD}${GREEN}≈ ${total_tokens} tokens saved${RESET} across ${#TABLE_LABELS[@]} commands"
echo ""
divider
echo -e "  Demo used ${CYAN}--no-analytics${RESET}. Real usage stats unaffected."
echo -e "  ${CYAN}yeet stats${RESET}    — view real cumulative savings"
echo -e "  ${CYAN}yeet clear${RESET}    — reset analytics"
echo -e "  ${CYAN}yeet update${RESET}   — rebuild and reinstall"
divider
echo ""
