package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var tscCmd = &cobra.Command{
	Use:                "tsc [flags...]",
	Short:              "TypeScript compiler errors grouped by file",
	Args:               cobra.ArbitraryArgs,
	RunE:               runTSC,
	DisableFlagParsing: true, // the wrapped tool owns its flags, not cobra
}

func init() {
	rootCmd.AddCommand(tscCmd)
}

func runTSC(cmd *cobra.Command, args []string) error {
	start := time.Now()
	args = stripYeetFlags(args)

	tscBin := "tsc"
	if !yeetexec.Available("tsc") {
		if yeetexec.Available("npx") {
			tscBin = "" // signal to use npx
		} else {
			return fmt.Errorf("tsc not found; install TypeScript or ensure npx is available")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var result yeetexec.Result
	if tscBin == "" {
		allArgs := append([]string{"tsc"}, args...)
		result = yeetexec.Run(ctx, "npx", allArgs...)
	} else {
		result = yeetexec.Run(ctx, "tsc", args...)
	}

	raw := result.Stdout + result.Stderr
	rendered := filterTSCOutput(raw)
	if rawOutput {
		rendered = raw
	}
	// A build with hundreds of errors can group into something longer than the
	// compiler's own output. printBetterN keeps the shorter of the two, so the
	// wrapper can never cost more than the command it replaced.
	printed, _ := printBetterN(raw, rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "tsc",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(raw),
			CharsRendered: len(rendered),
			CharsPrinted:  printed,
			BaselineCmd:   "tsc " + strings.Join(args, " "),
			YeetCmd:       "yeet tsc " + strings.Join(args, " "),
			BaselineKind:  analytics.BaselineAsInvoked,
			ExitCode:      result.ExitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

// A failing build is fixed a few errors at a time, and the same type error
// usually repeats down a file. Showing every one of several hundred costs more
// than it tells anyone — the counts say how much is left.
const (
	tscMaxPerFile = 4
	tscMaxFiles   = 25
)

// tscErrorRE matches: path/to/file.ts(line,col): error TS1234: message
var tscErrorRE = regexp.MustCompile(`^(.+\.tsx?)\((\d+),(\d+)\):\s+(error|warning)\s+(TS\d+):\s+(.+)$`)

func filterTSCOutput(output string) string {
	type tscError struct {
		file, line, col, kind, code, msg string
	}

	fileErrors := make(map[string][]tscError)
	var fileOrder []string

	for _, line := range strings.Split(output, "\n") {
		m := tscErrorRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		e := tscError{file: m[1], line: m[2], col: m[3], kind: m[4], code: m[5], msg: m[6]}
		if _, seen := fileErrors[e.file]; !seen {
			fileOrder = append(fileOrder, e.file)
		}
		fileErrors[e.file] = append(fileErrors[e.file], e)
	}

	if len(fileOrder) == 0 {
		// Check for "Found 0 errors" style success
		if strings.Contains(output, "Found 0 errors") {
			return "tsc: no errors\n"
		}
		return output
	}

	sort.Strings(fileOrder)

	totalErrors := 0
	for _, file := range fileOrder {
		totalErrors += len(fileErrors[file])
	}

	var buf strings.Builder
	shownFiles := fileOrder
	if len(shownFiles) > tscMaxFiles {
		shownFiles = shownFiles[:tscMaxFiles]
	}
	for _, file := range shownFiles {
		errs := fileErrors[file]
		buf.WriteString(fmt.Sprintf("\n%s (%d error(s)):\n", file, len(errs)))
		shown := errs
		if len(shown) > tscMaxPerFile {
			shown = shown[:tscMaxPerFile]
		}
		for _, e := range shown {
			buf.WriteString(fmt.Sprintf("  %s:%s  %s %s: %s\n", e.line, e.col, e.kind, e.code, e.msg))
		}
		if n := len(errs) - len(shown); n > 0 {
			buf.WriteString(fmt.Sprintf("  ... %d more in this file\n", n))
		}
	}
	if n := len(fileOrder) - len(shownFiles); n > 0 {
		buf.WriteString(fmt.Sprintf("\n... %d more file(s) with errors (--raw for all)\n", n))
	}

	buf.WriteString(fmt.Sprintf("\nTotal: %d error(s) in %d file(s)\n", totalErrors, len(fileOrder)))
	return buf.String()
}
