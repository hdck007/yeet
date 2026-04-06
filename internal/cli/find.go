package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/hdck007/yeet/internal/ignore"
	"github.com/spf13/cobra"
)

var findCmd = &cobra.Command{
	Use:   "find <pattern> [path]",
	Short: "Compact find results",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runFind,
}

func init() {
	rootCmd.AddCommand(findCmd)
}

func runFind(cmd *cobra.Command, args []string) error {
	start := time.Now()

	pattern := args[0]
	searchPath := "."
	if len(args) > 1 {
		searchPath = args[1]
	}

	absPath, err := filepath.Abs(searchPath)
	if err != nil {
		return err
	}

	matcher := ignore.NewMatcher(absPath)
	var results []string

	err = filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// Never skip the root directory itself
		if d.IsDir() && path != absPath && matcher.ShouldIgnore(d.Name(), true) {
			return fs.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if matcher.ShouldIgnore(d.Name(), false) {
			return nil
		}

		matched, _ := filepath.Match(pattern, d.Name())
		if matched {
			rel, _ := filepath.Rel(absPath, path)
			results = append(results, rel)
		}
		return nil
	})
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	for _, r := range results {
		fmt.Fprintln(&buf, r)
	}
	fmt.Fprintf(&buf, "(found %d files)\n", len(results))

	rendered := buf.String()
	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		// Estimate raw: a typical `find` outputs full paths with metadata
		rawEstimate := len(rendered) * 2
		if err := db.RecordUsage(analytics.Usage{
			Command:       "find",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      rawEstimate,
			CharsRendered: len(rendered),
			ExitCode:      0,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}

	return nil
}
