package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var dockerCmd = &cobra.Command{
	Use:                "docker <subcommand> [args...]",
	Short:              "docker read commands with the wide columns dropped",
	Args:               cobra.ArbitraryArgs,
	RunE:               runDocker,
	DisableFlagParsing: true,
}

func init() {
	rootCmd.AddCommand(dockerCmd)
}

const (
	dockerMaxRows = 40
	dockerMaxLog  = 120
)

func runDocker(cmd *cobra.Command, args []string) error {
	start := time.Now()
	args = stripYeetFlags(args)

	if !yeetexec.Available("docker") {
		return fmt.Errorf("docker not found in PATH")
	}
	if len(args) > 0 && guardDocker(append([]string{"docker"}, args...)) == vNever {
		return fmt.Errorf("yeet docker only wraps read-only subcommands; run `docker %s` directly", strings.Join(args, " "))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result := yeetexec.Run(ctx, "docker", args...)
	raw := result.Stdout
	if strings.TrimSpace(raw) == "" {
		raw = result.Stdout + result.Stderr
	}

	rendered := renderDocker(args, raw)
	if rawOutput {
		rendered = raw
	}
	printed, _ := printBetterN(raw, rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "docker",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(raw),
			CharsRendered: len(rendered),
			CharsPrinted:  printed,
			BaselineCmd:   "docker " + strings.Join(args, " "),
			YeetCmd:       "yeet docker " + strings.Join(args, " "),
			BaselineKind:  analytics.BaselineAsInvoked,
			ExitCode:      result.ExitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

func renderDocker(args []string, raw string) string {
	sub, group := "", ""
	if len(args) > 0 {
		sub = args[0]
	}
	if len(args) > 1 {
		group = args[1]
	}

	switch {
	case sub == "logs", sub == "compose" && group == "logs":
		return renderLogStream(raw, dockerMaxLog)
	case sub == "ps", sub == "compose" && group == "ps", sub == "container" && group == "ls":
		return renderDockerPS(raw)
	case sub == "images", sub == "image" && group == "ls":
		return renderDockerTable(raw, []string{"IMAGE ID", "DIGEST", "CREATED"}, nil)
	case sub == "inspect":
		// inspect is JSON; the json renderer already knows how to shrink it.
		return raw
	}

	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	if len(lines) <= dockerMaxRows {
		return raw
	}
	return strings.Join(capLines(lines, dockerMaxRows), "\n") + "\n"
}

// dockerPSDrop are the `docker ps` columns whose content is either redundant
// with a column that is kept or unusable on its own. The container ID is a
// twelve-character hash that every other docker command will also accept the
// name for; COMMAND is truncated by docker itself into an ellipsis that says
// nothing; CREATED duplicates what STATUS already reports for a running
// container.
var dockerPSDrop = []string{"CONTAINER ID", "COMMAND", "CREATED"}

// renderDockerPS keeps the columns that answer "what is up, and on what port",
// and groups by status so a machine with thirty exited containers reports that
// in one line.
func renderDockerPS(raw string) string {
	t, ok := parseTable(raw)
	if !ok {
		return raw
	}

	counts := map[string]int{}
	for _, row := range t.rows {
		counts[dockerStatusClass(t.cell(row, "STATUS"))]++
	}

	var classes []string
	for c := range counts {
		classes = append(classes, c)
	}
	sort.SliceStable(classes, func(i, j int) bool {
		if counts[classes[i]] != counts[classes[j]] {
			return counts[classes[i]] > counts[classes[j]]
		}
		return classes[i] < classes[j]
	})
	var summary []string
	for _, c := range classes {
		summary = append(summary, fmt.Sprintf("%d %s", counts[c], c))
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "%d containers: %s\n", len(t.rows), strings.Join(summary, ", "))
	buf.WriteString(renderDockerTable(raw, dockerPSDrop, dedupPortMappings))
	return buf.String()
}

// dedupPortMappings drops docker's IPv6 restatement of a mapping it has already
// printed for IPv4. `0.0.0.0:8000->8080/tcp, :::8000->8080/tcp` is one published
// port described twice, and the second copy answers no question the first does
// not.
func dedupPortMappings(ports string) string {
	if ports == "" {
		return ports
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range strings.Split(ports, ", ") {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		// Key on the published port and its target, ignoring the bind address:
		// that is what makes the IPv4 and IPv6 forms the same mapping.
		key := m
		if host, target, found := strings.Cut(m, "->"); found {
			port := host
			if i := strings.LastIndexByte(host, ':'); i >= 0 {
				port = host[i+1:]
			}
			key = port + "->" + target
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return strings.Join(out, ", ")
}

// dockerStatusClass reduces "Up 3 hours (healthy)" and "Exited (0) 2 days ago"
// to the word that distinguishes them.
func dockerStatusClass(status string) string {
	f := strings.Fields(status)
	if len(f) == 0 {
		return "unknown"
	}
	switch f[0] {
	case "Up":
		if strings.Contains(status, "unhealthy") {
			return "up (unhealthy)"
		}
		if strings.Contains(status, "health: starting") {
			return "up (starting)"
		}
		return "up"
	case "Exited":
		if strings.Contains(status, "(0)") {
			return "exited (0)"
		}
		return "exited (nonzero)"
	}
	return strings.ToLower(f[0])
}

// renderDockerTable reprints a table without the named columns. portFn, when
// given, rewrites the PORTS cell of every row.
func renderDockerTable(raw string, drop []string, portFn func(string) string) string {
	t, ok := parseTable(raw)
	if !ok {
		return raw
	}

	dropSet := map[string]bool{}
	for _, d := range drop {
		dropSet[strings.ToUpper(d)] = true
	}

	var keep []int
	for i, c := range t.cols {
		if !dropSet[strings.ToUpper(c)] {
			keep = append(keep, i)
		}
	}
	if len(keep) == len(t.cols) || len(keep) == 0 {
		return raw
	}

	if portFn != nil {
		if pi := t.index("PORTS"); pi >= 0 {
			for _, row := range t.rows {
				if pi < len(row) {
					row[pi] = portFn(row[pi])
				}
			}
		}
	}

	// Width the kept columns to their own content rather than to the original
	// header padding, which was sized for the columns that were dropped.
	widths := make([]int, len(keep))
	for j, c := range keep {
		widths[j] = len(t.cols[c])
	}
	for _, row := range t.rows {
		for j, c := range keep {
			if c < len(row) && len(row[c]) > widths[j] {
				widths[j] = len(row[c])
			}
		}
	}

	var buf strings.Builder
	writeRow := func(get func(int) string) {
		var cells []string
		for j, c := range keep {
			v := get(c)
			if j < len(keep)-1 {
				v = v + strings.Repeat(" ", widths[j]-len(v))
			}
			cells = append(cells, v)
		}
		buf.WriteString(strings.TrimRight(strings.Join(cells, "  "), " ") + "\n")
	}

	writeRow(func(c int) string { return t.cols[c] })
	shown := t.rows
	if len(shown) > dockerMaxRows {
		shown = shown[:dockerMaxRows]
	}
	for _, row := range shown {
		r := row
		writeRow(func(c int) string {
			if c < len(r) {
				return r[c]
			}
			return ""
		})
	}
	if n := len(t.rows) - len(shown); n > 0 {
		fmt.Fprintf(&buf, "(%d more rows; --raw for all)\n", n)
	}
	return buf.String()
}
