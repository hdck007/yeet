package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var failuresLimit int
var failuresClear bool

var failuresCmd = &cobra.Command{
	Use:   "failures",
	Short: "Show recorded yeet command failures",
	Long:  "Display a table of all yeet commands that exited non-zero, with the exact command, exit code, and stderr captured by the PostToolUse hook.",
	RunE:  runFailures,
}

func init() {
	failuresCmd.Flags().IntVarP(&failuresLimit, "limit", "n", 50, "Max failures to show")
	failuresCmd.Flags().BoolVar(&failuresClear, "clear", false, "Clear all recorded failures")
	rootCmd.AddCommand(failuresCmd)
}

func runFailures(cmd *cobra.Command, args []string) error {
	if db == nil {
		return fmt.Errorf("analytics DB unavailable")
	}

	if failuresClear {
		if err := db.ClearFailures(); err != nil {
			return fmt.Errorf("clear failures: %w", err)
		}
		fmt.Println("ok — failures cleared")
		return nil
	}

	rows, err := db.GetFailures(failuresLimit)
	if err != nil {
		return fmt.Errorf("query failures: %w", err)
	}

	if len(rows) == 0 {
		fmt.Println("No failures recorded.")
		return nil
	}

	fmt.Printf("%-4s  %-19s  %-4s  %-8s  %s\n", "#", "Time", "Exit", "Subcmd", "Command")
	fmt.Println(strings.Repeat("─", 100))

	for i, f := range rows {
		// Trim timestamp to seconds
		ts := f.CreatedAt
		if len(ts) > 19 {
			ts = ts[:19]
		}
		ts = strings.ReplaceAll(ts, "T", " ")

		// Truncate long commands for the table
		cmd := f.FullCmd
		if len(cmd) > 60 {
			cmd = cmd[:57] + "..."
		}

		fmt.Printf("%-4d  %-19s  %-4d  %-8s  %s\n", i+1, ts, f.ExitCode, f.Subcmd, cmd)

		if f.Stderr != "" {
			stderr := strings.TrimSpace(f.Stderr)
			// Print up to 2 lines of stderr, indented
			lines := strings.SplitN(stderr, "\n", 3)
			for _, line := range lines[:min(2, len(lines))] {
				if strings.TrimSpace(line) != "" {
					fmt.Printf("      stderr: %s\n", line)
				}
			}
			if len(lines) > 2 {
				fmt.Printf("      stderr: (%d more lines)\n", strings.Count(stderr, "\n")-1)
			}
		}
	}

	fmt.Printf("\n%d failure(s). Use `yeet failures --clear` to reset.\n", len(rows))
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
