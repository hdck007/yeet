package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/hdck007/yeet/internal/filter"
	"github.com/spf13/cobra"
)

var (
	readLevel     string
	readMax       int
	readTail      int
	readLineNum   bool
	readVerbose   int
	readLines     string
	readThreshold int
)

var readCmd = &cobra.Command{
	Use:   "read [file]",
	Short: "Smart file reading with language-aware filtering",
	Long: `Reads source files with optional language-aware filtering to strip boilerplate.

Filter levels:
  minimal (default)  — full content, no filtering
  moderate           — strip comments and collapse blank lines
  aggressive         — signatures only (func, type, struct, class, etc.)

Use -n to add line numbers (off by default).

Use "-" or omit file to read from stdin.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRead,
}

func init() {
	readCmd.Flags().StringVarP(&readLevel, "level", "l", "", "Filter level: minimal, moderate, aggressive")
	readCmd.Flags().IntVarP(&readMax, "max-lines", "m", 0, "Show only first N lines (smart truncation)")
	readCmd.Flags().IntVarP(&readTail, "tail", "t", 0, "Show only last N lines")
	readCmd.Flags().BoolVarP(&readLineNum, "numbers", "n", false, "Show line numbers (default off)")
	readCmd.Flags().CountVarP(&readVerbose, "verbose", "v", "Verbose output (-v, -vv)")
	readCmd.Flags().StringVar(&readLines, "lines", "", "Show only lines N-M (e.g. --lines 308-325), uses original line numbers")
	readCmd.Flags().IntVar(&readThreshold, "threshold", 0, "Big-file warning threshold in lines (0 = YEET_BIG_FILE_THRESHOLD env var or default 150; -1 = disable)")
	rootCmd.AddCommand(readCmd)
}

func runRead(cmd *cobra.Command, args []string) error {
	return runWithFallback("read", args, func() error {
		return runReadImpl(args)
	}, Fallback{
		Bin: "cat",
		Args: func(a []string) []string { return a },
	})
}

func runReadImpl(args []string) error {
	level := filter.ParseFilterLevel(readLevel)

	if len(args) == 0 || args[0] == "-" {
		return runStdin(level, readMax, readTail, readLineNum, readVerbose)
	}

	return runFile(args[0], level, readMax, readTail, readLineNum, readVerbose)
}

func extractLineRange(content, spec string) (string, error) {
	parts := strings.SplitN(spec, "-", 2)
	start, err := strconv.Atoi(parts[0])
	if err != nil || start < 1 {
		return "", fmt.Errorf("invalid --lines value %q: start must be a positive integer", spec)
	}
	end := start
	if len(parts) == 2 {
		end, err = strconv.Atoi(parts[1])
		if err != nil || end < start {
			return "", fmt.Errorf("invalid --lines value %q: end must be >= start", spec)
		}
	}
	lines := strings.Split(content, "\n")
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n") + "\n", nil
}

func getReadThreshold() int {
	if readThreshold != 0 {
		return readThreshold
	}
	if v := os.Getenv("YEET_BIG_FILE_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	if n := PersistedThreshold(); n > 0 {
		return n
	}
	return 150
}

func formatWithOriginalLineNums(nums []int, lines []string) string {
	if len(nums) == 0 {
		return ""
	}
	maxNum := nums[len(nums)-1]
	width := len(fmt.Sprintf("%d", maxNum))
	var buf bytes.Buffer
	for i, line := range lines {
		fmt.Fprintf(&buf, "%*d │ %s\n", width, nums[i], line)
	}
	return buf.String()
}

func runFile(filename string, level filter.FilterLevel, maxLines, tailLines int, lineNumbers bool, verbose int) error {
	start := time.Now()

	if verbose > 0 {
		fmt.Fprintf(os.Stderr, "Reading: %s (filter: %s)\n", filename, level)
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filename, err)
	}

	// Binary detection fallback (retained from original Go for safety)
	if isBinary(data) {
		info, _ := os.Stat(filename)
		size := int64(len(data))
		if info != nil {
			size = info.Size()
		}
		msg := fmt.Sprintf("binary file, %d bytes\n", size)
		fmt.Print(msg)
		recordReadAnalytics(filename, string(data), msg, start)
		return nil
	}

	content := string(data)

	// --lines N-M: extract raw lines before any filtering, preserving original line numbers.
	if readLines != "" {
		extracted, err := extractLineRange(content, readLines)
		if err != nil {
			return err
		}
		if readLineNum {
			startLine, _ := strconv.Atoi(strings.SplitN(readLines, "-", 2)[0])
			fmt.Print(formatWithLineNumbersFrom(extracted, startLine))
		} else {
			fmt.Print(extracted)
		}
		recordReadAnalytics(fmt.Sprintf("cat %s", filename), content, extracted, start)
		return nil
	}

	lang := filter.DetectLanguage(filename)

	if verbose > 1 {
		fmt.Fprintf(os.Stderr, "Detected language: %s\n", lang)
	}

	// Big-file warning when no filter level is specified
	if readLevel == "" {
		threshold := getReadThreshold()
		if threshold > 0 {
			lineCount := strings.Count(content, "\n") + 1
			if lineCount > threshold {
				fmt.Printf("yeet: %s has %d lines (threshold: %d). Use --lines or grep instead of reading the whole file:\n", filename, lineCount, threshold)
				fmt.Printf("  1. Search first:     yeet grep \"<pattern>\" %s\n", filename)
				fmt.Printf("  2. Targeted lines:   yeet read %s --lines N-M\n", filename)
				fmt.Printf("  3. Signatures only:  yeet read %s -l aggressive\n", filename)
				fmt.Printf("  ⚠ LAST RESORT ONLY:  yeet read %s -l minimal  — high token cost, only if all else fails\n", filename)
				return nil
			}
		}
	}

	// Aggressive: always extract signatures with original line numbers
	if level == filter.FilterAggressive {
		nums, sigLines, ok := filter.ExtractSignaturesWithLineNums(content, lang)
		if ok {
			output := formatWithOriginalLineNums(nums, sigLines)
			fmt.Print(output)
			recordReadAnalytics(fmt.Sprintf("cat %s", filename), content, output, start)
			return nil
		}
		// Language not supported for aggressive (e.g. binary/unknown) — show raw
		fmt.Print(content)
		recordReadAnalytics(fmt.Sprintf("cat %s", filename), content, content, start)
		return nil
	}

	// Apply filter (moderate / minimal)
	filtered, applied := filter.FilterContent(content, lang, level)

	// Safety: if filter emptied a non-empty file, fall back to raw content
	if strings.TrimSpace(filtered) == "" && strings.TrimSpace(content) != "" {
		fmt.Fprintf(os.Stderr, "yeet: warning: filter produced empty output for %s (%d bytes), showing raw content\n",
			filename, len(content))
		filtered = content
		applied = false
	}

	if verbose > 0 && applied {
		origLines := strings.Count(content, "\n")
		filtLines := strings.Count(filtered, "\n")
		reduction := 0.0
		if origLines > 0 {
			reduction = float64(origLines-filtLines) / float64(origLines) * 100.0
		}
		fmt.Fprintf(os.Stderr, "Lines: %d -> %d (%.1f%% reduction)\n", origLines, filtLines, reduction)
	}

	// Apply line windowing
	filtered = applyLineWindow(filtered, maxLines, tailLines, lang)

	var output string
	if lineNumbers {
		output = formatWithLineNumbers(filtered)
	} else {
		output = filtered
	}

	fmt.Print(output)
	recordReadAnalytics(fmt.Sprintf("cat %s", filename), content, output, start)
	return nil
}

func runStdin(level filter.FilterLevel, maxLines, tailLines int, lineNumbers bool, verbose int) error {
	start := time.Now()

	if verbose > 0 {
		fmt.Fprintf(os.Stderr, "Reading from stdin (filter: %s)\n", level)
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("failed to read from stdin: %w", err)
	}
	content := string(data)
	lang := filter.LangUnknown

	if verbose > 1 {
		fmt.Fprintf(os.Stderr, "Language: %s (stdin has no extension)\n", lang)
	}

	// Aggressive: always extract signatures with original line numbers
	if level == filter.FilterAggressive {
		nums, sigLines, ok := filter.ExtractSignaturesWithLineNums(content, lang)
		if ok {
			output := formatWithOriginalLineNums(nums, sigLines)
			fmt.Print(output)
			recordReadAnalytics("stdin", content, output, start)
			return nil
		}
		fmt.Print(content)
		recordReadAnalytics("stdin", content, content, start)
		return nil
	}

	// Apply filter (moderate / minimal)
	filtered, applied := filter.FilterContent(content, lang, level)

	if verbose > 0 && applied {
		origLines := strings.Count(content, "\n")
		filtLines := strings.Count(filtered, "\n")
		reduction := 0.0
		if origLines > 0 {
			reduction = float64(origLines-filtLines) / float64(origLines) * 100.0
		}
		fmt.Fprintf(os.Stderr, "Lines: %d -> %d (%.1f%% reduction)\n", origLines, filtLines, reduction)
	}

	filtered = applyLineWindow(filtered, maxLines, tailLines, lang)

	var output string
	if lineNumbers {
		output = formatWithLineNumbers(filtered)
	} else {
		output = filtered
	}

	fmt.Print(output)
	recordReadAnalytics("stdin", content, output, start)
	return nil
}

func isBinary(data []byte) bool {
	checkLen := 512
	if len(data) < checkLen {
		checkLen = len(data)
	}
	for _, b := range data[:checkLen] {
		if b == 0 {
			return true
		}
	}
	return false
}

func formatWithLineNumbers(content string) string {
	return formatWithLineNumbersFrom(content, 1)
}

func formatWithLineNumbersFrom(content string, startLine int) string {
	lines := strings.Split(content, "\n")
	var buf bytes.Buffer
	lastLine := startLine + len(lines) - 1
	width := len(fmt.Sprintf("%d", lastLine))
	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			break
		}
		fmt.Fprintf(&buf, "%*d │ %s\n", width, startLine+i, line)
	}
	return buf.String()
}

func applyLineWindow(content string, maxLines, tailLines int, lang filter.Language) string {
	if tailLines > 0 {
		if tailLines == 0 {
			return ""
		}
		
		lines := strings.Split(content, "\n")
		hasTrailingNewline := strings.HasSuffix(content, "\n")
		
		if hasTrailingNewline {
			// Ignore the empty string resulting from the split after the final newline
			lines = lines[:len(lines)-1]
		}

		start := len(lines) - tailLines
		if start < 0 {
			start = 0
		}

		result := strings.Join(lines[start:], "\n")
		if hasTrailingNewline {
			result += "\n"
		}
		return result
	}

	if maxLines > 0 {
		return filter.SmartTruncate(content, maxLines, lang)
	}

	return content
}

func recordReadAnalytics(command, raw, rendered string, start time.Time) {
	// Assumes globals `noAnalytics` and `db` exist in the cli package scope as in your original
	if noAnalytics || db == nil {
		return
	}
	if err := db.RecordUsage(analytics.Usage{
		Command:       "read",
		ArgsSummary:   command,
		CharsRaw:      len(raw),
		CharsRendered: len(rendered),
		ExitCode:      0,
		DurationMs:    time.Since(start).Milliseconds(),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
	}
}