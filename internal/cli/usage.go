package cli

import (
		"fmt"
		"strings"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/spf13/cobra"
)

var (
	usageReset bool
)

var usageCommand = &cobra.Command{
	Use:   "usage",
	Short: "View usage",
	RunE:  runGetUsage,
}

func init() {
	usageCommand.Flags().BoolVar(&usageReset, "reset", false, "Clear all analytics data")
	rootCmd.AddCommand(usageCommand)
}

func runGetUsage(cmd *cobra.Command, args []string) error {
	statsDB, err := analytics.Open()
	if err != nil {
		return fmt.Errorf("open analytics: %w", err)
	}
	defer statsDB.Close()

	if usageReset {
		if err := statsDB.ResetStats(); err != nil {
			return fmt.Errorf("reset analytics: %w", err)
		}
		fmt.Println("Analytics data cleared.")
		return nil
	}

	stats, err := statsDB.GetUsages()
	if err != nil {
		return fmt.Errorf("query analytics: %w", err)
	}

	if len(stats) == 0 {
		fmt.Println("No analytics data yet. Run some yeet commands first!")
		return nil
	}

	printUsageTable(stats)
	return nil
}

func printUsageTable(stats []analytics.CommandUsages) {
	fmt.Printf("%-20s  %s\n", "Command", "Args")
	fmt.Println(strings.Repeat("─", 50))

	for _, s := range stats {
		fmt.Printf("%-20s  %s\n", s.CommandName, s.ArgsSummary)
	}

	fmt.Println(strings.Repeat("─", 70))
}
