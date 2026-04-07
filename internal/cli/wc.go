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

var wcCmd = &cobra.Command{
	Use:                "wc [flags] [file...]",
	Short:              "Compact word/line/byte count",
	DisableFlagParsing: true,
	RunE:               runWC,
}

func init() {
	rootCmd.AddCommand(wcCmd)
}

func runWC(cmd *cobra.Command, args []string) error {
	start := time.Now()

	if !yeetexec.Available("wc") {
		return fmt.Errorf("wc not found in PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := yeetexec.Run(ctx, "wc", args...)
	raw := result.Stdout + result.Stderr

	rendered := filterWCOutput(raw, args)
	improved := printBetter(raw, rendered)

	if improved && !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "wc",
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

var wcSpaceRE = regexp.MustCompile(`\s+`)

func filterWCOutput(output string, args []string) string {
	// Detect mode from flags
	flags := ""
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags += a
		}
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 1 {
		// Single file: compact format
		parts := wcSpaceRE.Split(strings.TrimSpace(lines[0]), -1)
		switch {
		case strings.Contains(flags, "l") && !strings.Contains(flags, "w") && !strings.Contains(flags, "c"):
			if len(parts) >= 1 {
				return parts[0] + "L\n"
			}
		case strings.Contains(flags, "w") && !strings.Contains(flags, "l") && !strings.Contains(flags, "c"):
			if len(parts) >= 1 {
				return parts[0] + "W\n"
			}
		case strings.Contains(flags, "c") && !strings.Contains(flags, "l") && !strings.Contains(flags, "w"):
			if len(parts) >= 1 {
				return parts[0] + "B\n"
			}
		default:
			// Full: NL NW NB
			if len(parts) >= 3 {
				return fmt.Sprintf("%sL %sW %sB\n", parts[0], parts[1], parts[2])
			}
		}
	}

	// Multiple files: strip common path prefix, align columns
	var result strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts := wcSpaceRE.Split(trimmed, -1)
		if len(parts) >= 4 {
			result.WriteString(fmt.Sprintf("%-8s %-8s %-8s %s\n", parts[0], parts[1], parts[2], parts[3]))
		} else {
			result.WriteString(line + "\n")
		}
	}
	return result.String()
}
