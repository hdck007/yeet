package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var duCmd = &cobra.Command{
	Use:                "du [flags] [path...]",
	Short:              "Disk usage, largest first, with the long tail collapsed",
	Args:               cobra.ArbitraryArgs,
	RunE:               runDU,
	DisableFlagParsing: true, // du flags (-sh, -ah) are not cobra flags
}

func init() {
	rootCmd.AddCommand(duCmd)
}

// A recursive du emits a line per directory in source order, which for any
// tree containing node_modules or .git is thousands of lines in an order
// nobody wants. The question is always "what is big", so the answer is the
// large entries, sorted, with the rest accounted for but not listed.
const duTopN = 25

func runDU(cmd *cobra.Command, args []string) error {
	start := time.Now()
	args = stripYeetFlags(args)

	if !yeetexec.Available("du") {
		return fmt.Errorf("du not found in PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result := yeetexec.Run(ctx, "du", args...)
	// du reports unreadable directories on stderr, one line each. Those are
	// noise for the question being asked, but a run that produced nothing at
	// all still needs to say why.
	raw := result.Stdout
	if strings.TrimSpace(raw) == "" {
		raw = result.Stdout + result.Stderr
	}

	rendered := renderDU(raw)
	if rawOutput {
		rendered = raw
	}
	printed, _ := printBetterN(raw, rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "du",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(raw),
			CharsRendered: len(rendered),
			CharsPrinted:  printed,
			BaselineCmd:   "du " + strings.Join(args, " "),
			YeetCmd:       "yeet du " + strings.Join(args, " "),
			BaselineKind:  analytics.BaselineAsInvoked,
			ExitCode:      result.ExitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

type duEntry struct {
	// size is the value as du printed it, so the output never restates a size
	// in a unit du was not using.
	size string
	// bytes is that value normalised, used only for ordering and totals.
	bytes float64
	path  string
}

func renderDU(raw string) string {
	entries, humanUnits, ok := parseDU(raw)
	if !ok || len(entries) <= duTopN {
		return raw
	}

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].bytes > entries[j].bytes })

	var total float64
	for _, e := range entries {
		total += e.bytes
	}

	var buf strings.Builder
	// Only claim a total when the units are known. Without a suffix du is
	// printing blocks whose size depends on the platform and on -k/-B, and
	// summing those into a byte figure would be an invented number.
	if humanUnits {
		fmt.Fprintf(&buf, "du: %d entries, %s total (largest first)\n", len(entries), humanBytes(total))
	} else {
		fmt.Fprintf(&buf, "du: %d entries, %.0f blocks total (largest first)\n", len(entries), total)
	}

	for _, e := range entries[:duTopN] {
		fmt.Fprintf(&buf, "  %8s  %s\n", e.size, e.path)
	}

	rest := entries[duTopN:]
	var restTotal float64
	for _, e := range rest {
		restTotal += e.bytes
	}
	if humanUnits {
		fmt.Fprintf(&buf, "  (%d smaller entries, %s)\n", len(rest), humanBytes(restTotal))
	} else {
		fmt.Fprintf(&buf, "  (%d smaller entries, %.0f blocks)\n", len(rest), restTotal)
	}
	return buf.String()
}

// parseDU reads du's "<size>\t<path>" lines. humanUnits reports whether the
// sizes carried a unit suffix, which decides whether totals can be stated in
// bytes at all.
func parseDU(raw string) (entries []duEntry, humanUnits bool, ok bool) {
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// du separates with a tab, but -h on some platforms pads with spaces.
		sizeStr, path, found := strings.Cut(line, "\t")
		if !found {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			sizeStr, path = fields[0], strings.Join(fields[1:], " ")
		}
		sizeStr = strings.TrimSpace(sizeStr)
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		b, suffixed, valid := parseSize(sizeStr)
		if !valid {
			continue
		}
		humanUnits = humanUnits || suffixed
		entries = append(entries, duEntry{size: sizeStr, bytes: b, path: path})
	}
	return entries, humanUnits, len(entries) > 0
}

// parseSize turns "1.2G", "512K", "0B" or a bare block count into a comparable
// number. suffixed reports whether a unit was present.
func parseSize(s string) (bytes float64, suffixed bool, ok bool) {
	if s == "" {
		return 0, false, false
	}
	// GNU du -h can print "1.2Gi"; the unit letter is then not the last byte.
	s = strings.TrimSuffix(s, "i")
	if s == "" {
		return 0, false, false
	}
	mult := 1.0
	switch s[len(s)-1] {
	case 'B':
		mult, suffixed = 1, true
	case 'K', 'k':
		mult, suffixed = 1<<10, true
	case 'M', 'm':
		mult, suffixed = 1<<20, true
	case 'G', 'g':
		mult, suffixed = 1<<30, true
	case 'T', 't':
		mult, suffixed = 1<<40, true
	case 'P', 'p':
		mult, suffixed = 1<<50, true
	}
	num := s
	if suffixed {
		num = s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(num), 64)
	if err != nil {
		return 0, suffixed, false
	}
	return v * mult, suffixed, true
}

func humanBytes(b float64) string {
	units := []string{"B", "K", "M", "G", "T", "P"}
	i := 0
	for b >= 1024 && i < len(units)-1 {
		b /= 1024
		i++
	}
	if b >= 100 || i == 0 {
		return fmt.Sprintf("%.0f%s", b, units[i])
	}
	return fmt.Sprintf("%.1f%s", b, units[i])
}
