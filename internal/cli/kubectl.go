package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var kubectlCmd = &cobra.Command{
	Use:                "kubectl <subcommand> [args...]",
	Short:              "kubectl read commands with healthy resources collapsed",
	Args:               cobra.ArbitraryArgs,
	RunE:               runKubectl,
	DisableFlagParsing: true,
}

func init() {
	rootCmd.AddCommand(kubectlCmd)
}

const (
	kubectlMaxRows = 40  // rows kept when a listing has no health signal
	kubectlMaxLog  = 120 // lines kept from a log stream
)

func runKubectl(cmd *cobra.Command, args []string) error {
	start := time.Now()
	args = stripYeetFlags(args)

	if !yeetexec.Available("kubectl") {
		return fmt.Errorf("kubectl not found in PATH")
	}
	// The rewrite layer keeps mutations away from here, but yeet kubectl is
	// also a command a user can type directly, and it runs the real binary.
	if len(args) > 0 && guardKubectl(append([]string{"kubectl"}, args...)) == vNever {
		return fmt.Errorf("yeet kubectl only wraps read-only subcommands; run `kubectl %s` directly", strings.Join(args, " "))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result := yeetexec.Run(ctx, "kubectl", args...)
	raw := result.Stdout
	if strings.TrimSpace(raw) == "" {
		raw = result.Stdout + result.Stderr
	}

	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	rendered := renderKubectl(sub, raw)
	if rawOutput {
		rendered = raw
	}
	printed, _ := printBetterN(raw, rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "kubectl",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(raw),
			CharsRendered: len(rendered),
			CharsPrinted:  printed,
			BaselineCmd:   "kubectl " + strings.Join(args, " "),
			YeetCmd:       "yeet kubectl " + strings.Join(args, " "),
			BaselineKind:  analytics.BaselineAsInvoked,
			ExitCode:      result.ExitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

func renderKubectl(sub, raw string) string {
	switch sub {
	case "get":
		return renderKubectlGet(raw)
	case "describe":
		return renderKubectlDescribe(raw)
	case "logs":
		return renderLogStream(raw, kubectlMaxLog)
	}
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	if len(lines) <= kubectlMaxRows {
		return raw
	}
	return strings.Join(capLines(lines, kubectlMaxRows), "\n") + "\n"
}

// ─── get ──────────────────────────────────────────────────────────────────────

// renderKubectlGet turns a listing into a health summary plus the rows that are
// not healthy. On a real cluster almost every row says Running and 2/2, and
// those rows are the answer to no question — but the three that say
// CrashLoopBackOff are the whole reason the command was run, so they are kept
// verbatim and never summarised.
func renderKubectlGet(raw string) string {
	t, ok := parseTable(raw)
	if !ok {
		return raw
	}

	statusAt := t.index("STATUS")
	readyAt := t.index("READY")
	if statusAt < 0 && readyAt < 0 {
		// No health signal (services, configmaps, nodes with -o name). Nothing
		// to collapse against, so only the row count is negotiable.
		if len(t.rows) <= kubectlMaxRows {
			return raw
		}
		lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
		return strings.Join(capLines(lines, kubectlMaxRows+1), "\n") + "\n"
	}

	counts := map[string]int{}
	var order []string
	var unhealthy [][]string
	for _, row := range t.rows {
		st := t.cell(row, "STATUS")
		if st == "" {
			st = "-"
		}
		if _, seen := counts[st]; !seen {
			order = append(order, st)
		}
		counts[st]++
		if !kubeRowHealthy(t, row) {
			unhealthy = append(unhealthy, row)
		}
	}

	// A listing where everything is fine and short enough to read is left
	// alone: reformatting it would save nothing and cost the reader the exact
	// columns they asked for.
	if len(unhealthy) == 0 && len(t.rows) <= 10 {
		return raw
	}

	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	var summary []string
	for _, st := range order {
		summary = append(summary, fmt.Sprintf("%d %s", counts[st], st))
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "%d resources: %s\n", len(t.rows), strings.Join(summary, ", "))

	if len(unhealthy) == 0 {
		fmt.Fprintf(&buf, "all ready — names: %s\n", joinNames(t, t.rows, 30))
		return buf.String()
	}

	fmt.Fprintf(&buf, "\nnot ready (%d):\n", len(unhealthy))
	fmt.Fprintf(&buf, "  %s\n", strings.Join(t.cols, "  "))
	shown := unhealthy
	if len(shown) > kubectlMaxRows {
		shown = shown[:kubectlMaxRows]
	}
	for _, row := range shown {
		fmt.Fprintf(&buf, "  %s\n", strings.Join(row, "  "))
	}
	if n := len(unhealthy) - len(shown); n > 0 {
		fmt.Fprintf(&buf, "  (%d more not-ready rows)\n", n)
	}
	return buf.String()
}

// kubeRowHealthy decides whether a row can be folded into a count. It errs
// toward "not healthy": an unrecognised status is surfaced in full rather than
// hidden behind a number.
func kubeRowHealthy(t *table, row []string) bool {
	status := strings.ToLower(t.cell(row, "STATUS"))
	switch status {
	case "running", "completed", "succeeded", "active", "bound", "":
	default:
		return false
	}
	// A finished Job reports 0/1 ready for the rest of its life. Holding it to
	// the readiness rule would report every completed job as broken.
	terminal := status == "completed" || status == "succeeded"

	// READY is "2/2" on pods, "3/3" on deployments and statefulsets. Anything
	// where the two sides differ is still coming up or has lost a replica.
	if ready := t.cell(row, "READY"); ready != "" && !terminal {
		if have, want, found := strings.Cut(ready, "/"); found {
			if strings.TrimSpace(have) != strings.TrimSpace(want) {
				return false
			}
		}
	}
	// A pod that keeps restarting reports Running between restarts, so the
	// status alone would call a crash loop healthy.
	if r := t.cell(row, "RESTARTS"); r != "" {
		// The column can read "12 (3m ago)".
		n, err := strconv.Atoi(strings.Fields(r)[0])
		if err == nil && n > 5 {
			return false
		}
	}
	return true
}

func joinNames(t *table, rows [][]string, max int) string {
	nameAt := t.index("NAME")
	if nameAt < 0 {
		nameAt = 0
	}
	var names []string
	for i, row := range rows {
		if i >= max {
			names = append(names, fmt.Sprintf("(+%d more)", len(rows)-max))
			break
		}
		if nameAt < len(row) {
			names = append(names, row[nameAt])
		}
	}
	return strings.Join(names, " ")
}

// ─── describe ─────────────────────────────────────────────────────────────────

// kubeDropBlocks are the indented blocks of a describe that are almost never
// what the reader came for and are frequently the bulk of the output: the full
// annotation set (which on a Helm-managed resource holds an entire rendered
// manifest), the label set, and every container's environment and mounts.
var kubeDropBlocks = map[string]bool{
	"Annotations":    true,
	"Labels":         true,
	"Environment":    true,
	"Mounts":         true,
	"Volumes":        true,
	"Tolerations":    true,
	"Node-Selectors": true,
}

// renderKubectlDescribe keeps the fields that describe what is wrong — status,
// conditions, image, and the event log — and drops the blocks that describe how
// the object was declared.
func renderKubectlDescribe(raw string) string {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	var out []string
	dropping := ""
	dropped := 0

	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		indent := len(line) - len(trimmed)

		if dropping != "" {
			// A dropped block continues while lines stay more indented than
			// the key that opened it.
			if trimmed != "" && indent > 0 {
				dropped++
				continue
			}
			dropping = ""
		}

		if key, _, found := strings.Cut(trimmed, ":"); found && kubeDropBlocks[strings.TrimSpace(key)] {
			dropping = strings.TrimSpace(key)
			dropped++
			out = append(out, strings.Repeat(" ", indent)+dropping+":  <omitted>")
			continue
		}
		out = append(out, line)
	}

	if dropped == 0 && len(out) <= kubectlMaxRows*2 {
		return raw
	}
	out = capLines(out, kubectlMaxRows*2)
	if dropped > 0 {
		out = append(out, fmt.Sprintf("(%d lines of annotations/labels/env omitted; --raw for all)", dropped))
	}
	return strings.Join(out, "\n") + "\n"
}

// ─── logs ─────────────────────────────────────────────────────────────────────

// renderLogStream collapses the repetition that dominates a log tail and keeps
// both ends of what is left.
func renderLogStream(raw string, max int) string {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	if len(lines) <= max/2 {
		return raw
	}
	deduped := dedupConsecutive(lines)
	return strings.Join(capLines(deduped, max), "\n") + "\n"
}
