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

var prettierCmd = &cobra.Command{
	Use:   "prettier [args...]",
	Short: "Run prettier, show only files that need formatting",
	Args:  cobra.ArbitraryArgs,
	RunE:  runPrettier,
}

func init() {
	rootCmd.AddCommand(prettierCmd)
}

func runPrettier(cmd *cobra.Command, args []string) error {
	start := time.Now()

	prettier := detectPrettierBin()
	if prettier == "" {
		return fmt.Errorf("prettier not found; install with npm install -D prettier")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var result yeetexec.Result
	if prettier == "npx" {
		result = yeetexec.Run(ctx, "npx", append([]string{"prettier"}, args...)...)
	} else {
		result = yeetexec.Run(ctx, prettier, args...)
	}

	raw := result.Stdout + result.Stderr
	rendered := filterPrettierOutput(raw, result.ExitCode)
	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "prettier",
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

func detectPrettierBin() string {
	if yeetexec.Available("prettier") {
		return "prettier"
	}
	if yeetexec.Available("npx") {
		return "npx"
	}
	return ""
}

func filterPrettierOutput(output string, exitCode int) string {
	if strings.TrimSpace(output) == "" {
		if exitCode == 0 {
			return "prettier: all files formatted\n"
		}
		return "prettier: no output (did you pass files?)\n"
	}

	// In --check mode, prettier prints one file per line for files that need formatting
	var needsFormat []string
	var other []string

	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "code style issues") || strings.HasSuffix(trimmed, "ms") {
			// Skip "Code style issues found in X files. Run Prettier..."
			// Skip timing lines
			continue
		}
		// In check mode, output lines are file paths that need formatting
		if !strings.HasPrefix(trimmed, "[") && !strings.Contains(trimmed, ":") {
			needsFormat = append(needsFormat, trimmed)
		} else {
			other = append(other, trimmed)
		}
	}

	var buf strings.Builder
	if len(needsFormat) > 0 {
		buf.WriteString(fmt.Sprintf("[prettier] %d file(s) need formatting:\n", len(needsFormat)))
		for _, f := range needsFormat {
			buf.WriteString("  " + f + "\n")
		}
	}
	for _, l := range other {
		buf.WriteString(l + "\n")
	}
	if buf.Len() == 0 {
		return "prettier: all files formatted\n"
	}
	return buf.String()
}
