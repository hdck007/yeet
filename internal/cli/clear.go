package cli

import (
	"fmt"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/spf13/cobra"
)

var clearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all analytics data",
	RunE:  runClear,
}

func init() {
	rootCmd.AddCommand(clearCmd)
}

func runClear(cmd *cobra.Command, args []string) error {
	clearDB, err := analytics.Open()
	if err != nil {
		return fmt.Errorf("open analytics: %w", err)
	}
	defer clearDB.Close()

	if err := clearDB.ResetStats(); err != nil {
		return fmt.Errorf("clear analytics: %w", err)
	}

	fmt.Println("Analytics cleared.")
	return nil
}
