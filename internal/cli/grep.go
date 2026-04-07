package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var (
	grepMaxPerFile  = 25
	grepMaxResults  = 200
	grepLineMaxLen  = 80
	grepContextOnly bool
	grepFileType    string
	grepVerbose     int
)

var grepCmd = &cobra.Command{
	Use:   "grep <pattern> [path] [extra_args...]",
	Short: "Compact grouped search results",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runGrep,
}

func init() {
	grepCmd.Flags().BoolVar(&grepContextOnly, "context", false, "Extract context only")
	grepCmd.Flags().StringVar(&grepFileType, "type", "", "File type (rg only)")
	grepCmd.Flags().IntVarP(&grepVerbose, "verbose", "v", 0, "Verbosity level")
	grepCmd.Flags().IntVar(&grepMaxResults, "max-results", 200, "Max overall results shown")
	grepCmd.Flags().IntVar(&grepLineMaxLen, "max-line-len", 80, "Max chars per line")
	grepCmd.Flags().IntVar(&grepMaxPerFile, "max-per-file", 25, "Max matches shown per file")
	rootCmd.AddCommand(grepCmd)
}

type grepMatch struct {
	lineNum string
	content string
}

func runGrep(cmd *cobra.Command, args []string) error {
	start := time.Now()

	pattern := args[0]
	searchPath := "."
	var extraArgs []string

	if len(args) > 1 {
		searchPath = args[1]
	}
	if len(args) > 2 {
		extraArgs = args[2:]
	}

	if grepVerbose > 0 {
		fmt.Fprintf(os.Stderr, "grep: '%s' in %s\n", pattern, searchPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fix: convert BRE alternation \| → | for rg (which uses PCRE-style regex)
	rgPattern := strings.ReplaceAll(pattern, `\|`, "|")

	var result yeetexec.Result

	if yeetexec.Available("rg") {
		rgArgs := []string{"-n", "--no-heading", "--color=never"}
		if grepFileType != "" {
			rgArgs = append(rgArgs, "--type", grepFileType)
		}

		for _, arg := range extraArgs {
			// Fix: skip grep-ism -r flag (rg is recursive by default)
			if arg == "-r" || arg == "--recursive" {
				continue
			}
			rgArgs = append(rgArgs, arg)
		}
		rgArgs = append(rgArgs, rgPattern, searchPath)

		result = yeetexec.Run(ctx, "rg", rgArgs...)
	} else {
		grepArgs := []string{"-rn", "--color=never"}
		grepArgs = append(grepArgs, extraArgs...)
		grepArgs = append(grepArgs, pattern, searchPath)
		result = yeetexec.Run(ctx, "grep", grepArgs...)
	}

	rawOutput := result.Stdout

	if strings.TrimSpace(rawOutput) == "" {
		if result.ExitCode == 2 && strings.TrimSpace(result.Stderr) != "" {
			fmt.Fprintln(os.Stderr, strings.TrimSpace(result.Stderr))
		}
		msg := fmt.Sprintf("0 matches for '%s'\n", pattern)
		fmt.Print(msg)
		trackAnalytics(start, args, rawOutput, msg, result.ExitCode)
		return nil
	}

	var contextRe *regexp.Regexp
	if grepContextOnly {
		// Compile context regex once
		reStr := fmt.Sprintf(`(?i).{0,20}%s.*`, regexp.QuoteMeta(pattern))
		contextRe, _ = regexp.Compile(reStr)
	}

	fileMatches := make(map[string][]grepMatch)
	total := 0

	scanner := bufio.NewScanner(strings.NewReader(rawOutput))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)

		var file, lineNum, content string
		if len(parts) == 3 {
			file, lineNum, content = parts[0], parts[1], parts[2]
		} else if len(parts) == 2 {
			file, lineNum, content = searchPath, parts[0], parts[1]
		} else {
			continue
		}

		total++
		cleaned := cleanLine(content, grepLineMaxLen, contextRe, pattern)
		fileMatches[file] = append(fileMatches[file], grepMatch{lineNum, cleaned})
	}

	var fileOrder []string
	for f := range fileMatches {
		fileOrder = append(fileOrder, f)
	}
	sort.Strings(fileOrder)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%d matches in %dF:\n\n", total, len(fileOrder))

	shown := 0
	for _, file := range fileOrder {
		if shown >= grepMaxResults {
			break
		}

		matches := fileMatches[file]
		fileDisplay := compactPath(file)
		fmt.Fprintf(&buf, "[file] %s (%d):\n", fileDisplay, len(matches))

		perFile := matches
		if len(matches) > grepMaxPerFile {
			perFile = matches[:grepMaxPerFile]
		}

		for _, m := range perFile {
			if shown >= grepMaxResults {
				break
			}
			fmt.Fprintf(&buf, "  %4s: %s\n", m.lineNum, m.content)
			shown++
		}

		if len(matches) > grepMaxPerFile {
			fmt.Fprintf(&buf, "  +%d\n", len(matches)-grepMaxPerFile)
		}
		buf.WriteString("\n")
	}

	if total > shown {
		fmt.Fprintf(&buf, "... +%d\n", total-shown)
	}

	rendered := buf.String()
	fmt.Print(rendered)

	trackAnalytics(start, args, rawOutput, rendered, result.ExitCode)
	return nil
}

func cleanLine(line string, maxLen int, contextRe *regexp.Regexp, pattern string) string {
	trimmed := strings.TrimSpace(line)

	if contextRe != nil {
		if loc := contextRe.FindStringIndex(trimmed); loc != nil {
			matched := trimmed[loc[0]:loc[1]]
			if utf8.RuneCountInString(matched) <= maxLen {
				return matched
			}
		}
	}

	runes := []rune(trimmed)
	charLen := len(runes)

	if charLen <= maxLen {
		return trimmed
	}

	lower := strings.ToLower(trimmed)
	patternLower := strings.ToLower(pattern)

	if idx := strings.Index(lower, patternLower); idx != -1 {
		// Calculate character position safely for multibyte strings
		charPos := utf8.RuneCountInString(lower[:idx])

		start := charPos - (maxLen / 3)
		if start < 0 {
			start = 0
		}

		end := start + maxLen
		if end > charLen {
			end = charLen
			start = end - maxLen
			if start < 0 {
				start = 0
			}
		}

		slice := string(runes[start:end])
		if start > 0 && end < charLen {
			return fmt.Sprintf("...%s...", slice)
		} else if start > 0 {
			return fmt.Sprintf("...%s", slice)
		}
		return fmt.Sprintf("%s...", slice)
	}

	// Fallback if pattern wasn't found (e.g., regex differences)
	if maxLen > 3 {
		return fmt.Sprintf("%s...", string(runes[:maxLen-3]))
	}
	return string(runes[:maxLen])
}

func compactPath(path string) string {
	if len(path) <= 50 {
		return path
	}

	parts := strings.Split(path, "/")
	if len(parts) <= 3 {
		return path
	}

	return fmt.Sprintf("%s/.../%s/%s", parts[0], parts[len(parts)-2], parts[len(parts)-1])
}

func trackAnalytics(start time.Time, args []string, rawOutput, rendered string, exitCode int) {
	// Assuming these variables are defined globally elsewhere in your package,
	// just like the original Go snippet you provided.
	improved := len(rendered) < len(rawOutput)
	if improved && !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "grep",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(rawOutput),
			CharsRendered: len(rendered),
			ExitCode:      exitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
}