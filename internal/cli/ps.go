package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var psCmd = &cobra.Command{
	Use:                "ps [flags] [args...]",
	Short:              "Process list grouped by program, with CPU/memory leaders",
	Args:               cobra.ArbitraryArgs,
	RunE:               runPS,
	DisableFlagParsing: true, // ps flags (aux, -ef) are not cobra flags
}

func init() {
	rootCmd.AddCommand(psCmd)
}

// A `ps aux` on a working machine is several hundred rows of which almost all
// are helper processes with near-identical command lines. The two questions an
// agent ever asks of it are "is X running?" and "what is eating the machine?",
// and both are answered by a grouped view plus the leaders — not by the rows.
const (
	psTopN      = 8  // rows in each of the CPU and memory leader tables
	psMaxGroups = 60 // program groups listed before the tail is collapsed
	psMaxCmdLen = 60 // characters of command line kept on a leader row
)

func runPS(cmd *cobra.Command, args []string) error {
	start := time.Now()
	args = stripYeetFlags(args)

	if !yeetexec.Available("ps") {
		return fmt.Errorf("ps not found in PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The baseline is exactly this invocation: yeet runs the same ps the caller
	// asked for and only reshapes what comes back.
	result := yeetexec.Run(ctx, "ps", args...)
	raw := result.Stdout
	if raw == "" {
		raw = result.Stderr
	}

	rendered := renderPS(raw)
	if rawOutput {
		rendered = raw
	}
	printed, _ := printBetterN(raw, rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "ps",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(raw),
			CharsRendered: len(rendered),
			CharsPrinted:  printed,
			BaselineCmd:   "ps " + strings.Join(args, " "),
			YeetCmd:       "yeet ps " + strings.Join(args, " "),
			BaselineKind:  analytics.BaselineAsInvoked,
			ExitCode:      result.ExitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

// psProc is one row of ps output, normalised across the layouts that `aux` and
// `-ef` produce.
type psProc struct {
	pid     string
	cpu     float64
	mem     float64
	hasLoad bool // false when the layout carried no %CPU/%MEM columns
	command string
	label   string
}

func renderPS(raw string) string {
	procs, header, ok := parsePS(raw)
	// Below a couple of screens' worth there is nothing to summarise, and a
	// summary would lose the detail without saving anything worth having.
	if !ok || len(procs) < 12 {
		return raw
	}

	var totalCPU, totalMem float64
	anyLoad := false
	for _, p := range procs {
		totalCPU += p.cpu
		totalMem += p.mem
		anyLoad = anyLoad || p.hasLoad
	}

	var buf strings.Builder
	if anyLoad {
		fmt.Fprintf(&buf, "%d procs · cpu %.1f%% · mem %.1f%%\n", len(procs), totalCPU, totalMem)
	} else {
		fmt.Fprintf(&buf, "%d procs (%s carried no cpu/mem columns)\n", len(procs), header)
	}

	if anyLoad {
		writePSLeaders(&buf, "top cpu", procs, func(a, b psProc) bool { return a.cpu > b.cpu })
		writePSLeaders(&buf, "top mem", procs, func(a, b psProc) bool { return a.mem > b.mem })
	}

	writePSGroups(&buf, procs)
	return buf.String()
}

func writePSLeaders(buf *strings.Builder, title string, procs []psProc, less func(a, b psProc) bool) {
	sorted := make([]psProc, len(procs))
	copy(sorted, procs)
	sort.SliceStable(sorted, func(i, j int) bool { return less(sorted[i], sorted[j]) })

	n := psTopN
	if len(sorted) < n {
		n = len(sorted)
	}
	// A table of zeros tells the reader nothing they did not already know from
	// the totals line.
	if n == 0 || (sorted[0].cpu == 0 && sorted[0].mem == 0) {
		return
	}

	fmt.Fprintf(buf, "\n%s:\n", title)
	for _, p := range sorted[:n] {
		fmt.Fprintf(buf, "  %5.1f %5.1f %7s  %s\n", p.cpu, p.mem, p.pid, truncateMiddle(p.command, psMaxCmdLen))
	}
}

type psGroup struct {
	label string
	count int
	cpu   float64
	mem   float64
	pids  []string
}

func writePSGroups(buf *strings.Builder, procs []psProc) {
	byLabel := map[string]*psGroup{}
	var order []string
	for _, p := range procs {
		g, seen := byLabel[p.label]
		if !seen {
			g = &psGroup{label: p.label}
			byLabel[p.label] = g
			order = append(order, p.label)
		}
		g.count++
		g.cpu += p.cpu
		g.mem += p.mem
		if len(g.pids) < 4 {
			g.pids = append(g.pids, p.pid)
		}
	}

	groups := make([]*psGroup, 0, len(order))
	for _, l := range order {
		groups = append(groups, byLabel[l])
	}
	// Heaviest first: that is the order in which the reader wants to scan.
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].cpu != groups[j].cpu {
			return groups[i].cpu > groups[j].cpu
		}
		if groups[i].count != groups[j].count {
			return groups[i].count > groups[j].count
		}
		return groups[i].label < groups[j].label
	})

	fmt.Fprintf(buf, "\nby program (%d procs → %d programs):\n", len(procs), len(groups))

	shown := groups
	if len(shown) > psMaxGroups {
		shown = groups[:psMaxGroups]
	}
	for _, g := range shown {
		fmt.Fprintf(buf, "  %s", g.label)
		if g.count > 1 {
			fmt.Fprintf(buf, " x%d", g.count)
		}
		fmt.Fprintf(buf, "  cpu %.1f  mem %.1f  pid %s", g.cpu, g.mem, strings.Join(g.pids, ","))
		if g.count > len(g.pids) {
			fmt.Fprintf(buf, "+%d", g.count-len(g.pids))
		}
		buf.WriteString("\n")
	}

	if rest := groups[len(shown):]; len(rest) > 0 {
		var procCount int
		var cpu, mem float64
		for _, g := range rest {
			procCount += g.count
			cpu += g.cpu
			mem += g.mem
		}
		fmt.Fprintf(buf, "  (%d more programs, %d procs, cpu %.1f, mem %.1f)\n", len(rest), procCount, cpu, mem)
	}
}

// parsePS reads ps output using its own header row, so the same code handles
// `aux`, `-ef`, and any explicit -o list. Returning ok=false means the layout
// was not recognisable and the raw output should be passed through rather than
// guessed at.
func parsePS(raw string) (procs []psProc, header string, ok bool) {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	if len(lines) < 2 {
		return nil, "", false
	}

	cols := strings.Fields(lines[0])
	if len(cols) < 2 {
		return nil, "", false
	}
	idx := map[string]int{}
	for i, c := range cols {
		idx[strings.ToUpper(strings.TrimPrefix(c, "%"))] = i
	}

	pidAt, hasPID := idx["PID"]
	cpuAt, hasCPU := idx["CPU"]
	memAt, hasMem := idx["MEM"]
	cmdAt := len(cols) - 1
	if !hasPID {
		return nil, "", false
	}
	// The command must be the trailing column for the "rest of the line"
	// split to be correct.
	switch strings.ToUpper(cols[cmdAt]) {
	case "COMMAND", "CMD", "ARGS":
	default:
		return nil, "", false
	}

	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := splitLeadingFields(line, len(cols))
		if len(f) != len(cols) {
			continue
		}
		p := psProc{pid: f[pidAt], command: strings.TrimSpace(f[cmdAt])}
		if hasCPU {
			p.cpu, _ = strconv.ParseFloat(f[cpuAt], 64)
			p.hasLoad = true
		}
		if hasMem {
			p.mem, _ = strconv.ParseFloat(f[memAt], 64)
			p.hasLoad = true
		}
		p.label = psLabel(p.command)
		procs = append(procs, p)
	}
	if len(procs) == 0 {
		return nil, "", false
	}
	return procs, strings.Join(cols, " "), true
}

