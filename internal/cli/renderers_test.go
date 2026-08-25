package cli

import (
	"fmt"
	"strings"
	"testing"
)

// ─── du ───────────────────────────────────────────────────────────────────────

func TestRenderDU_SortsAndCollapses(t *testing.T) {
	var b strings.Builder
	b.WriteString("8.2G\t./node_modules\n")
	b.WriteString("2.1G\t./.git\n")
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "%dK\t./src/pkg%03d\n", 4+i%20, i)
	}
	b.WriteString("512M\t./dist\n")
	raw := b.String()

	got := renderDU(raw)
	if len(got) >= len(raw) {
		t.Fatalf("renderDU did not shrink: %d in, %d out", len(raw), len(got))
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")

	// The point of the command is "what is big", so the largest entry has to be
	// first and present.
	if !strings.Contains(lines[1], "node_modules") {
		t.Errorf("renderDU did not put the largest entry first:\n%s", strings.Join(lines[:4], "\n"))
	}
	if !strings.Contains(got, "./.git") || !strings.Contains(got, "./dist") {
		t.Errorf("renderDU dropped a large entry:\n%s", got)
	}
	if !strings.Contains(got, "smaller entries") {
		t.Errorf("renderDU did not account for the collapsed tail:\n%s", got)
	}
}

// Without a unit suffix du is printing blocks whose size depends on the
// platform and on -k/-B. Restating those as bytes would be an invented number.
func TestRenderDU_DoesNotInventUnits(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "%d\t./p%02d\n", 100+i, i)
	}
	got := renderDU(b.String())
	if !strings.Contains(got, "blocks total") {
		t.Errorf("renderDU claimed byte units for unsuffixed sizes:\n%s", got)
	}
}

func TestRenderDU_LeavesShortOutputAlone(t *testing.T) {
	raw := "8.2G\t./node_modules\n2.1G\t./.git\n"
	if got := renderDU(raw); got != raw {
		t.Errorf("renderDU reformatted a two-line listing:\n%s", got)
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in       string
		want     float64
		suffixed bool
		ok       bool
	}{
		{"512B", 512, true, true},
		{"4K", 4096, true, true},
		{"1.5M", 1.5 * 1024 * 1024, true, true},
		{"2G", 2 << 30, true, true},
		{"1.2Gi", 1.2 * (1 << 30), true, true},
		{"4096", 4096, false, true},
		{"", 0, false, false},
		{"abc", 0, false, false},
	}
	for _, tc := range tests {
		got, suf, ok := parseSize(tc.in)
		if ok != tc.ok || suf != tc.suffixed || (ok && got != tc.want) {
			t.Errorf("parseSize(%q) = (%v, %v, %v), want (%v, %v, %v)",
				tc.in, got, suf, ok, tc.want, tc.suffixed, tc.ok)
		}
	}
}

// ─── kubectl ──────────────────────────────────────────────────────────────────

// kubeGetPodsFixture builds a listing padded the way kubectl pads: every
// column widened to its own widest value, so the parser sees real alignment
// rather than something hand-spaced.
func kubeGetPodsFixture(healthy int, broken []string) string {
	type row struct{ name, ready, status, restarts, age string }
	rows := []row{}
	for i := 0; i < healthy; i++ {
		rows = append(rows, row{fmt.Sprintf("api-deployment-7d9f8b6c4d-%05x", i), "2/2", "Running", "0", fmt.Sprintf("%dd", 1+i%20)})
	}
	for i, st := range broken {
		rows = append(rows, row{fmt.Sprintf("worker-6b5c4d3e2f-%05x", i), "0/1", st, "14 (3m ago)", fmt.Sprintf("%dm", 3+i)})
	}
	return renderPaddedTable(
		[]string{"NAME", "READY", "STATUS", "RESTARTS", "AGE"},
		func() [][]string {
			out := make([][]string, 0, len(rows))
			for _, r := range rows {
				out = append(out, []string{r.name, r.ready, r.status, r.restarts, r.age})
			}
			return out
		}())
}

// renderPaddedTable lays out a table the way kubectl and docker do: three
// spaces between columns, every column as wide as its widest cell.
func renderPaddedTable(cols []string, rows [][]string) string {
	w := make([]int, len(cols))
	for i, c := range cols {
		w[i] = len(c)
	}
	for _, r := range rows {
		for i, c := range r {
			if len(c) > w[i] {
				w[i] = len(c)
			}
		}
	}
	var b strings.Builder
	emit := func(cells []string) {
		for i, c := range cells {
			if i == len(cells)-1 {
				b.WriteString(c)
			} else {
				b.WriteString(c + strings.Repeat(" ", w[i]-len(c)+3))
			}
		}
		b.WriteString("\n")
	}
	emit(cols)
	for _, r := range rows {
		emit(r)
	}
	return b.String()
}

