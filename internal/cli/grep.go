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

var grepCmd = &cobra.Command{
	Use:   "grep <pattern> [path]",
	Short: "Compact search results — file:line only",
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

	fmt.Print(rendered)

	if !noAnalytics && db != nil {
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

func formatGrepCompact(raw string) string {
	if raw == "" {
		return "(no matches)\n"
	}

	var buf bytes.Buffer
	totalMatches := 0
	fileSet := map[string]bool{}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		file := parts[0]
		lineNum := parts[1]
		fileSet[file] = true
		totalMatches++
		fmt.Fprintf(&buf, "%s:%s\n", file, lineNum)
	}

	fmt.Fprintf(&buf, "(%d matches in %d files)\n", totalMatches, len(fileSet))
	return buf.String()
}

func nativeGrep(pattern string, searchPath string) (string, string) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Sprintf("invalid pattern: %v\n", err)
	}

	absPath, _ := filepath.Abs(searchPath)
	matcher := ignore.NewMatcher(absPath)

	var rawBuf, buf bytes.Buffer
	totalMatches := 0
	fileSet := map[string]bool{}

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
				fmt.Fprintf(&buf, "%s:%d\n", rel, i+1)
				fileSet[rel] = true
				totalMatches++
			}
		}
		return nil
	})

	sort.Strings(nil) // keep deterministic
	if totalMatches == 0 {
		return rawBuf.String(), "(no matches)\n"
	}
	fmt.Fprintf(&buf, "(%d matches in %d files)\n", totalMatches, len(fileSet))
	return rawBuf.String(), buf.String()
}
