package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/spf13/cobra"
)

var (
	noAnalytics bool
	rawOutput   bool
	db          *analytics.DB

	// Version is set via ldflags at build time.
	Version = "dev"
)

var rootCmd = &cobra.Command{
	Use:   "yeet",
	Short: "Token-optimized CLI wrapper",
	Long:  "Yeet wraps common system commands and produces noise-filtered, compact output optimized for LLM token consumption.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "version" || cmd.Name() == "stats" {
			// stats command manages its own DB connection
			if cmd.Name() == "version" {
				return nil
			}
		}

		if noAnalytics || os.Getenv("YEET_NO_ANALYTICS") == "1" {
			noAnalytics = true
			return nil
		}

		var err error
		db, err = analytics.Open()
		if err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics unavailable: %v\n", err)
			noAnalytics = true
		}
		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		if db != nil {
			return db.Close()
		}
		return nil
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&noAnalytics, "no-analytics", false, "Disable analytics recording")
	rootCmd.PersistentFlags().BoolVar(&rawOutput, "raw", false, "Pass through unfiltered output")
}

func Execute() error {
	cmd, err := rootCmd.ExecuteC()
	if err != nil && !noAnalytics && db != nil {
		_ = db.RecordFailure(analytics.Failure{
			Subcmd:   cmd.Name(),
			FullCmd:  strings.Join(os.Args, " "),
			ExitCode: 1,
			Stderr:   err.Error(),
		})
		_ = db.Close()
		db = nil
	}
	return err
}
