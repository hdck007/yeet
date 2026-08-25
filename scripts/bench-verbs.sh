#!/usr/bin/env bash
# bench-verbs.sh — deterministic measurement of the verb families and of how
# many chained commands the rewrite hook actually catches.
#
#   bash scripts/bench-verbs.sh
#   bash scripts/bench-verbs.sh --baseline /path/to/older-yeet   # old vs new coverage
#   bash scripts/bench-verbs.sh --md out.md
#
# `ps aux`, `kubectl get pods` and `docker ps` produce different output on every
# machine, so quoting a number measured against a live cluster would not be
# reproducible. This script generates fixed fixtures instead: the stub tools in
# $TMP/stub emit the same bytes every run, on any machine, with no cluster, no
# daemon and no node_modules. Every number it prints can be re-derived by
# anyone, which is the point.
#
# The companion scripts/bench-offline.sh measures the file-and-search commands
# against a real repo. This one covers what that cannot reach.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
YEET_BIN=""
BASELINE_BIN=""
MD_OUT=""

while [ $# -gt 0 ]; do
  case "$1" in
    --yeet)     YEET_BIN="$2"; shift 2 ;;
    --baseline) BASELINE_BIN="$2"; shift 2 ;;
    --md)       MD_OUT="$2"; shift 2 ;;
    -h|--help)  sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ -z "$YEET_BIN" ]; then
  YEET_BIN="$REPO_ROOT/dist/yeet-bench"
  ( cd "$REPO_ROOT" && go build -o "$YEET_BIN" ./cmd/yeet ) || {
    echo "could not build yeet; pass --yeet <path>" >&2; exit 1; }
fi

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  BOLD='\033[1m'; GREEN='\033[32m'; RED='\033[31m'; DIM='\033[2m'; RESET='\033[0m'
else
  BOLD=''; GREEN=''; RED=''; DIM=''; RESET=''
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/stub"

# ─── Fixtures ────────────────────────────────────────────────────────────────
# Each stub emits the shape of output the real tool produces on a busy machine.
# Sizes are chosen to match what an agent actually runs into, not a worst case.

cat > "$TMP/stub/ps" << 'STUB'
#!/usr/bin/env python3
print("USER               PID  %CPU %MEM      VSZ    RSS   TT  STAT STARTED      TIME COMMAND")
print("dev               4213  92.4  1.1 41203344  98123   ??  R     9:14AM   4:12.88 /usr/local/bin/node /repo/node_modules/.bin/vitest run --reporter=verbose")
print("dev               5001  11.0  2.2 41000000 180000   ??  S     9:10AM   0:44.10 /usr/bin/python3 /repo/scripts/ingest.py --watch")
print("root                 1   0.1  0.1  4321012   9123   ??  Ss    8:59AM   0:12.00 /sbin/launchd")
CHROME = "/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/126.0.6478.127/Helpers/Google Chrome Helper (Renderer).app/Contents/MacOS/Google Chrome Helper (Renderer)"
for i in range(214):
    print("dev              %5d   0.0  0.2 41888888  17%03d   ??  S     9:0%dAM   0:00.%02d %s --type=renderer --field-trial-handle=1718379636,r,7290312931 --instance=%d" % (20000+i, i%1000, i%10, i%100, CHROME, i))
for i in range(64):
    print("dev              %5d   0.0  0.1 40911111   9%03d   ??  S     9:1%dAM   0:00.%02d /Applications/Visual Studio Code.app/Contents/Frameworks/Code Helper (Plugin).app/Contents/MacOS/Code Helper (Plugin) --node-ipc --clientProcessId=%d" % (30000+i, i%1000, i%10, i%100, i))
for i in range(40):
    print("root             %5d   0.0  0.0  4188888   1%03d   ??  Ss    8:5%dAM   0:00.%02d /usr/libexec/some-system-daemon-%02d --launchd --idle-timeout=300" % (400+i, i%1000, i%10, i%100, i))
STUB

cat > "$TMP/stub/du" << 'STUB'
#!/usr/bin/env python3
print("8.2G\t./node_modules"); print("2.1G\t./.git"); print("512M\t./dist")
for i in range(1200):
    print("%dK\t./node_modules/.pnpm/some-package-%04d@1.%d.%d/node_modules/some-package-%04d/dist/esm" % (4+(i%64), i, i%9, i%7, i))
STUB

cat > "$TMP/stub/kubectl" << 'STUB'
#!/usr/bin/env python3
import sys
a = sys.argv[1:]
def pad(cols, rows):
    w = [len(c) for c in cols]
    for r in rows:
        for i, c in enumerate(r): w[i] = max(w[i], len(c))
    def emit(cells): print("".join(c if i == len(cells)-1 else c+" "*(w[i]-len(c)+3) for i, c in enumerate(cells)))
    emit(cols); [emit(r) for r in rows]
