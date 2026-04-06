package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/hdck007/yeet/internal/filter"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff <file1> <file2>",
	Short: "Condensed diff output",
	Args:  cobra.ExactArgs(2),
	RunE:  runDiff,
}

func init() {
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	start := time.Now()

	file1, file2 := args[0], args[1]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := yeetexec.Run(ctx, "diff", "-u", file1, file2)
	rawOutput := result.Stdout

	if rawOutput == "" && result.ExitCode == 0 {
		rendered := "files are identical\n"
		fmt.Print(rendered)
		recordDiffAnalytics(args, rawOutput, rendered, start)
		return nil
	}

	rendered := filter.CompactDiff(rawOutput)
	fmt.Print(rendered)
	recordDiffAnalytics(args, rawOutput, rendered, start)
	return nil
}

func recordDiffAnalytics(args []string, raw, rendered string, start time.Time) {
	if noAnalytics || db == nil {
		return
	}
	if err := db.RecordUsage(analytics.Usage{
		Command:       "diff",
		ArgsSummary:   strings.Join(args, " "),
		CharsRaw:      len(raw),
		CharsRendered: len(rendered),
		ExitCode:      0,
		DurationMs:    time.Since(start).Milliseconds(),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
	}
}
