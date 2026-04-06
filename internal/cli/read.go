package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/hdck007/yeet/internal/filter"
	"github.com/spf13/cobra"
)

var (
	readLevel   string
	readMax     int
	readTail    int
	readLineNum bool
	readVerbose int
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
	rootCmd.AddCommand(readCmd)
}

func runRead(cmd *cobra.Command, args []string) error {
	level := filter.ParseFilterLevel(readLevel)

	if len(args) == 0 || args[0] == "-" {
		return runStdin(level, readMax, readTail, readLineNum, readVerbose)
	}

	return runFile(args[0], level, readMax, readTail, readLineNum, readVerbose)
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
	lang := filter.DetectLanguage(filename)

	if verbose > 1 {
		fmt.Fprintf(os.Stderr, "Detected language: %s\n", lang)
	}

	// Apply filter
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
	if lineNumbers && level != filter.FilterAggressive {
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

	// Apply filter
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
	if lineNumbers && level != filter.FilterAggressive {
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
	lines := strings.Split(content, "\n")
	var buf bytes.Buffer
	width := len(fmt.Sprintf("%d", len(lines)))
	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			break
		}
		fmt.Fprintf(&buf, "%*d │ %s\n", width, i+1, line)
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