if a[:1] == ["get"]:
    rows = [["prod", "api-deployment-7d9f8b6c4d-%05x" % i, "2/2", "Running", "0", "%dd" % (1+i%20),
             "10.4.%d.%d" % (i//255, i%255), "ip-10-0-%d.ec2.internal" % (i%12)] for i in range(148)]
    for i, st in enumerate(["CrashLoopBackOff", "Pending", "ImagePullBackOff"]):
        rows.append(["prod", "worker-6b5c4d3e2f-%05x" % i, "0/1", st, "14 (3m ago)", "%dm" % (3+i),
                     "10.4.9.%d" % i, "ip-10-0-9.ec2.internal"])
    pad(["NAMESPACE", "NAME", "READY", "STATUS", "RESTARTS", "AGE", "IP", "NODE"], rows)
elif a[:1] == ["describe"]:
    print("Name:         api-0"); print("Namespace:    prod"); print("Status:       Running")
    print("Labels:       app=api")
    for i in range(30): print("              tier-label-%02d=value-%02d" % (i, i))
    print("Annotations:  kubectl.kubernetes.io/last-applied-configuration:")
    for i in range(260):
        print('                {"apiVersion":"v1","kind":"Pod","metadata":{"name":"api-0","labels":{"app":"api","rev":"%d"}},"spec":{"containers":[{"name":"api","image":"registry.example.com/api:1.2.3"}]}}' % i)
    print("Environment:")
    for i in range(80): print("      SOME_SERVICE_%02d_URL:  https://service-%02d.internal.example.com:8443/v1" % (i, i))
    print("Events:")
    print("  Warning  BackOff  3m  kubelet  Back-off restarting failed container api in pod api-0_prod")
else:
    for i in range(900):
        print('{"level":"info","ts":"2026-08-25T09:%02d:%02dZ","msg":"GET /healthz","status":200,"dur_ms":0.4}' % (i%60, i%60))
    print('{"level":"error","ts":"2026-08-25T09:59:59Z","msg":"failed to reach postgres: connection refused"}')
STUB

cat > "$TMP/stub/docker" << 'STUB'
#!/usr/bin/env python3
import sys
a = sys.argv[1:]
def pad(cols, rows):
    w = [len(c) for c in cols]
    for r in rows:
        for i, c in enumerate(r): w[i] = max(w[i], len(c))
    def emit(cells): print("".join(c if i == len(cells)-1 else c+" "*(w[i]-len(c)+3) for i, c in enumerate(cells)))
    emit(cols); [emit(r) for r in rows]
if a[:1] == ["ps"]:
    rows = [["a1b2c3d4e5f%01d" % i, "registry.example.com/team/api-service:1.24.%d" % i,
             '"docker-entrypoint.s…"', "%d hours ago" % (i+1), "Up %d hours (healthy)" % (i+1),
             "0.0.0.0:80%02d->8080/tcp, :::80%02d->8080/tcp" % (i, i), "prod-api-svc-%02d" % i] for i in range(34)]
    rows += [["f9e8d7c6b5a%01d" % i, "registry.example.com/team/batch-job:0.9.%d" % i,
              '"/bin/sh -c ./run.sh"', "%d days ago" % (i+1), "Exited (0) %d days ago" % (i+1),
              "", "prod-batch-job-%02d" % i] for i in range(26)]
    pad(["CONTAINER ID", "IMAGE", "COMMAND", "CREATED", "STATUS", "PORTS", "NAMES"], rows)
elif a[:1] == ["images"]:
    pad(["REPOSITORY", "TAG", "IMAGE ID", "CREATED", "SIZE"],
        [["registry.example.com/team/svc-%03d" % i, "1.%d.%d" % (i%9, i%7), "%012x" % (i*7919),
          "%d days ago" % (i+1), "%dMB" % (120+i*13)] for i in range(180)])
else:
    for i in range(700): print("api  | 2026-08-25T09:%02d:%02dZ INFO  GET /healthz 200 0.4ms" % (i%60, i%60))
    print("api  | 2026-08-25T09:59:59Z ERROR upstream timeout after 30s calling billing-service")
STUB

# vitest emits its human report by default and JSON when asked, exactly as the
# real one does — so the baseline is the report a caller would have seen, not
# the JSON yeet asks for.
cat > "$TMP/stub/vitest" << 'STUB'
#!/usr/bin/env python3
import sys, json
if any("--reporter=json" in x for x in sys.argv):
    files = []
    for f in range(64):
        res = [{"status": "passed", "fullName": "suite %d > case %d" % (f, i), "failureMessages": []} for i in range(18)]
        if f == 17:
            res[3] = {"status": "failed", "fullName": "suite 17 > computes the invoice total",
                      "failureMessages": ["AssertionError: expected 42 to be 43\n    at /repo/src/invoice.test.ts:88:24\n" +
                                          "\n".join("    at runNextTicks (node:internal/process/task_queues:%d:5)" % (i+60) for i in range(14))]}
        files.append({"name": "/repo/src/mod%02d.test.ts" % f, "assertionResults": res})
    print(json.dumps({"testResults": files, "numTotalTests": 64*18, "numPassedTests": 64*18-1,
                      "numFailedTests": 1, "numPendingTests": 0}))
else:
    for f in range(64):
        print(" ✓ src/mod%02d.test.ts (18 tests) %dms" % (f, 120+f))
        for i in range(18): print("   ✓ suite %d > case %d %dms" % (f, i, 1+i))
    print(" ✗ src/invoice.test.ts (18 tests | 1 failed) 402ms")
    print("   ✗ suite 17 > computes the invoice total")
    print("     AssertionError: expected 42 to be 43")
    for i in range(14): print("      at runNextTicks (node:internal/process/task_queues:%d:5)" % (i+60))
    print(" Test Files  1 failed | 64 passed (65)")
    print("      Tests  1 failed | 1151 passed (1152)")
    sys.exit(1)
STUB

cat > "$TMP/stub/tsc" << 'STUB'
#!/usr/bin/env python3
for f in range(48):
    for e in range(6):
        print("src/module%02d/component.tsx(%d,%d): error TS2345: Argument of type 'string | undefined' is not assignable to parameter of type 'string'." % (f, 40+e*13, 8+e))
print(""); print("Found 288 errors in 48 files."); print("")
print("Errors  Files")
for f in range(48): print("     6  src/module%02d/component.tsx" % f)
STUB

cat > "$TMP/stub/npm" << 'STUB'
#!/usr/bin/env python3
for i in range(418):
    print("npm warn deprecated legacy-package-%03d@%d.0.%d: This package is no longer supported. Please migrate to @scope/modern-package-%03d instead." % (i, i%9, i%7, i))
for i in range(120):
    print("npm http fetch GET 200 https://registry.npmjs.org/some-package-%03d 142ms (cache miss)" % i)
print(""); print("added 1483 packages, and audited 1484 packages in 41s"); print("")
print("212 packages are looking for funding"); print("  run `npm fund` for details"); print("")
print("found 0 vulnerabilities")
STUB
cp "$TMP/stub/npm" "$TMP/stub/pnpm"
chmod +x "$TMP"/stub/*

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required to generate the fixtures" >&2; exit 1
fi

export PATH="$TMP/stub:$PATH"
export YEET_NO_ANALYTICS=1

# ─── Per-verb savings ────────────────────────────────────────────────────────

MD_ROWS=""
tot_raw=0; tot_new=0

measure() {
  local label="$1" cmd="$2"
  local rb nb rt nt pct
  rb=$( ( eval "$cmd" ) 2>&1 | wc -c | tr -d ' ' )
  nb=$( ( eval "$YEET_BIN $cmd" ) 2>&1 | wc -c | tr -d ' ' )
  rt=$(( (rb+3)/4 )); nt=$(( (nb+3)/4 ))
  pct=$(awk "BEGIN{printf \"%.1f\", 100*($rb-$nb)/$rb}")
  printf '  %-26s %9s %9s %9s %8s%%\n' "$label" "$rb" "$nb" "$((rt-nt))" "$pct"
  MD_ROWS="${MD_ROWS}| \`$label\` | $rb | $nb | $((rt-nt)) | **${pct}%** |
"
  tot_raw=$((tot_raw+rb)); tot_new=$((tot_new+nb))
}

printf "${BOLD}Per-verb savings${RESET} ${DIM}(bytes; tokens at 4 chars/token)${RESET}\n"
printf '  %-26s %9s %9s %9s %9s\n' "command" "raw" "yeet" "tok saved" "cut"
printf '  %s\n' "---------------------------------------------------------------------"
measure "ps aux"                  "ps aux"
measure "du -h ."                 "du -h ."
measure "kubectl get pods -A"     "kubectl get pods -A"
measure "kubectl describe pod x"  "kubectl describe pod x"
measure "kubectl logs deploy/api" "kubectl logs deploy/api"
measure "docker ps -a"            "docker ps -a"
measure "docker images"           "docker images"
measure "docker compose logs api" "docker compose logs api"
measure "vitest run"              "vitest run"
measure "tsc --noEmit"            "tsc --noEmit"
measure "npm install"             "npm install"
measure "pnpm install"            "pnpm install"
printf '  %s\n' "---------------------------------------------------------------------"
TOT_PCT=$(awk "BEGIN{printf \"%.1f\", 100*($tot_raw-$tot_new)/$tot_raw}")
printf "  ${BOLD}%-26s %9s %9s %9s %8s%%${RESET}\n" "TOTAL" "$tot_raw" "$tot_new" \
  "$(( (tot_raw+3)/4 - (tot_new+3)/4 ))" "$TOT_PCT"
echo ""

# ─── Rewrite coverage ────────────────────────────────────────────────────────
# A renderer nothing routes to saves nothing, so coverage is measured separately
# from the size of the saving.

CORPUS="$REPO_ROOT/scripts/bench-corpus.txt"
if [ ! -f "$CORPUS" ]; then
  echo "missing corpus: $CORPUS" >&2; exit 1
fi

coverage() {
  local bin="$1" total=0 hit=0 allow=0 ask=0
  while IFS= read -r cmd; do
    case "$cmd" in ''|'#'*) continue ;; esac
    total=$((total+1))
    "$bin" rewrite "$cmd" >/dev/null 2>&1
    case $? in
      0) hit=$((hit+1)); allow=$((allow+1)) ;;
      3) hit=$((hit+1)); ask=$((ask+1)) ;;
    esac
  done < "$CORPUS"
  printf '%d %d %d %d' "$total" "$hit" "$allow" "$ask"
}

printf "${BOLD}Rewrite coverage${RESET} ${DIM}(scripts/bench-corpus.txt)${RESET}\n"
read -r n_tot n_hit n_allow n_ask <<< "$(coverage "$YEET_BIN")"
NEW_PCT=$(awk "BEGIN{printf \"%.1f\", 100*$n_hit/$n_tot}")
if [ -n "$BASELINE_BIN" ]; then
  read -r b_tot b_hit b_allow b_ask <<< "$(coverage "$BASELINE_BIN")"
  BASE_PCT=$(awk "BEGIN{printf \"%.1f\", 100*$b_hit/$b_tot}")
  printf '  %-12s %s/%s rewritten  %s(%s auto-allow, %s ask)%s  %s%%\n' \
    "baseline" "$b_hit" "$b_tot" "$DIM" "$b_allow" "$b_ask" "$RESET" "$BASE_PCT"
fi
printf '  %-12s %s/%s rewritten  %s(%s auto-allow, %s ask)%s  %s%s%%%s\n' \
  "this build" "$n_hit" "$n_tot" "$DIM" "$n_allow" "$n_ask" "$RESET" "$GREEN" "$NEW_PCT" "$RESET"
echo ""

# ─── Safety ──────────────────────────────────────────────────────────────────
# Coverage is only worth having if it stops at the right place. Every command
# below either changes state or is a shape a rewrite would answer wrongly, and
# every one must come back untouched.

printf "${BOLD}Must not be rewritten${RESET}\n"
leaks=0; checked=0
while IFS= read -r cmd; do
  case "$cmd" in ''|'#'*) continue ;; esac
  checked=$((checked+1))
  out=$("$YEET_BIN" rewrite "$cmd" 2>/dev/null); code=$?
  if [ "$code" -ne 1 ]; then
    printf "  ${RED}LEAK${RESET} (exit %d) %s → %s\n" "$code" "$cmd" "$out"
    leaks=$((leaks+1))
  fi
done < "$REPO_ROOT/scripts/bench-corpus-negative.txt"
if [ "$leaks" -eq 0 ]; then
  printf "  ${GREEN}%d/%d held${RESET}  ${DIM}(mutations and stdin/redirect shapes all passed through)${RESET}\n" "$checked" "$checked"
fi
echo ""

if [ -n "$MD_OUT" ]; then
  {
    echo "| command | raw bytes | yeet bytes | tokens saved | cut |"
    echo "|---|---|---|---|---|"
    printf '%s' "$MD_ROWS"
    echo "| **TOTAL** | **$tot_raw** | **$tot_new** | **$(( (tot_raw+3)/4 - (tot_new+3)/4 ))** | **${TOT_PCT}%** |"
    echo ""
    echo "Rewrite coverage: ${n_hit}/${n_tot} (${NEW_PCT}%) — ${n_allow} auto-allow, ${n_ask} ask."
    [ -n "$BASELINE_BIN" ] && echo "Baseline coverage: ${b_hit}/${b_tot} (${BASE_PCT}%)."
    echo "Must-not-rewrite corpus: ${checked}/${checked} held."
  } > "$MD_OUT"
  echo "markdown written to $MD_OUT"
fi

exit "$leaks"
