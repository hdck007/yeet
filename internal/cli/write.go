package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/spf13/cobra"
)

var writeCmd = &cobra.Command{
	Use:   "write <file>",
	Short: "Write stdin to file with compact confirmation",
	Long:  "Reads from stdin, writes to file, returns a token-optimized confirmation instead of echoing the full content.",
	Args:  cobra.ExactArgs(1),
	RunE:  runWrite,
}

func init() {
	rootCmd.AddCommand(writeCmd)
}

func runWrite(cmd *cobra.Command, args []string) error {
	start := time.Now()
	filename := args[0]

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(filename)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}

	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	content := string(data)
	lines := strings.Count(content, "\n")
	if len(content) > 0 && content[len(content)-1] != '\n' {
		lines++
	}

	// Compact confirmation — this is all that goes to stdout
	rendered := fmt.Sprintf("ok %s (%d lines, %s)\n", filename, lines, formatFileSize(int64(len(data))))
	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		// Raw would be echoing the full file content back
		if err := db.RecordUsage(analytics.Usage{
			Command:       "write",
			ArgsSummary:   filename,
			CharsRaw:      len(data),
			CharsRendered: len(rendered),
			ExitCode:      0,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}

	return nil
}

func formatFileSize(bytes int64) string {
	switch {
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
