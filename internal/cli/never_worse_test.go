package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Wiring a renderer into the rewrite table means an agent reaches it without
// choosing to, so a renderer that makes its input longer is a regression the
// rewrite now applies automatically. `tsc` did exactly that on a build with a
// few hundred errors: grouping them by file printed more than the compiler had,
// and nothing caught it because tsc printed its rendering unconditionally.
//
// printBetterN is what makes that impossible — it prints whichever of the two
// is shorter. This test pins the invariant at the source level, because the bug
// was never in a renderer's logic: it was a call site that did not use it.
func TestEveryRewriteReachableRendererIsGuarded(t *testing.T) {
	// Every command the rewrite table can route traffic to, minus two that
	// genuinely have no native output to compare against:
	//
	//   read  — reads the file itself rather than wrapping a command, so the
	//           baseline is the file's own content (BaselineDirect) and
	//           printing all of it is what `-l minimal` is for.
	//   grep  — the rg context branch parses `rg --json`, whose output is
	//           several times larger than the search a caller would have run.
	//           That row is already labelled synthetic for the same reason.
	reachable := []string{
		"ls", "find", "diff", "git", "gh",
		"ps", "du", "kubectl", "docker",
		"vitest", "tsc", "lint", "playwright", "prettier", "prisma", "next",
		"npm", "pkgmanager",
	}
	for _, name := range reachable {
		path := filepath.Join(".", name+".go")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("rule table points at %q but %s is missing", name, path)
			continue
		}
		body := string(src)
		if !strings.Contains(body, "printBetterN(") && !strings.Contains(body, "printBetter(") {
			t.Errorf("%s prints without comparing against the raw output; a rendering "+
				"longer than the command's own output would be sent to the model", path)
		}
		// The specific shape that caused the tsc regression.
		if strings.Contains(body, "fmt.Print(rendered)") {
			t.Errorf("%s calls fmt.Print(rendered) directly; use printBetterN so a "+
				"rendering that came out longer than raw is never printed", path)
		}
	}
}

// Each renderer must actually shrink the kind of output it exists for. This is
// the saving itself, stated as a test so a change that quietly stops filtering
// shows up here rather than in a token bill.
func TestRenderersShrinkTheirOwnInput(t *testing.T) {
	cases := []struct {
		name    string
		render  func(string) string
		input   string
		maxKept float64 // fraction of the input the renderer may keep
	}{
		{"ps aux", renderPS, psAuxFixture(300), 0.10},
		{"du -h", renderDU, manyDULines(1200), 0.10},
		{"kubectl get", renderKubectlGet, kubeGetPodsFixture(150, []string{"CrashLoopBackOff"}), 0.10},
		{"kubectl describe", renderKubectlDescribe, bigDescribe(), 0.10},
		{"kubectl logs", func(s string) string { return renderLogStream(s, kubectlMaxLog) },
			strings.Repeat("GET /healthz 200 0.4ms\n", 500), 0.15},
		{"docker ps", renderDockerPS, dockerPSFixture(), 0.70},
		{"tsc", filterTSCOutput, manyTSCErrors(60, 8), 0.35},
		{"npm", func(s string) string { return filterNPMOutput(s, 0) },
			strings.Repeat("npm warn deprecated x@1.0.0: no longer supported\n", 300) + "added 3 packages in 4s\n", 0.05},
		{"pnpm", func(s string) string { return filterPkgManagerOutput("pnpm", s, 0) },
			strings.Repeat("WARN  deprecated x@1.0.0: no longer supported\n", 300) + "added 3 packages in 4s\n", 0.05},
	}

	for _, tc := range cases {
		out := tc.render(tc.input)
		kept := float64(len(out)) / float64(len(tc.input))
		if kept > tc.maxKept {
			t.Errorf("%s kept %.1f%% of its input (%d -> %d bytes), want at most %.0f%%",
				tc.name, kept*100, len(tc.input), len(out), tc.maxKept*100)
		}
	}
}

func manyDULines(n int) string {
	var b strings.Builder
	b.WriteString("8.2G\t./node_modules\n2.1G\t./.git\n512M\t./dist\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%dK\t./node_modules/.pnpm/some-package-%04d@1.%d.%d/node_modules/some-package-%04d/dist/esm\n",
			4+(i%64), i, i%9, i%7, i)
	}
	return b.String()
}

