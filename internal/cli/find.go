package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
	return runWithFallback("find", args, func() error {
		return runFindImpl(args)
	}, Fallback{
		Bin: "find",
		Args: func(a []string) []string {
			path := "."
			if len(a) > 1 {
				path = a[1]
			}
			return []string{path, "-name", a[0]}
		},
	})
}

func runFindImpl(args []string) error {
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
		if d.IsDir() && path != absPath &&
			(strings.HasPrefix(d.Name(), ".") || matcher.ShouldIgnore(d.Name(), true)) {
			return fs.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		// Skip hidden files (starting with ".") — mirrors rtk/ignore crate default
		if strings.HasPrefix(d.Name(), ".") || matcher.ShouldIgnore(d.Name(), false) {
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

	// Group files by their parent directory (rtk format: "NF MD:\n\ndir/ f1 f2 ...")
	dirOrder := []string{}
	dirFiles := map[string][]string{}
	dirSet := map[string]bool{}

	for _, r := range results {
		dir := filepath.Dir(r)
		if !dirSet[dir] {
			dirSet[dir] = true
			dirOrder = append(dirOrder, dir)
		}
		dirFiles[dir] = append(dirFiles[dir], filepath.Base(r))
	}

	// Sort dirs: root "." first, then alphabetically
	sort.Slice(dirOrder, func(i, j int) bool {
		if dirOrder[i] == "." {
			return true
		}
		if dirOrder[j] == "." {
			return false
		}
		return dirOrder[i] < dirOrder[j]
	})

	// Cap at 50 total files shown (mirrors rtk default max_results=50)
	const findMaxResults = 50
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%dF %dD:\n\n", len(results), len(dirOrder))

	shown := 0
	for _, dir := range dirOrder {
		if shown >= findMaxResults {
			break
		}
		label := "./"
		if dir != "." {
			label = dir + "/"
		}
		files := dirFiles[dir]
		if shown+len(files) > findMaxResults {
			files = files[:findMaxResults-shown]
		}
		fmt.Fprintf(&buf, "%s %s\n", label, strings.Join(files, " "))
		shown += len(files)
	}
	if len(results) > shown {
		fmt.Fprintf(&buf, "+%d more\n", len(results)-shown)
	}

	rendered := buf.String()
	// Estimate raw: a typical `find` outputs full paths with metadata (~2x)
	rawOutput := strings.Repeat("x", len(rendered)*2)
	improved := printBetter(rawOutput, rendered)

	if improved && !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "find",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(rawOutput),
			CharsRendered: len(rendered),
			ExitCode:      0,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}

	return nil
}
