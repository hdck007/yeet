package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/hdck007/yeet/internal/ignore"
	"github.com/spf13/cobra"
)

const grepMaxPerFile = 25   // matches per file (mirrors rtk default)
const grepMaxResults = 200  // global cap across all files (mirrors rtk default)
const grepLineMaxLen = 80   // chars per line before truncation

var grepCmd = &cobra.Command{
	Use:   "grep <pattern> [path]",
	Short: "Compact grouped search results",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runGrep,
}

func init() {
	rootCmd.AddCommand(grepCmd)
}

func runGrep(cmd *cobra.Command, args []string) error {
	start := time.Now()

	pattern := args[0]
	searchPath := "."
	if len(args) > 1 {
		searchPath = args[1]
	}

	var rawOutput, rendered string

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if yeetexec.Available("grep") {
		result := yeetexec.Run(ctx, "grep", "-rn", pattern, searchPath)
		rawOutput = result.Stdout
		rendered = formatGrepCompact(rawOutput)
	} else {
		rawOutput, rendered = nativeGrep(pattern, searchPath)
	}

	improved := printBetter(rawOutput, rendered)

	if improved && !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "grep",
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

type grepMatch struct {
	lineNum string
	content string
}

// abbreviatePath shortens a long path to /.../parent/file.
func abbreviatePath(path string) string {
	parts := strings.Split(path, string(os.PathSeparator))
	if len(parts) <= 4 {
		return path
	}
	return "/.../" + strings.Join(parts[len(parts)-3:], "/")
}

// truncateLine trims a line to maxLen, appending "…" if cut.
func truncateLine(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= grepLineMaxLen {
		return s
	}
	return s[:grepLineMaxLen] + "…"
}

// formatGrepCompact converts grep -rn output to rtk-style grouped format:
//
//	N matches in MF:
//
//	[file] /.../path (K):
//	   linenum: content
func formatGrepCompact(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "(no matches)\n"
	}

	// file → ordered list of matches
	fileOrder := []string{}
	fileMatches := map[string][]grepMatch{}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		file, lineNum, content := parts[0], parts[1], parts[2]
		if _, seen := fileMatches[file]; !seen {
			fileOrder = append(fileOrder, file)
		}
		fileMatches[file] = append(fileMatches[file], grepMatch{lineNum, content})
	}

	sort.Strings(fileOrder)

	totalMatches := 0
	for _, m := range fileMatches {
		totalMatches += len(m)
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%d matches in %dF:\n", totalMatches, len(fileOrder))

	shown := 0
	for _, file := range fileOrder {
		if shown >= grepMaxResults {
			break
		}
		matches := fileMatches[file]
		fmt.Fprintf(&buf, "\n[file] %s (%d):\n", abbreviatePath(file), len(matches))
		perFile := matches
		overflow := 0
		if len(matches) > grepMaxPerFile {
			perFile = matches[:grepMaxPerFile]
			overflow = len(matches) - grepMaxPerFile
		}
		for _, m := range perFile {
			if shown >= grepMaxResults {
				overflow += len(perFile) - (len(perFile) - overflow) // remaining
				break
			}
			fmt.Fprintf(&buf, "  %s: %s\n", m.lineNum, truncateLine(m.content))
			shown++
		}
		if overflow > 0 {
			fmt.Fprintf(&buf, "  +%d more\n", overflow)
		}
	}
	if shown >= grepMaxResults && totalMatches > shown {
		fmt.Fprintf(&buf, "\n[truncated: %d total matches, showing first %d]\n", totalMatches, shown)
	}

	return buf.String()
}

func nativeGrep(pattern string, searchPath string) (string, string) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Sprintf("invalid pattern: %v\n", err)
	}

	absPath, _ := filepath.Abs(searchPath)
	matcher := ignore.NewMatcher(absPath)

	fileOrder := []string{}
	fileMatches := map[string][]grepMatch{}
	var rawBuf bytes.Buffer

	filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && path != absPath && matcher.ShouldIgnore(d.Name(), true) {
			return fs.SkipDir
		}
		if d.IsDir() || matcher.ShouldIgnore(d.Name(), false) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Skip binary files
		checkLen := 512
		if len(data) < checkLen {
			checkLen = len(data)
		}
		for _, b := range data[:checkLen] {
			if b == 0 {
				return nil
			}
		}

		rel, _ := filepath.Rel(absPath, path)
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				rawBuf.WriteString(fmt.Sprintf("%s:%d:%s\n", rel, i+1, line))
				if _, seen := fileMatches[rel]; !seen {
					fileOrder = append(fileOrder, rel)
				}
				fileMatches[rel] = append(fileMatches[rel], grepMatch{fmt.Sprintf("%d", i+1), line})
			}
		}
		return nil
	})

	if len(fileOrder) == 0 {
		return rawBuf.String(), "(no matches)\n"
	}

	totalMatches := 0
	for _, m := range fileMatches {
		totalMatches += len(m)
	}

	sort.Strings(fileOrder)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%d matches in %dF:\n", totalMatches, len(fileOrder))

	shown := 0
	for _, file := range fileOrder {
		if shown >= grepMaxResults {
			break
		}
		matches := fileMatches[file]
		fmt.Fprintf(&buf, "\n[file] %s (%d):\n", abbreviatePath(file), len(matches))
		perFile := matches
		overflow := 0
		if len(matches) > grepMaxPerFile {
			perFile = matches[:grepMaxPerFile]
			overflow = len(matches) - grepMaxPerFile
		}
		for _, m := range perFile {
			if shown >= grepMaxResults {
				break
			}
			fmt.Fprintf(&buf, "  %s: %s\n", m.lineNum, truncateLine(m.content))
			shown++
		}
		if overflow > 0 {
			fmt.Fprintf(&buf, "  +%d more\n", overflow)
		}
	}
	if shown >= grepMaxResults && totalMatches > shown {
		fmt.Fprintf(&buf, "\n[truncated: %d total matches, showing first %d]\n", totalMatches, shown)
	}

	return rawBuf.String(), buf.String()
}