func bigDescribe() string {
	var b strings.Builder
	b.WriteString("Name:         api-0\nNamespace:    prod\nStatus:       Running\n")
	b.WriteString("Annotations:  kubectl.kubernetes.io/last-applied-configuration:\n")
	for i := 0; i < 260; i++ {
		fmt.Fprintf(&b, "                {\"apiVersion\":\"v1\",\"kind\":\"Pod\",\"metadata\":{\"name\":\"api-%d\"}}\n", i)
	}
	b.WriteString("Events:\n  Warning  BackOff  3m  kubelet  Back-off restarting failed container\n")
	return b.String()
}

func dockerPSFixture() string {
	cols := []string{"CONTAINER ID", "IMAGE", "COMMAND", "CREATED", "STATUS", "PORTS", "NAMES"}
	var rows [][]string
	for i := 0; i < 34; i++ {
		rows = append(rows, []string{
			fmt.Sprintf("a1b2c3d4e5f%01d", i), "registry.example.com/team/api-service:1.24.0",
			`"docker-entrypoint.s…"`, fmt.Sprintf("%d hours ago", i+1),
			fmt.Sprintf("Up %d hours (healthy)", i+1),
			fmt.Sprintf("0.0.0.0:80%02d->8080/tcp, :::80%02d->8080/tcp", i, i),
			fmt.Sprintf("prod-api-svc-%02d", i),
		})
	}
	return renderPaddedTable(cols, rows)
}

func manyTSCErrors(files, perFile int) string {
	var b strings.Builder
	total := 0
	for f := 0; f < files; f++ {
		for e := 0; e < perFile; e++ {
			// One file, several errors down it — which is what a real failing
			// build looks like, and what the per-file cap exists for.
			fmt.Fprintf(&b, "src/module%02d/component.tsx(%d,%d): error TS2345: Argument of type 'string | undefined' is not assignable to parameter of type 'string'.\n", f, 40+e*13, 8+e)
			total++
		}
	}
	fmt.Fprintf(&b, "\nFound %d errors in %d files.\n", total, files)
	return b.String()
}

// A build with hundreds of type errors is fixed a few at a time. Reprinting all
// of them in a nicer shape is not a saving, so the listing is capped — but the
// totals still have to be exact, or the reader cannot tell how much is left.
func TestFilterTSCOutput_CapsButCountsHonestly(t *testing.T) {
	raw := manyTSCErrors(60, 8) // 480 errors across 60 files
	got := filterTSCOutput(raw)

	if !strings.Contains(got, "Total: 480 error(s) in 60 file(s)") {
		t.Errorf("filterTSCOutput lost the exact totals:\n%s", truncateMiddle(got, 300))
	}
	if !strings.Contains(got, "more in this file") || !strings.Contains(got, "more file(s) with errors") {
		t.Errorf("filterTSCOutput capped the listing without saying so:\n%s", truncateMiddle(got, 300))
	}
	if len(got) >= len(raw) {
		t.Errorf("filterTSCOutput did not shrink 480 errors: %d in, %d out", len(raw), len(got))
	}
}

// docker prints one published port twice, once per address family. The second
// copy answers no question the first does not.
func TestDedupPortMappings(t *testing.T) {
	tests := []struct{ in, want string }{
		{"0.0.0.0:8000->8080/tcp, :::8000->8080/tcp", "0.0.0.0:8000->8080/tcp"},
		{"0.0.0.0:80->80/tcp, :::80->80/tcp, 0.0.0.0:443->443/tcp, :::443->443/tcp",
			"0.0.0.0:80->80/tcp, 0.0.0.0:443->443/tcp"},
		{"", ""},
		{"0.0.0.0:8000->8080/tcp", "0.0.0.0:8000->8080/tcp"},
		// Different container ports are different mappings and both stay.
		{"0.0.0.0:8000->8080/tcp, 0.0.0.0:8000->9090/tcp", "0.0.0.0:8000->8080/tcp, 0.0.0.0:8000->9090/tcp"},
		{"9000/tcp", "9000/tcp"},
	}
	for _, tc := range tests {
		if got := dedupPortMappings(tc.in); got != tc.want {
			t.Errorf("dedupPortMappings(%q)\n  got  %q\n  want %q", tc.in, got, tc.want)
		}
	}
}