// On a real cluster nearly every row says Running and 2/2. Those rows answer no
// question — but the three that say CrashLoopBackOff are the entire reason the
// command was run, so they have to survive verbatim.
func TestRenderKubectlGet_KeepsTheBrokenRowsVerbatim(t *testing.T) {
	raw := kubeGetPodsFixture(120, []string{"CrashLoopBackOff", "Pending", "ImagePullBackOff"})
	got := renderKubectlGet(raw)

	if len(got) >= len(raw) {
		t.Fatalf("renderKubectlGet did not shrink: %d in, %d out", len(raw), len(got))
	}
	for _, want := range []string{"CrashLoopBackOff", "Pending", "ImagePullBackOff", "not ready (3)", "123 resources"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderKubectlGet dropped %q:\n%s", want, got)
		}
	}
	// The healthy rows are the bulk and must be a count, not 120 lines.
	if n := strings.Count(got, "api-deployment"); n != 0 {
		t.Errorf("renderKubectlGet listed %d healthy rows; want them counted only:\n%s", n, got)
	}
}

// A pod that keeps restarting reports Running between restarts, so status alone
// would call a crash loop healthy.
func TestKubeRowHealthy(t *testing.T) {
	cols := []string{"NAME", "READY", "STATUS", "RESTARTS", "AGE"}
	tests := []struct {
		row  []string
		want bool
		why  string
	}{
		{[]string{"a", "2/2", "Running", "0", "3d"}, true, "steady state"},
		{[]string{"a", "1/2", "Running", "0", "3d"}, false, "a replica is missing"},
		{[]string{"a", "0/1", "Pending", "0", "3d"}, false, "not scheduled"},
		{[]string{"a", "1/1", "Running", "14 (3m ago)", "3d"}, false, "restarting repeatedly"},
		{[]string{"a", "1/1", "Running", "2", "3d"}, true, "a couple of restarts is not a loop"},
		{[]string{"a", "0/1", "Completed", "0", "3d"}, true, "a finished job is not broken"},
		{[]string{"a", "1/1", "Unknown", "0", "3d"}, false, "an unrecognised status is surfaced, not hidden"},
	}
	for _, tc := range tests {
		tbl, ok := parseTable(renderPaddedTable(cols, [][]string{tc.row}))
		if !ok {
			t.Fatalf("parseTable refused %q", tc.row)
		}
		if got := kubeRowHealthy(tbl, tbl.rows[0]); got != tc.want {
			t.Errorf("kubeRowHealthy(%v) = %v, want %v — %s", tc.row, got, tc.want, tc.why)
		}
	}
}

// A healthy, short listing is left exactly as it came: reformatting it would
// cost the caller the columns they asked for and save nothing.
func TestRenderKubectlGet_LeavesSmallHealthyListingsAlone(t *testing.T) {
	raw := kubeGetPodsFixture(3, nil)
	if got := renderKubectlGet(raw); got != raw {
		t.Errorf("renderKubectlGet reformatted a healthy 3-row listing:\n%s", got)
	}
}

// An annotation block on a Helm-managed object holds an entire rendered
// manifest. The events at the bottom are what explains the failure.
func TestRenderKubectlDescribe_DropsDeclarationKeepsEvents(t *testing.T) {
	var b strings.Builder
	b.WriteString("Name:         api-0\n")
	b.WriteString("Namespace:    prod\n")
	b.WriteString("Status:       Running\n")
	b.WriteString("Annotations:  kubectl.kubernetes.io/last-applied-configuration:\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "                {\"apiVersion\":\"v1\",\"kind\":\"Pod\",\"metadata\":{\"name\":\"api-%d\"}}\n", i)
	}
	b.WriteString("Events:\n")
	b.WriteString("  Warning  BackOff  3m   kubelet  Back-off restarting failed container\n")
	raw := b.String()

	got := renderKubectlDescribe(raw)
	if len(got) >= len(raw) {
		t.Fatalf("renderKubectlDescribe did not shrink: %d in, %d out", len(raw), len(got))
	}
	for _, want := range []string{"Name:", "Status:", "Back-off restarting", "omitted"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderKubectlDescribe dropped %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "last-applied-configuration:\n                {") {
		t.Errorf("renderKubectlDescribe kept the annotation body:\n%s", got)
	}
}

