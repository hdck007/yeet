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
	statsAudit bool
	statsLimit int
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "View analytics dashboard",
	RunE:  runStats,
}

func init() {
	statsCmd.Flags().BoolVar(&statsJSON, "json", false, "Output as JSON")
	statsCmd.Flags().BoolVar(&statsReset, "reset", false, "Clear all analytics data")
	statsCmd.Flags().BoolVar(&statsAudit, "audit", false, "Show the exact baseline each saving was measured against")
	statsCmd.Flags().IntVar(&statsLimit, "limit", 25, "Rows to show with --audit")
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

	if statsAudit {
		return printAudit(statsDB)
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

// printAudit shows where the numbers come from. A savings figure is only worth
// as much as the baseline it was measured against, so each row names that
// baseline command and how it was obtained.
func printAudit(statsDB *analytics.DB) error {
	kinds, err := statsDB.CountByBaselineKind()
	if err != nil {
		return fmt.Errorf("count baselines: %w", err)
	}
	rows, err := statsDB.GetAuditRows(statsLimit)
	if err != nil {
		return fmt.Errorf("query audit rows: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("No analytics data yet. Run some yeet commands first!")
		return nil
	}

	total := 0
	for _, n := range kinds {
		total += n
	}
	fmt.Printf("%d recorded invocations\n", total)
	fmt.Printf("  %-11s %d   measured against the command you actually ran (counted in totals)\n",
		analytics.BaselineAsInvoked, kinds[analytics.BaselineAsInvoked])
	if n := kinds[analytics.BaselineDirect]; n > 0 {
		fmt.Printf("  %-11s %d   no native command involved; baseline is the full content\n",
			analytics.BaselineDirect, n)
	}
	if n := kinds[analytics.BaselineSynthetic]; n > 0 {
		fmt.Printf("  %-11s %d   yeet had to run a larger form of the command — NOT counted in totals\n",
			analytics.BaselineSynthetic, n)
	}
	if n := kinds["unlabelled"]; n > 0 {
		fmt.Printf("  %-11s %d   recorded before baselines were tracked — NOT counted in totals\n",
			"unlabelled", n)
	}

	fmt.Println()
	fmt.Printf("%-6s %-11s %9s %9s %7s  %s\n", "cmd", "baseline", "baseline", "printed", "saved", "baseline command")
	fmt.Println(strings.Repeat("─", 100))
	for _, r := range rows {
		pct := 0.0
		if r.CharsRaw > 0 {
			pct = float64(r.CharsSaved) / float64(r.CharsRaw) * 100
		}
		bc := r.BaselineCmd
		if bc == "" {
			bc = "(not recorded)"
		}
		if len(bc) > 46 {
			bc = bc[:43] + "..."
		}
		fmt.Printf("%-6s %-11s %9s %9s %6.1f%%  %s\n",
			r.Command, r.BaselineKind,
			formatNumber(r.CharsRaw), formatNumber(r.CharsPrinted), pct, bc)
	}
	fmt.Println()
	fmt.Println("Re-run any baseline command above and compare it with the yeet form to check a row.")
	return nil
}
