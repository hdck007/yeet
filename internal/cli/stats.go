package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/spf13/cobra"
)

var (
	statsJSON  bool
	statsReset bool
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "View analytics dashboard",
	RunE:  runStats,
}

func init() {
	statsCmd.Flags().BoolVar(&statsJSON, "json", false, "Output as JSON")
	statsCmd.Flags().BoolVar(&statsReset, "reset", false, "Clear all analytics data")
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) error {
	statsDB, err := analytics.Open()
	if err != nil {
		return fmt.Errorf("open analytics: %w", err)
	}
	defer statsDB.Close()

	if statsReset {
		if err := statsDB.ResetStats(); err != nil {
			return fmt.Errorf("reset analytics: %w", err)
		}
		fmt.Println("Analytics data cleared.")
		return nil
	}

	stats, err := statsDB.GetAllStats()
	if err != nil {
		return fmt.Errorf("query analytics: %w", err)
	}

	if len(stats) == 0 {
		fmt.Println("No analytics data yet. Run some yeet commands first!")
		return nil
	}

	if statsJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(stats)
	}

	printStatsTable(stats)
	return nil
}

func printStatsTable(stats []analytics.CommandStats) {
	// Header
	fmt.Printf("%-10s %6s %12s %14s %8s %13s\n",
		"Command", "Runs", "Chars Raw", "Chars Rendered", "Saved", "Tokens Saved")
	fmt.Println(strings.Repeat("─", 70))

	var totalRuns, totalRaw, totalRendered, totalSaved, totalTokens int
	for _, s := range stats {
		savedPct := 0.0
		if s.CharsRaw > 0 {
			savedPct = float64(s.CharsSaved) / float64(s.CharsRaw) * 100
		}
		fmt.Printf("%-10s %6d %12s %14s %7.1f%% %13s\n",
			s.CommandName,
			s.TotalRuns,
			formatNumber(s.CharsRaw),
			formatNumber(s.CharsRendered),
			savedPct,
			formatNumber(s.TokensSaved))

		totalRuns += s.TotalRuns
		totalRaw += s.CharsRaw
		totalRendered += s.CharsRendered
		totalSaved += s.CharsSaved
		totalTokens += s.TokensSaved
	}

	fmt.Println(strings.Repeat("─", 70))
	totalPct := 0.0
	if totalRaw > 0 {
		totalPct = float64(totalSaved) / float64(totalRaw) * 100
	}
	fmt.Printf("%-10s %6d %12s %14s %7.1f%% %13s\n",
		"Total",
		totalRuns,
		formatNumber(totalRaw),
		formatNumber(totalRendered),
		totalPct,
		formatNumber(totalTokens))
}

func formatNumber(n int) string {
	if n < 0 {
		return "-" + formatNumber(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
