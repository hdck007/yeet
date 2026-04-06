package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var vitestCmd = &cobra.Command{
	Use:   "vitest [args...]",
	Short: "Vitest test output — failures only",
	Args:  cobra.ArbitraryArgs,
	RunE:  runVitest,
}

func init() {
	rootCmd.AddCommand(vitestCmd)
}

type vitestTestFile struct {
	Name             string         `json:"name"`
	AssertionResults []vitestResult `json:"assertionResults"`
}

type vitestResult struct {
	Status      string   `json:"status"`
	FullName    string   `json:"fullName"`
	FailureMessages []string `json:"failureMessages"`
}

type vitestJSON struct {
	TestResults      []vitestTestFile `json:"testResults"`
	NumTotalTests    int              `json:"numTotalTests"`
	NumPassedTests   int              `json:"numPassedTests"`
	NumFailedTests   int              `json:"numFailedTests"`
	NumPendingTests  int              `json:"numPendingTests"`
}

func runVitest(cmd *cobra.Command, args []string) error {
	start := time.Now()

	runner := detectJSRunner("vitest")
	jsonArgs := append([]string{"run", "--reporter=json"}, args...)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	var result yeetexec.Result
	if runner == "npx" {
		result = yeetexec.Run(ctx, "npx", append([]string{"vitest"}, jsonArgs...)...)
	} else {
		result = yeetexec.Run(ctx, runner, jsonArgs...)
	}

	raw := result.Stdout
	rendered := filterVitestOutput(raw, result.Stderr, result.ExitCode)
	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "vitest",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(raw),
			CharsRendered: len(rendered),
			ExitCode:      result.ExitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

func filterVitestOutput(stdout, stderr string, exitCode int) string {
	// Try to parse JSON from stdout
	jsonStr := extractJSONObject(stdout)
	if jsonStr != "" {
		var out vitestJSON
		if err := json.Unmarshal([]byte(jsonStr), &out); err == nil {
			return formatVitestJSON(&out)
		}
	}

	// Fallback: plain text
	if exitCode == 0 {
		return formatTestSummaryPlain(stdout, true)
	}
	return formatTestSummaryPlain(stdout+"\n"+stderr, false)
}

func formatVitestJSON(out *vitestJSON) string {
	if out.NumFailedTests == 0 {
		return fmt.Sprintf("vitest: all %d tests passed\n", out.NumTotalTests)
	}

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("[FAIL] %d/%d tests failed\n\n", out.NumFailedTests, out.NumTotalTests))

	for _, file := range out.TestResults {
		hasFailures := false
		for _, r := range file.AssertionResults {
			if r.Status == "failed" {
				hasFailures = true
				break
			}
		}
		if !hasFailures {
			continue
		}
		buf.WriteString(fmt.Sprintf("  %s\n", file.Name))
		for _, r := range file.AssertionResults {
			if r.Status != "failed" {
				continue
			}
			buf.WriteString(fmt.Sprintf("    FAIL: %s\n", r.FullName))
			for _, msg := range r.FailureMessages {
				// Trim long stack traces
				lines := strings.Split(msg, "\n")
				for i, l := range lines {
					if i >= 5 {
						buf.WriteString(fmt.Sprintf("         ... (%d more lines)\n", len(lines)-5))
						break
					}
					buf.WriteString("         " + strings.TrimSpace(l) + "\n")
				}
			}
		}
	}

	if out.NumPendingTests > 0 {
		buf.WriteString(fmt.Sprintf("\n(%d skipped)\n", out.NumPendingTests))
	}
	return buf.String()
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func formatTestSummaryPlain(output string, success bool) string {
	clean := ansiRE.ReplaceAllString(output, "")
	if success {
		// Show only summary lines
		var summary []string
		for _, line := range strings.Split(clean, "\n") {
			if strings.Contains(line, "pass") || strings.Contains(line, "fail") ||
				strings.Contains(line, "skip") || strings.Contains(line, "Tests ") {
				summary = append(summary, line)
			}
		}
		if len(summary) > 0 {
			return strings.Join(summary, "\n") + "\n"
		}
	}
	return clean
}

// extractJSONObject finds the first {...} JSON object in a string
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
