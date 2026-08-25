package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

// pnpm and yarn produce the same shape of noise as npm — a progress log, a
// deprecation warning per transitive dependency, a funding notice — around one
// or two lines that matter. They differ only in how they prefix those lines,
// so the npm filter is generalised rather than duplicated.

var pnpmCmd = &cobra.Command{
	Use:                "pnpm [command] [args...]",
	Short:              "pnpm with progress and deprecation noise collapsed",
	Args:               cobra.ArbitraryArgs,
	RunE:               func(c *cobra.Command, a []string) error { return runPkgManager("pnpm", a) },
	DisableFlagParsing: true,
}

var yarnCmd = &cobra.Command{
	Use:                "yarn [command] [args...]",
	Short:              "yarn with progress and deprecation noise collapsed",
	Args:               cobra.ArbitraryArgs,
	RunE:               func(c *cobra.Command, a []string) error { return runPkgManager("yarn", a) },
	DisableFlagParsing: true,
}

func init() {
	rootCmd.AddCommand(pnpmCmd)
	rootCmd.AddCommand(yarnCmd)
}

func runPkgManager(bin string, args []string) error {
	start := time.Now()
	args = stripYeetFlags(args)

	if !yeetexec.Available(bin) {
		return fmt.Errorf("%s not found in PATH", bin)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	result := yeetexec.Run(ctx, bin, args...)
	raw := result.Stdout + result.Stderr

	rendered := filterPkgManagerOutput(bin, raw, result.ExitCode)
	if rawOutput {
		rendered = raw
	}
	printed, _ := printBetterN(raw, rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       bin,
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(raw),
			CharsRendered: len(rendered),
			CharsPrinted:  printed,
			BaselineCmd:   bin + " " + strings.Join(args, " "),
			YeetCmd:       "yeet " + bin + " " + strings.Join(args, " "),
			BaselineKind:  analytics.BaselineAsInvoked,
			ExitCode:      result.ExitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

// pkgLineKind classifies one line of package-manager output.
type pkgLineKind int

const (
	pkgOther pkgLineKind = iota
	pkgWarn
	pkgError
	pkgSummary
	pkgProgress
)

// classifyPkgLine recognises the three prefix dialects. npm writes
// "npm warn"/"npm error", pnpm writes bare "WARN "/" ERR_PNPM_*", yarn writes
// "warning"/"error".
func classifyPkgLine(line string) pkgLineKind {
	l := strings.ToLower(strings.TrimSpace(line))
	switch {
	case strings.HasPrefix(l, "npm err") || strings.HasPrefix(l, "npm error"),
		strings.HasPrefix(l, "error"), strings.Contains(l, "err_pnpm_"),
		strings.HasPrefix(l, "elifecycle"):
		return pkgError
	case strings.HasPrefix(l, "npm warn") || strings.HasPrefix(l, "npm warning"),
		strings.HasPrefix(l, "warn "), strings.HasPrefix(l, "warning"),
		strings.HasPrefix(l, "deprecated"):
		return pkgWarn
	case strings.HasPrefix(l, "packages:"), strings.HasPrefix(l, "progress:"),
		strings.HasPrefix(l, "resolving:"), strings.HasPrefix(l, "downloading"),
		strings.HasPrefix(l, "resolution step"), strings.HasPrefix(l, "fetching"),
		strings.HasPrefix(l, "[1/"), strings.HasPrefix(l, "[2/"),
		strings.HasPrefix(l, "[3/"), strings.HasPrefix(l, "[4/"),
		strings.HasPrefix(l, "success saved lockfile"):
		return pkgProgress
	case strings.Contains(l, "added "), strings.Contains(l, "removed "),
		strings.Contains(l, "audited "), strings.Contains(l, "found "),
		strings.Contains(l, "packages in "), strings.Contains(l, "done in "),
		strings.HasPrefix(l, "up to date"), strings.HasPrefix(l, "dependencies:"),
		strings.HasPrefix(l, "+ "), strings.HasPrefix(l, "- "):
		return pkgSummary
	}
	return pkgOther
}

// filterPkgManagerOutput keeps every error, every line that states what changed,
// and a count of the warnings. The warnings themselves are the bulk of an
// install log and are almost never acted on — but a failure has to survive
// intact, because that is the one case where the detail is the point.
func filterPkgManagerOutput(bin, output string, exitCode int) string {
	var warnings, errors, summary, other []string

	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(ansiRE.ReplaceAllString(line, ""))
		if trimmed == "" {
			continue
		}
		switch classifyPkgLine(trimmed) {
		case pkgError:
			errors = append(errors, trimmed)
		case pkgWarn:
			warnings = append(warnings, trimmed)
		case pkgSummary:
			summary = append(summary, trimmed)
		case pkgProgress:
			// Progress bars and per-package fetch lines describe work that has
			// already finished by the time anyone reads this.
		default:
			other = append(other, trimmed)
		}
	}

	var buf strings.Builder
	if exitCode != 0 {
		fmt.Fprintf(&buf, "[FAIL] %s exit %d\n", bin, exitCode)
		for _, e := range errors {
			buf.WriteString("  " + e + "\n")
		}
		// On a failure the unclassified lines are usually the stack trace or
		// the build log that explains it, so they are kept.
		for _, l := range capLines(other, 40) {
			buf.WriteString("  " + l + "\n")
		}
	}
	for _, l := range summary {
		buf.WriteString(l + "\n")
	}
	if len(warnings) > 0 {
		fmt.Fprintf(&buf, "[%d warnings suppressed — --raw to see all]\n", len(warnings))
	}
	if buf.Len() == 0 {
		for _, l := range capLines(other, 40) {
			buf.WriteString(l + "\n")
		}
	}
	return buf.String()
}