func TestRenderLogStream_CollapsesRepetition(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString("GET /healthz 200 0.4ms\n")
	}
	b.WriteString("ERROR failed to reach postgres: connection refused\n")
	raw := b.String()

	got := renderLogStream(raw, kubectlMaxLog)
	if len(got) >= len(raw) {
		t.Fatalf("renderLogStream did not shrink: %d in, %d out", len(raw), len(got))
	}
	// The one line that is not a health check is the reason anyone read the log.
	if !strings.Contains(got, "connection refused") {
		t.Errorf("renderLogStream dropped the error line:\n%s", got)
	}
	if !strings.Contains(got, "(x500)") {
		t.Errorf("renderLogStream did not report how often the repeated line occurred:\n%s", got)
	}
}

// ─── docker ───────────────────────────────────────────────────────────────────

func TestRenderDockerPS_SummarisesAndDropsWideColumns(t *testing.T) {
	cols := []string{"CONTAINER ID", "IMAGE", "COMMAND", "CREATED", "STATUS", "PORTS", "NAMES"}
	var rows [][]string
	for i := 0; i < 20; i++ {
		rows = append(rows, []string{
			fmt.Sprintf("a1b2c3d4e5f%01d", i), "registry.example.com/api:1.2.3",
			`"docker-entrypoint.s…"`, fmt.Sprintf("%d hours ago", i+1),
			fmt.Sprintf("Up %d hours", i+1), fmt.Sprintf("0.0.0.0:80%02d->8080/tcp", i),
			fmt.Sprintf("svc-%02d", i),
		})
	}
	for i := 0; i < 12; i++ {
		rows = append(rows, []string{
			fmt.Sprintf("f9e8d7c6b5a%01d", i), "registry.example.com/job:0.9.0",
			`"/bin/sh -c run.sh"`, fmt.Sprintf("%d days ago", i+1),
			fmt.Sprintf("Exited (0) %d days ago", i+1), "",
			fmt.Sprintf("job-%02d", i),
		})
	}
	raw := renderPaddedTable(cols, rows)

	got := renderDockerPS(raw)
	if len(got) >= len(raw) {
		t.Fatalf("renderDockerPS did not shrink: %d in, %d out", len(raw), len(got))
	}
	if !strings.Contains(got, "32 containers") || !strings.Contains(got, "20 up") || !strings.Contains(got, "12 exited (0)") {
		t.Errorf("renderDockerPS lost the status summary:\n%s", strings.Split(got, "\n")[0])
	}
	// The name is how every other docker command refers to a container, so it
	// stays; the twelve-character id it duplicates does not.
	if !strings.Contains(got, "svc-00") {
		t.Errorf("renderDockerPS dropped the container names:\n%s", got)
	}
	if strings.Contains(got, "CONTAINER ID") {
		t.Errorf("renderDockerPS kept the redundant id column:\n%s", got)
	}
}

