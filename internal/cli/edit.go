package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/spf13/cobra"
)

var (
	editOld        string
	editNew        string
	editReplaceAll bool
)

var editCmd = &cobra.Command{
	Use:   "edit <file>",
	Short: "Apply string replacement in a file with compact confirmation",
	Long: `Replace text in a file. Returns a compact confirmation instead of echoing full content.

Examples:
  yeet edit file.go --old 'foo' --new 'bar'              # Replace first occurrence
  yeet edit file.go --old 'foo' --new 'bar' --all         # Replace all occurrences
  echo 'old|||new' | yeet edit file.go                    # Pipe mode: old|||new delimiter

  # Heredoc mode (best for multi-line replacements):
  yeet edit file.go << 'EDIT'
  old multi-line
  content here
  |||
  new multi-line
  content here
  EDIT`,
	Args: cobra.ExactArgs(1),
	RunE: runEdit,
}

func init() {
	editCmd.Flags().StringVar(&editOld, "old", "", "Text to find")
	editCmd.Flags().StringVar(&editNew, "new", "", "Text to replace with")
	editCmd.Flags().BoolVar(&editReplaceAll, "all", false, "Replace all occurrences")
	rootCmd.AddCommand(editCmd)
}

func runEdit(cmd *cobra.Command, args []string) error {
	start := time.Now()
	filename := args[0]

	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	original := string(data)

	oldStr := editOld
	newStr := editNew

	// If --old not provided, try reading from stdin
	if oldStr == "" {
		stdinData, err := readStdinNonBlocking()
		if err == nil && len(stdinData) > 0 {
			raw := string(stdinData)
			// Support two formats:
			// 1. Inline:   old|||new
			// 2. Heredoc:  old content\n|||\nnew content  (||| on its own line)
			if idx := strings.Index(raw, "\n|||\n"); idx >= 0 {
				oldStr = raw[:idx]
				newStr = raw[idx+len("\n|||\n"):]
				// Strip trailing newline added by heredoc
				newStr = strings.TrimRight(newStr, "\n")
			} else if parts := strings.SplitN(raw, "|||", 2); len(parts) == 2 {
				oldStr = strings.TrimSpace(parts[0])
				newStr = strings.TrimSpace(parts[1])
			}
		}
		if oldStr == "" {
			return fmt.Errorf("--old flag is required (or pipe via stdin: 'old|||new' or heredoc with ||| on its own line)")
		}
	}

	if !strings.Contains(original, oldStr) {
		return fmt.Errorf("old string not found in %s", filename)
	}

	var modified string
	var count int
	if editReplaceAll {
		count = strings.Count(original, oldStr)
		modified = strings.ReplaceAll(original, oldStr, newStr)
	} else {
		count = 1
		modified = strings.Replace(original, oldStr, newStr, 1)
	}

	if modified == original {
		fmt.Printf("ok %s (no changes)\n", filename)
		return nil
	}

	if err := os.WriteFile(filename, []byte(modified), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	// Find which lines changed
	oldLines := strings.Split(original, "\n")
	newLines := strings.Split(modified, "\n")
	changedLineNums := diffLineNumbers(oldLines, newLines)

	linesStr := "lines"
	if len(changedLineNums) == 1 {
		linesStr = "line"
	}

	rendered := fmt.Sprintf("ok %s (%d replacement%s, %d %s changed: %s)\n",
		filename,
		count,
		pluralS(count),
		len(changedLineNums),
		linesStr,
		formatLineNums(changedLineNums))
	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "edit",
			ArgsSummary:   filename,
			CharsRaw:      len(original),
			CharsRendered: len(rendered),
			ExitCode:      0,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}

	return nil
}

func readStdinNonBlocking() ([]byte, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil, err
	}
	// Only read if there's piped data
	if info.Mode()&os.ModeCharDevice != 0 {
		return nil, fmt.Errorf("no piped input")
	}
	var buf bytes.Buffer
	_, err = buf.ReadFrom(os.Stdin)
	return buf.Bytes(), err
}

func diffLineNumbers(old, new []string) []int {
	var changed []int
	maxLen := len(old)
	if len(new) > maxLen {
		maxLen = len(new)
	}
	for i := 0; i < maxLen; i++ {
		var o, n string
		if i < len(old) {
			o = old[i]
		}
		if i < len(new) {
			n = new[i]
		}
		if o != n {
			changed = append(changed, i+1)
		}
	}
	return changed
}

func formatLineNums(nums []int) string {
	if len(nums) == 0 {
		return "none"
	}
	if len(nums) > 5 {
		strs := make([]string, 5)
		for i := 0; i < 5; i++ {
			strs[i] = fmt.Sprintf("%d", nums[i])
		}
		return strings.Join(strs, ",") + fmt.Sprintf("...+%d more", len(nums)-5)
	}
	strs := make([]string, len(nums))
	for i, n := range nums {
		strs[i] = fmt.Sprintf("%d", n)
	}
	return strings.Join(strs, ",")
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
