package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var nextCmd = &cobra.Command{
	Use:   "next [args...]",
	Short: "Next.js build — routes and bundle sizes only",
	Args:  cobra.ArbitraryArgs,
	RunE:  runNext,
}

func init() {
	rootCmd.AddCommand(nextCmd)
}

func runNext(cmd *cobra.Command, args []string) error {
	start := time.Now()

	buildArgs := append([]string{"build"}, args...)

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	var result yeetexec.Result
	if yeetexec.Available("next") {
		result = yeetexec.Run(ctx, "next", buildArgs...)
	} else {
		result = yeetexec.Run(ctx, "npx", append([]string{"next"}, buildArgs...)...)
	}

	raw := result.Stdout + result.Stderr
	rendered := filterNextBuild(raw, result.ExitCode)
	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "next",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(raw),
			CharsRendered: len(rendered),
			ExitCode:      result.ExitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

var (
	nextRouteRE  = regexp.MustCompile(`^\s*(○|●|λ|ƒ|\+|─)\s+(/\S*)`)
	nextBundleRE = regexp.MustCompile(`\d+(\.\d+)?\s*(kB|MB|B)`)
	nextErrorRE  = regexp.MustCompile(`(?i)(error|failed|warning)`)
)

func filterNextBuild(output string, exitCode int) string {
	clean := ansiRE.ReplaceAllString(output, "")

	var routes, errors, warnings, summary []string

	for _, line := range strings.Split(clean, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)

		if nextRouteRE.MatchString(trimmed) || nextBundleRE.MatchString(trimmed) {
			routes = append(routes, trimmed)
		} else if strings.Contains(lower, "error") && !strings.Contains(lower, "no errors") {
			errors = append(errors, trimmed)
		} else if strings.Contains(lower, "warn") {
			warnings = append(warnings, trimmed)
		} else if strings.Contains(lower, "compil") || strings.Contains(lower, "built") ||
			strings.Contains(lower, "total") || strings.Contains(lower, "success") {
			summary = append(summary, trimmed)
		}
	}

	var buf strings.Builder
	if exitCode != 0 {
		buf.WriteString("[FAIL]\n")
		for _, e := range errors {
			buf.WriteString("  " + e + "\n")
		}
		return buf.String()
	}

	for _, l := range summary {
		buf.WriteString(l + "\n")
	}
	if len(routes) > 0 {
		buf.WriteString("\nRoutes:\n")
		for _, r := range routes {
			buf.WriteString("  " + r + "\n")
		}
	}
	if len(warnings) > 0 {
		buf.WriteString(fmt.Sprintf("\n[%d warnings]\n", len(warnings)))
	}
	if buf.Len() == 0 {
		return clean
	}
	return buf.String()
}
