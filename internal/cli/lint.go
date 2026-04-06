package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var lintCmd = &cobra.Command{
	Use:   "lint [args...]",
	Short: "ESLint/Biome linter output grouped by rule",
	Args:  cobra.ArbitraryArgs,
	RunE:  runLint,
}

func init() {
	rootCmd.AddCommand(lintCmd)
}

type eslintMessage struct {
	RuleID   *string `json:"ruleId"`
	Severity int     `json:"severity"`
	Message  string  `json:"message"`
	Line     int     `json:"line"`
	Column   int     `json:"column"`
}

type eslintResult struct {
	FilePath     string          `json:"filePath"`
	Messages     []eslintMessage `json:"messages"`
	ErrorCount   int             `json:"errorCount"`
	WarningCount int             `json:"warningCount"`
}

func runLint(cmd *cobra.Command, args []string) error {
	start := time.Now()

	// Detect linter
	linter := detectLinter()
	if linter == "" {
		return fmt.Errorf("no linter found (tried: eslint, biome). Install one and try again.")
	}

	lintArgs := append([]string{"--format", "json"}, args...)
	if linter == "biome" {
		lintArgs = append([]string{"check", "--reporter=json"}, args...)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result := yeetexec.Run(ctx, linter, lintArgs...)
	raw := result.Stdout + result.Stderr
	rendered := filterLintOutput(raw, result.ExitCode)
	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "lint",
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

func detectLinter() string {
	for _, l := range []string{"eslint", "biome"} {
		if yeetexec.Available(l) {
			return l
		}
	}
	return ""
}

func filterLintOutput(raw string, exitCode int) string {
	// Try ESLint JSON
	var results []eslintResult
	if err := json.Unmarshal([]byte(raw), &results); err == nil {
		return formatESLintResults(results, exitCode)
	}

	// Fallback: plain text grouping
	return formatLintPlain(raw, exitCode)
}

func formatESLintResults(results []eslintResult, exitCode int) string {
	type ruleViolation struct {
		file string
		line int
		msg  string
	}

	byRule := make(map[string][]ruleViolation)
	var ruleOrder []string

	totalErrors, totalWarnings := 0, 0
	for _, r := range results {
		totalErrors += r.ErrorCount
		totalWarnings += r.WarningCount
		for _, m := range r.Messages {
			ruleID := "<unknown>"
			if m.RuleID != nil {
				ruleID = *m.RuleID
			}
			v := ruleViolation{file: r.FilePath, line: m.Line, msg: m.Message}
			if _, seen := byRule[ruleID]; !seen {
				ruleOrder = append(ruleOrder, ruleID)
			}
			byRule[ruleID] = append(byRule[ruleID], v)
		}
	}

	if len(ruleOrder) == 0 {
		return "lint: no violations\n"
	}

	sort.Slice(ruleOrder, func(i, j int) bool {
		return len(byRule[ruleOrder[j]]) < len(byRule[ruleOrder[i]])
	})

	var buf strings.Builder
	for _, rule := range ruleOrder {
		violations := byRule[rule]
		buf.WriteString(fmt.Sprintf("\n[%s] (%d violations)\n", rule, len(violations)))
		for _, v := range violations {
			buf.WriteString(fmt.Sprintf("  %s:%d  %s\n", v.file, v.line, v.msg))
		}
	}
	buf.WriteString(fmt.Sprintf("\nTotal: %d errors, %d warnings\n", totalErrors, totalWarnings))
	return buf.String()
}

func formatLintPlain(output string, exitCode int) string {
	if exitCode == 0 {
		return "lint: no violations\n"
	}
	return output
}