// splitLeadingFields takes the first n-1 whitespace-separated tokens and
// returns them plus the unsplit remainder. ps pads columns to variable widths
// and the command itself contains spaces, so neither a plain Fields nor a
// SplitN gets this right on its own.
func splitLeadingFields(line string, n int) []string {
	out := make([]string, 0, n)
	i := 0
	for len(out) < n-1 {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i >= len(line) {
			return out
		}
		start := i
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		out = append(out, line[start:i])
	}
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return append(out, line[i:])
}

// psInterpreters run someone else's code, so their own name says nothing about
// what the process is. For these the script or module being run is the
// identifying part — `node:vitest` answers "is vitest running?" where a
// fourteenth row reading `node` does not.
var psInterpreters = map[string]bool{
	"node": true, "node.exe": true, "deno": true, "bun": true,
	"python": true, "python2": true, "python3": true, "pypy": true,
	"ruby": true, "perl": true, "php": true, "java": true,
	"sh": true, "bash": true, "zsh": true, "fish": true,
	"npm": true, "npx": true, "pnpm": true, "yarn": true,
}

// psLabel reduces a command line to the shortest string that still identifies
// the program.
func psLabel(command string) string {
	// Quote-aware, so `bash -lc 'make build'` yields one argument rather than
	// a label built out of half a quoted string.
	fields := splitArgs(command)
	if len(fields) == 0 {
		return "?"
	}
	// Bracketed kernel threads on Linux are already labels, and taking a path
	// basename of one would cut it at the slash in `[kworker/2:1-events]`.
	if strings.HasPrefix(fields[0], "[") {
		return fields[0]
	}
	base := filepath.Base(fields[0])
	if !psInterpreters[base] {
		return base
	}
	for _, arg := range fields[1:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		script := filepath.Base(arg)
		if script == "" || script == "." {
			continue
		}
		// `.../node_modules/.bin/vitest` and `.../dist/cli.js` both reduce to
		// something the reader can recognise.
		script = strings.TrimSuffix(script, ".js")
		script = strings.TrimSuffix(script, ".mjs")
		script = strings.TrimSuffix(script, ".cjs")
		script = strings.TrimSuffix(script, ".py")
		script = strings.TrimSuffix(script, ".rb")
		if script == "" {
			continue
		}
		return base + ":" + script
	}
	return base
}

// truncateMiddle keeps both ends of a path-like string, which is where the
// identifying parts live: the leading directories say which install it came
// from and the tail says what it is.
func truncateMiddle(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max || max < 8 {
		return s
	}
	keepTail := (max - 3) / 2
	keepHead := max - 3 - keepTail
	return s[:keepHead] + "..." + s[len(s)-keepTail:]
}
