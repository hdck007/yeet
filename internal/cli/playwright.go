package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var playwrightCmd = &cobra.Command{
	Use:   "playwright [args...]",
	Short: "Playwright E2E test output — failures only",
	Args:  cobra.ArbitraryArgs,
	RunE:  runPlaywright,
}

func init() {
	rootCmd.AddCommand(playwrightCmd)
}

type pwStats struct {
	Expected   int     `json:"expected"`
	Unexpected int     `json:"unexpected"`
	Skipped    int     `json:"skipped"`
	Duration   float64 `json:"duration"`
}

type pwSpec struct {
	Title string    `json:"title"`
	Tests []pwTest  `json:"tests"`
}

type pwTest struct {
	Title   string     `json:"title"`
	Results []pwResult `json:"results"`
}

type pwResult struct {
	Status string    `json:"status"`
	Errors []pwError `json:"errors"`
}

type pwError struct {
	Message string `json:"message"`
}

type pwSuite struct {
	Title  string    `json:"title"`
	File   *string   `json:"file"`
	Specs  []pwSpec  `json:"specs"`
	Suites []pwSuite `json:"suites"`
}

type pwJSON struct {
	Stats  pwStats   `json:"stats"`
	Suites []pwSuite `json:"suites"`
}

func runPlaywright(cmd *cobra.Command, args []string) error {
	start := time.Now()

	pw := "playwright"
	if !yeetexec.Available("playwright") {
		pw = ""
	}

	reporterArgs := []string{"test", "--reporter=json"}
	reporterArgs = append(reporterArgs, args...)

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	var result yeetexec.Result
	if pw == "" {
		result = yeetexec.Run(ctx, "npx", append([]string{"playwright"}, reporterArgs...)...)
	} else {
		result = yeetexec.Run(ctx, "playwright", reporterArgs...)
	}

	raw := result.Stdout + result.Stderr
	rendered := filterPlaywrightOutput(raw, result.ExitCode)
	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "playwright",
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

func filterPlaywrightOutput(raw string, exitCode int) string {
	jsonStr := extractJSONObject(raw)
	if jsonStr != "" {
		var out pwJSON
		if err := json.Unmarshal([]byte(jsonStr), &out); err == nil {
			return formatPlaywrightJSON(&out)
		}
	}
	// Fallback
	if exitCode == 0 {
		return "playwright: all tests passed\n"
	}
	return ansiRE.ReplaceAllString(raw, "")
}

func formatPlaywrightJSON(out *pwJSON) string {
	if out.Stats.Unexpected == 0 {
		return fmt.Sprintf("playwright: %d tests passed (%.1fs)\n",
			out.Stats.Expected, out.Stats.Duration/1000)
	}

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("[FAIL] %d unexpected failure(s) / %d expected passed\n\n",
		out.Stats.Unexpected, out.Stats.Expected))

	var collectFailures func(suites []pwSuite, prefix string)
	collectFailures = func(suites []pwSuite, prefix string) {
		for _, suite := range suites {
			title := prefix
			if suite.Title != "" {
				if title != "" {
					title += " > "
				}
				title += suite.Title
			}
			for _, spec := range suite.Specs {
				for _, test := range spec.Tests {
					for _, r := range test.Results {
						if r.Status == "unexpected" || r.Status == "failed" {
							buf.WriteString(fmt.Sprintf("  FAIL: %s > %s\n", title, spec.Title))
							for _, e := range r.Errors {
								lines := strings.Split(e.Message, "\n")
								for i, l := range lines {
									if i >= 3 {
										buf.WriteString(fmt.Sprintf("       ... (%d more)\n", len(lines)-3))
										break
									}
									buf.WriteString("       " + strings.TrimSpace(l) + "\n")
								}
							}
						}
					}
				}
			}
			collectFailures(suite.Suites, title)
		}
	}
	collectFailures(out.Suites, "")

	if out.Stats.Skipped > 0 {
		buf.WriteString(fmt.Sprintf("\n(%d skipped)\n", out.Stats.Skipped))
	}
	return buf.String()
}
