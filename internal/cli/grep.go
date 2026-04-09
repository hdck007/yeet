package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var (
	grepMaxPerFile   = 25
	grepMaxResults   = 200
	grepLineMaxLen   = 80
	grepTrimToMatch  bool
	grepFileType     string
	grepVerbose      int
	grepContextLines = 0
)

var grepCmd = &cobra.Command{
	Use:   "grep <pattern> [path] [extra_args...]",
	Short: "Compact grouped search results with context",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runGrep,
}

func init() {
	grepCmd.Flags().BoolVar(&grepTrimToMatch, "trim", false, "Trim each match line to context around pattern")
	grepCmd.Flags().StringVar(&grepFileType, "type", "", "File type (rg only)")
	grepCmd.Flags().IntVarP(&grepVerbose, "verbose", "v", 0, "Verbosity level")
	grepCmd.Flags().IntVar(&grepMaxResults, "max-results", 200, "Max overall results shown")
	grepCmd.Flags().IntVar(&grepLineMaxLen, "max-line-len", 80, "Max chars per line")
	grepCmd.Flags().IntVar(&grepMaxPerFile, "max-per-file", 25, "Max matches shown per file")
	grepCmd.Flags().IntVarP(&grepContextLines, "context", "C", 0, "Lines of context around each match")
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

	rgPattern := strings.ReplaceAll(pattern, `\|`, "|")

	// Use context-aware path when context lines > 0 and rg is available
	if grepContextLines > 0 && yeetexec.Available("rg") {
		rendered, raw, err := runGrepWithContext(ctx, rgPattern, searchPath, extraArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "yeet grep: context mode error: %v\n", err)
		} else {
			fmt.Print(rendered)
			trackAnalytics(start, args, raw, rendered, 0)
			return nil
		}
	}

	// Fallback: match-only (no context)
	var result yeetexec.Result

	if yeetexec.Available("rg") {
		rgArgs := []string{"-n", "--no-heading", "--color=never"}
		if grepFileType != "" {
			rgArgs = append(rgArgs, "--type", grepFileType)
		}
		for _, arg := range extraArgs {
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
		msg := fmt.Sprintf("no matches for '%s'\n", pattern)
		fmt.Print(msg)
		trackAnalytics(start, args, rawOutput, msg, result.ExitCode)
		return nil
	}

	var contextRe *regexp.Regexp
	if grepTrimToMatch {
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

// rgJSONMsg is the subset of rg's --json output that we need.
type rgJSONMsg struct {
	Type string `json:"type"`
	Data struct {
		Path  struct{ Text string `json:"text"` } `json:"path"`
		Lines struct{ Text string `json:"text"` } `json:"lines"`
		LineNumber int `json:"line_number"`
	} `json:"data"`
}

type ctxLine struct {
	num     int
	text    string
	isMatch bool
}

type fileBlock struct {
	path    string
	lines   []ctxLine
	matches int
}

func runGrepWithContext(ctx context.Context, pattern, searchPath string, extraArgs []string) (rendered, raw string, err error) {
	rgArgs := []string{"--json", "-n", "--color=never",
		"--context", strconv.Itoa(grepContextLines)}
	if grepFileType != "" {
		rgArgs = append(rgArgs, "--type", grepFileType)
	}
	for _, arg := range extraArgs {
		if arg == "-r" || arg == "--recursive" {
			continue
		}
		rgArgs = append(rgArgs, arg)
	}
	rgArgs = append(rgArgs, pattern, searchPath)

	result := yeetexec.Run(ctx, "rg", rgArgs...)
	raw = result.Stdout

	if strings.TrimSpace(raw) == "" {
		return fmt.Sprintf("no matches for '%s'\n", pattern), raw, nil
	}

	// Parse JSON lines into per-file blocks
	var blocks []fileBlock
	fileIndex := map[string]int{}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		var msg rgJSONMsg
		if err2 := json.Unmarshal([]byte(scanner.Text()), &msg); err2 != nil {
			continue
		}
		if msg.Type != "match" && msg.Type != "context" {
			continue
		}

		file := msg.Data.Path.Text
		content := strings.TrimRight(msg.Data.Lines.Text, "\n\r")
		lineNum := msg.Data.LineNumber
		isMatch := msg.Type == "match"

		idx, exists := fileIndex[file]
		if !exists {
			blocks = append(blocks, fileBlock{path: file})
			idx = len(blocks) - 1
			fileIndex[file] = idx
		}
		if isMatch {
			blocks[idx].matches++
		}
		blocks[idx].lines = append(blocks[idx].lines, ctxLine{num: lineNum, text: content, isMatch: isMatch})
	}

	// Render
	totalMatches := 0
	for _, b := range blocks {
		totalMatches += b.matches
	}
	if totalMatches == 0 {
		return fmt.Sprintf("no matches for '%s'\n", pattern), raw, nil
	}

	var buf bytes.Buffer
	plural := "es"
	if totalMatches == 1 {
		plural = ""
	}
	fmt.Fprintf(&buf, "%d match%s in %dF:\n\n", totalMatches, plural, len(blocks))

	shown := 0
	for _, block := range blocks {
		if shown >= grepMaxResults {
			break
		}

		// Calculate line number display width for this block
		maxNum := 0
		for _, l := range block.lines {
			if l.num > maxNum {
				maxNum = l.num
			}
		}
		width := len(strconv.Itoa(maxNum))
		if width < 4 {
			width = 4
		}

		fmt.Fprintf(&buf, "%s (%d):\n", compactPath(block.path), block.matches)

		matchesShown := 0
		prevNum := -1
		for _, l := range block.lines {
			// Insert ··· separator when there's a gap in line numbers
			if prevNum >= 0 && l.num > prevNum+1 {
				fmt.Fprintf(&buf, "  ···\n")
			}
			prevNum = l.num

			content := cleanLine(l.text, grepLineMaxLen, nil, pattern)
			if l.isMatch {
				if matchesShown >= grepMaxPerFile {
					break
				}
				fmt.Fprintf(&buf, "► %*d: %s\n", width, l.num, content)
				matchesShown++
				shown++
			} else {
				fmt.Fprintf(&buf, "  %*d  %s\n", width, l.num, content)
			}
		}
		if block.matches > grepMaxPerFile {
			fmt.Fprintf(&buf, "  +%d more\n", block.matches-grepMaxPerFile)
		}
		buf.WriteString("\n")
	}

	if totalMatches > shown {
		fmt.Fprintf(&buf, "... +%d\n", totalMatches-shown)
	}

	return buf.String(), raw, nil
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
	if !noAnalytics && db != nil {
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
