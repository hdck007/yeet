package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log [file]",
	Short: "Deduplicate and summarize log output",
	Long:  "Strips timestamps/UUIDs to normalize log lines, then deduplicates and shows counts. Reads from stdin if no file given.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runLog,
}

func init() {
	rootCmd.AddCommand(logCmd)
}

var (
	logTimestampRE = regexp.MustCompile(`^\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}[.,]?\d*\s*`)
	logUUIDRE      = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	logHexRE       = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	logNumRE       = regexp.MustCompile(`\b\d{4,}\b`)
)

func runLog(cmd *cobra.Command, args []string) error {
	start := time.Now()

	var content string
	if len(args) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
		content = string(data)
	} else {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", args[0], err)
		}
		content = string(data)
	}

	rendered := analyzeLogContent(content)
	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		name := "stdin"
		if len(args) > 0 {
			name = args[0]
		}
		if err := db.RecordUsage(analytics.Usage{
			Command:       "log",
			ArgsSummary:   name,
			CharsRaw:      len(content),
			CharsRendered: len(rendered),
			ExitCode:      0,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

func normalizeLogLine(line string) string {
	// Strip leading timestamp
	line = logTimestampRE.ReplaceAllString(line, "")
	// Normalize UUIDs
	line = logUUIDRE.ReplaceAllString(line, "<uuid>")
	// Normalize hex addresses
	line = logHexRE.ReplaceAllString(line, "<hex>")
	// Normalize long numbers (IDs, timestamps)
	line = logNumRE.ReplaceAllString(line, "<N>")
	return strings.TrimSpace(line)
}

func analyzeLogContent(content string) string {
	counts := make(map[string]int)
	var order []string

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		raw := scanner.Text()
		if strings.TrimSpace(raw) == "" {
			continue
		}
		key := normalizeLogLine(raw)
		if counts[key] == 0 {
			order = append(order, key)
		}
		counts[key]++
	}

	// Sort by count descending, then alphabetical for ties
	sort.Slice(order, func(i, j int) bool {
		ci, cj := counts[order[i]], counts[order[j]]
		if ci != cj {
			return ci > cj
		}
		return order[i] < order[j]
	})

	var buf strings.Builder
	totalLines := 0
	uniqueLines := len(order)
	for _, key := range order {
		totalLines += counts[key]
	}

	buf.WriteString(fmt.Sprintf("[log] %d lines → %d unique patterns\n\n", totalLines, uniqueLines))
	for _, key := range order {
		c := counts[key]
		if c > 1 {
			buf.WriteString(fmt.Sprintf("  x%-4d %s\n", c, key))
		} else {
			buf.WriteString(fmt.Sprintf("       %s\n", key))
		}
	}
	return buf.String()
}