func TestDockerStatusClass(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Up 3 hours", "up"},
		{"Up 3 hours (healthy)", "up"},
		{"Up 2 minutes (unhealthy)", "up (unhealthy)"},
		{"Up 5 seconds (health: starting)", "up (starting)"},
		{"Exited (0) 2 days ago", "exited (0)"},
		{"Exited (137) 1 hour ago", "exited (nonzero)"},
		{"Created", "created"},
		{"", "unknown"},
	}
	for _, tc := range tests {
		if got := dockerStatusClass(tc.in); got != tc.want {
			t.Errorf("dockerStatusClass(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ─── table parsing ────────────────────────────────────────────────────────────

// Offsets come from the header because a cell can contain spaces ("Up 2
// hours") and a cell can be empty — either of which defeats splitting a row on
// whitespace.
func TestParseTable_HandlesSpacesAndEmptyCells(t *testing.T) {
	raw := "NAME      STATUS         PORTS                NAMES\n" +
		"api       Up 2 hours     0.0.0.0:80->80/tcp   web\n" +
		"job       Exited (0)                          batch\n"
	tbl, ok := parseTable(raw)
	if !ok {
		t.Fatal("parseTable refused a well-formed table")
	}
	if len(tbl.rows) != 2 {
		t.Fatalf("parseTable found %d rows, want 2", len(tbl.rows))
	}
	if got := tbl.cell(tbl.rows[0], "STATUS"); got != "Up 2 hours" {
		t.Errorf("STATUS = %q, want %q (a cell may contain spaces)", got, "Up 2 hours")
	}
	if got := tbl.cell(tbl.rows[1], "PORTS"); got != "" {
		t.Errorf("PORTS = %q, want empty (an unset cell must not shift the row)", got)
	}
	if got := tbl.cell(tbl.rows[1], "NAMES"); got != "batch" {
		t.Errorf("NAMES = %q, want %q", got, "batch")
	}
}

// A header name can itself contain a single space.
func TestSplitAlignedHeader_KeepsMultiWordNames(t *testing.T) {
	cols, starts := splitAlignedHeader("CONTAINER ID   IMAGE   NAMES")
	want := []string{"CONTAINER ID", "IMAGE", "NAMES"}
	if len(cols) != len(want) {
		t.Fatalf("splitAlignedHeader gave %v, want %v", cols, want)
	}
	for i := range want {
		if cols[i] != want[i] {
			t.Errorf("column %d = %q, want %q", i, cols[i], want[i])
		}
	}
	if starts[0] != 0 || starts[1] != 15 {
		t.Errorf("starts = %v, want the header offsets of each column", starts)
	}
}

// A cell wider than its header column shifts every offset after it. Reading
// such a row by offset would attribute values to the wrong columns, so the
// unambiguous wide-gap split is used instead — and it reads this row correctly.
func TestParseTable_RecoversFromOverflowedRows(t *testing.T) {
	raw := "NAME   STATUS\n" +
		"a-very-long-name-that-overflows   Running\n"
	tbl, ok := parseTable(raw)
	if !ok {
		t.Fatal("parseTable refused a row it can read unambiguously")
	}
	if got := tbl.cell(tbl.rows[0], "NAME"); got != "a-very-long-name-that-overflows" {
		t.Errorf("NAME = %q, want the full overflowing name", got)
	}
	if got := tbl.cell(tbl.rows[0], "STATUS"); got != "Running" {
		t.Errorf("STATUS = %q, want %q", got, "Running")
	}
}

// When neither reading gives the expected number of cells, the row is genuinely
// ambiguous and the whole table is refused: a partly-misread table would
// produce a confident summary of something that is not there.
func TestParseTable_RefusesAmbiguousRows(t *testing.T) {
	raw := "NAME   READY   STATUS   AGE\n" +
		"a-name-that-overflows-its-column   2/2   Running\n"
	if _, ok := parseTable(raw); ok {
		t.Error("parseTable accepted a row with fewer cells than the header has columns")
	}
}

// ─── package managers ─────────────────────────────────────────────────────────

func TestFilterPkgManagerOutput_KeepsWhatChangedCountsTheWarnings(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&b, "npm warn deprecated some-pkg@%d.0.0: no longer maintained\n", i)
	}
	b.WriteString("Progress: resolved 1482, reused 1480, downloaded 2, added 3\n")
	b.WriteString("added 3 packages, and audited 1483 packages in 4s\n")
	raw := b.String()

	got := filterPkgManagerOutput("npm", raw, 0)
	if len(got) >= len(raw) {
		t.Fatalf("filterPkgManagerOutput did not shrink: %d in, %d out", len(raw), len(got))
	}
	if !strings.Contains(got, "added 3 packages") {
		t.Errorf("the one line stating what changed was dropped:\n%s", got)
	}
	if !strings.Contains(got, "120 warnings suppressed") {
		t.Errorf("the suppressed warnings were not accounted for:\n%s", got)
	}
}

// A failure is the one case where the detail is the point, so it has to survive
// intact rather than be counted.
func TestFilterPkgManagerOutput_KeepsFailureDetail(t *testing.T) {
	raw := strings.Join([]string{
		"npm warn deprecated x@1.0.0: old",
		"npm error code ELIFECYCLE",
		"npm error errno 1",
		"npm error Failed at the build script",
		"Error: Cannot find module './missing'",
	}, "\n")

	got := filterPkgManagerOutput("npm", raw, 1)
	for _, want := range []string{"[FAIL] npm exit 1", "ELIFECYCLE", "Cannot find module"} {
		if !strings.Contains(got, want) {
			t.Errorf("filterPkgManagerOutput dropped %q from a failure:\n%s", want, got)
		}
	}
}

func TestClassifyPkgLine_RecognisesAllThreeDialects(t *testing.T) {
	tests := []struct {
		in   string
		want pkgLineKind
	}{
		{"npm warn deprecated x@1: old", pkgWarn},
		{"WARN  deprecated x@1: old", pkgWarn}, // pnpm
		{"warning x@1: old", pkgWarn},          // yarn
		{"npm error code ELIFECYCLE", pkgError},
		{"ERR_PNPM_FETCH_404 not found", pkgError},
		{"error An unexpected error occurred", pkgError},
		{"Progress: resolved 1482, reused 1480", pkgProgress},
		{"Packages: +1482", pkgProgress},
		{"[1/4] Resolving packages...", pkgProgress},
		{"added 3 packages in 4s", pkgSummary},
		{"up to date, audited 1483 packages", pkgSummary},
		{"some unclassifiable build output", pkgOther},
	}
	for _, tc := range tests {
		if got := classifyPkgLine(tc.in); got != tc.want {
			t.Errorf("classifyPkgLine(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
