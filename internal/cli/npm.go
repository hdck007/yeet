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

var npmCmd = &cobra.Command{
	Use:                "npm [command] [args...]",
	Short:              "npm with auto 'run' injection and filtered output",
	Args:               cobra.ArbitraryArgs,
	RunE:               runNPM,
	DisableFlagParsing: true,
}

func init() {
	rootCmd.AddCommand(npmCmd)
}

// npmBuiltins are subcommands that should NOT get "run" injected
var npmBuiltins = map[string]bool{
	"install": true, "i": true, "ci": true,
	"uninstall": true, "remove": true, "rm": true,
	"update": true, "up": true,
	"list": true, "ls": true, "outdated": true,
	"init": true, "create": true, "publish": true, "pack": true,
	"link": true, "audit": true, "fund": true,
	"exec": true, "explain": true, "why": true,
	"search": true, "view": true, "info": true, "show": true,
	"config": true, "set": true, "get": true,
	"cache": true, "prune": true, "dedupe": true,
	"run": true, "test": true, "start": true, "stop": true, "restart": true,
	"help": true, "version": true,
}

func runNPM(cmd *cobra.Command, args []string) error {
	start := time.Now()

	if !yeetexec.Available("npm") {
		return fmt.Errorf("npm not found in PATH")
	}

	// Auto-inject "run" for unknown subcommands
	npmArgs := args
	if len(args) > 0 && !npmBuiltins[args[0]] && !strings.HasPrefix(args[0], "-") {
		npmArgs = append([]string{"run"}, args...)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	result := yeetexec.Run(ctx, "npm", npmArgs...)
	raw := result.Stdout + result.Stderr

	rendered := filterNPMOutput(raw, result.ExitCode)
	improved := printBetter(raw, rendered)

	if improved && !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "npm",
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

func filterNPMOutput(output string, exitCode int) string {
	var warnings, errors, important, other []string

	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "npm warn") || strings.HasPrefix(lower, "npm warning"):
			warnings = append(warnings, trimmed)
		case strings.HasPrefix(lower, "npm err") || strings.HasPrefix(lower, "npm error"):
			errors = append(errors, trimmed)
		case strings.Contains(lower, "added ") || strings.Contains(lower, "removed ") ||
			strings.Contains(lower, "audited ") || strings.Contains(lower, "found "):
			important = append(important, trimmed)
		default:
			other = append(other, trimmed)
		}
	}

	var buf strings.Builder
	if exitCode != 0 {
		buf.WriteString("[FAIL]\n")
		for _, e := range errors {
			buf.WriteString("  " + e + "\n")
		}
	}
	for _, l := range important {
		buf.WriteString(l + "\n")
	}
	if len(warnings) > 0 {
		buf.WriteString(fmt.Sprintf("[%d warnings — run with --raw to see all]\n", len(warnings)))
	}
	if buf.Len() == 0 {
		// Pass through if nothing matched
		for _, l := range other {
			buf.WriteString(l + "\n")
		}
	}
	return buf.String()
}
