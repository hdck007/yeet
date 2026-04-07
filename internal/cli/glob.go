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

var globCmd = &cobra.Command{
	Use:   "glob <pattern> [path]",
	Short: "Fast file pattern matching with compact output",
	Long:  "Finds files matching a glob pattern (e.g., \"**/*.go\", \"src/**/*.ts\"). Returns paths sorted by modification time, most recent first.",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runGlob,
}

func init() {
	rootCmd.AddCommand(globCmd)
}

func runGlob(cmd *cobra.Command, args []string) error {
	return runWithFallback("glob", args, func() error {
		return runGlobImpl(args)
	}, Fallback{
		Bin: "find",
		Args: func(a []string) []string {
			path := "."
			if len(a) > 1 {
				path = a[1]
			}
			pat := a[0]
			for i := len(pat) - 1; i >= 0; i-- {
				if pat[i] == '/' {
					pat = pat[i+1:]
					break
				}
			}
			return []string{path, "-name", pat}
		},
	})
}

func runGlobImpl(args []string) error {
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

	// Check if pattern uses ** (recursive)
	isRecursive := strings.Contains(pattern, "**")
	// Extract the file glob part after **/ if present
	filePattern := pattern
	if isRecursive {
		parts := strings.SplitN(pattern, "**/", 2)
		if len(parts) == 2 {
			filePattern = parts[1]
		}
	}

	type fileEntry struct {
		path    string
		modTime time.Time
	}

	var results []fileEntry

	err = filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && path != absPath && matcher.ShouldIgnore(d.Name(), true) {
			return fs.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if matcher.ShouldIgnore(d.Name(), false) {
			return nil
		}

		rel, _ := filepath.Rel(absPath, path)

		var matched bool
		if isRecursive {
			// Match just the filename part against the file pattern
			matched, _ = filepath.Match(filePattern, d.Name())
		} else {
			// Non-recursive: match against relative path or filename
			matched, _ = filepath.Match(pattern, d.Name())
			if !matched {
				matched, _ = filepath.Match(pattern, rel)
			}
		}

		if matched {
			info, err := d.Info()
			modTime := time.Time{}
			if err == nil {
				modTime = info.ModTime()
			}
			results = append(results, fileEntry{path: rel, modTime: modTime})
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Sort by modification time, most recent first
	sort.Slice(results, func(i, j int) bool {
		return results[i].modTime.After(results[j].modTime)
	})

	var buf bytes.Buffer
	for _, r := range results {
		fmt.Fprintln(&buf, r.path)
	}
	fmt.Fprintf(&buf, "(%d files matched)\n", len(results))

	rendered := buf.String()
	// Raw estimate: find command typically outputs full paths with metadata (~2x)
	rawOutput := strings.Repeat("x", len(rendered)*2)
	improved := printBetter(rawOutput, rendered)

	if improved && !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "glob",
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
