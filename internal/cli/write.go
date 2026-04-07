package cli

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/spf13/cobra"
)

var writeb64 string

var writeCmd = &cobra.Command{
	Use:   "write <file>",
	Short: "Write to file with compact confirmation",
	Long: `Write content to a file and return a token-optimized confirmation.

Content source:
  stdin   cat > /tmp/f << 'EOF' ... EOF && yeet write <file> < /tmp/f

  --b64   Base64-encoded content (legacy, not recommended)`,
	Args: cobra.ExactArgs(1),
	RunE: runWrite,
}

func init() {
	writeCmd.Flags().StringVar(&writeb64, "b64", "", "Base64-encoded file content (avoids shell quoting issues)")
	rootCmd.AddCommand(writeCmd)
}

func runWrite(cmd *cobra.Command, args []string) error {
	return runWithFallback("write", args, func() error {
		return runWriteImpl(args)
	}, NoFallback)
}

func runWriteImpl(args []string) error {
	start := time.Now()
	filename := args[0]

	var data []byte
	var err error

	if writeb64 != "" {
		data, err = base64.StdEncoding.DecodeString(writeb64)
		if err != nil {
			// Try URL-safe variant
			data, err = base64.URLEncoding.DecodeString(writeb64)
			if err != nil {
				return fmt.Errorf("decode --b64: %w", err)
			}
		}
	} else {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
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
