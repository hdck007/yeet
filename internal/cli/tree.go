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

var treeCmd = &cobra.Command{
	Use:   "tree [path]",
	Short: "Directory tree with noise-dir filtering",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTree,
}

func init() {
	rootCmd.AddCommand(treeCmd)
}

// noiseDirs are excluded by default
var noiseDirs = []string{
	"node_modules", ".git", "target", "__pycache__", ".next", "dist",
	"build", ".cache", ".turbo", ".vercel", ".pytest_cache", ".mypy_cache",
	".tox", ".venv", "venv", "env", ".env", "coverage", ".nyc_output",
	".DS_Store", ".idea", ".vscode", ".vs",
}

func runTree(cmd *cobra.Command, args []string) error {
	start := time.Now()

	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	var raw, rendered string

	if yeetexec.Available("tree") {
		ignorePattern := strings.Join(noiseDirs, "|")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result := yeetexec.Run(ctx, "tree", "-I", ignorePattern, path)
		raw = result.Stdout
		rendered = raw // tree output is already compact
	} else {
		// Fallback: use yeet's own tree renderer
		opts := filter.DefaultTreeOpts()
		tree, err := filter.BuildTree(path, opts)
		if err != nil {
			return err
		}
		var buf strings.Builder
		filter.RenderTree(&buf, tree)
		raw = buf.String()
		rendered = raw
	}

	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "tree",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(raw),
			CharsRendered: len(rendered),
			ExitCode:      0,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}
