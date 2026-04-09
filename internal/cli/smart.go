package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/hdck007/yeet/internal/filter"
	"github.com/spf13/cobra"
)

var smartCmd = &cobra.Command{
	Use:   "smart <file>",
	Short: "2-line heuristic code summary",
	Args:  cobra.ExactArgs(1),
	RunE:  runSmart,
}

func init() {
	rootCmd.AddCommand(smartCmd)
}

func runSmart(cmd *cobra.Command, args []string) error {
	return runWithFallback("smart", args, func() error {
		return runSmartImpl(args)
	}, Fallback{
		Bin: "cat",
		Args: func(a []string) []string { return a },
	})
}

func runSmartImpl(args []string) error {
	start := time.Now()
	filename := args[0]

	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	info, err := os.Stat(filename)
	if err != nil {
		return err
	}

	content := string(data)
	rendered := filter.FileSummary(content, filename, info.Size())
	improved := printBetter(content, rendered)

	if improved && !strings.Contains(rendered, "(no declarations found)") {
		fmt.Printf("→ next: yeet read %s --lines N-M  (pick N-M from line numbers above; never read the full file)\n", filename)
	}

	if improved && !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "smart",
			ArgsSummary:   filename,
			CharsRaw:      len(content),
			CharsRendered: len(rendered),
			ExitCode:      0,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}

	return nil
}
