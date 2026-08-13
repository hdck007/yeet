package cli

import (
	"bufio"
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

// git is one of the noisiest command families an agent runs: `git status` prints
// paragraphs of advice, `git diff` prints entire hunks, `git log` prints a
// four-line block per commit. Each subcommand below keeps the facts an agent
// acts on and drops the prose.
//
// Anything not recognised is passed straight through to git unchanged, so
// `yeet git <anything>` is always safe.

var gitCmd = &cobra.Command{
	Use:                "git <subcommand> [args...]",
	Short:              "Compact git output (status, diff, log, show, branch, stash)",
	DisableFlagParsing: true,
	Args:               cobra.MinimumNArgs(1),
	RunE:               runGit,
}

func init() {
	rootCmd.AddCommand(gitCmd)
}

func runGit(cmd *cobra.Command, args []string) error {
	if !yeetexec.Available("git") {
		return fmt.Errorf("git not found in PATH")
	}
	start := time.Now()
	args = stripYeetFlags(args)
	if len(args) == 0 {
		return fmt.Errorf("git: no subcommand given")
	}
	sub := args[0]
	rest := args[1:]

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Each handler returns the args it wants git run with, and a renderer.
	// baselineArgs is what the caller's own `git <sub> ...` would have been —
	// the only fair thing to measure against.
	type plan struct {
		runArgs      []string
		baselineArgs []string
		render       func(raw string) string
		kind         string
	}

	var p plan
	switch sub {
	case "status":
		// Porcelain is both smaller and stable to parse. The caller's baseline is
		// their own `git status`, which is what we measure against.
		p = plan{
			runArgs:      append([]string{"status", "--porcelain=v1", "--branch"}, rest...),
			baselineArgs: append([]string{"status"}, rest...),
			render:       renderGitStatus,
			kind:         analytics.BaselineAsInvoked,
		}
	case "diff":
		// Full hunks are the single biggest git cost. A per-file +/- summary is
		// what an agent needs to decide where to look.
		p = plan{
			runArgs:      append([]string{"diff", "--numstat"}, rest...),
			baselineArgs: append([]string{"diff"}, rest...),
			render:       func(raw string) string { return renderGitNumstat(raw, "diff") },
			kind:         analytics.BaselineAsInvoked,
		}
	case "log":
		p = plan{
			runArgs:      append([]string{"log", "--pretty=format:%h|%an|%ar|%s", "--no-merges"}, rest...),
			baselineArgs: append([]string{"log"}, rest...),
			render:       renderGitLog,
			kind:         analytics.BaselineAsInvoked,
		}
	case "show":
		p = plan{
			runArgs:      append([]string{"show", "--numstat", "--pretty=format:%h|%an|%ar|%s"}, rest...),
			baselineArgs: append([]string{"show"}, rest...),
			render:       renderGitShow,
			kind:         analytics.BaselineAsInvoked,
		}
	case "branch":
		p = plan{
			runArgs:      append([]string{"branch", "--format=%(refname:short)|%(committerdate:relative)|%(HEAD)"}, rest...),
			baselineArgs: append([]string{"branch"}, rest...),
			render:       renderGitBranch,
			kind:         analytics.BaselineAsInvoked,
		}
	case "stash":
		if len(rest) == 0 || rest[0] == "list" {
			p = plan{
				runArgs:      []string{"stash", "list", "--pretty=format:%gd|%s"},
				baselineArgs: []string{"stash", "list"},
				render:       renderGitStash,
				kind:         analytics.BaselineAsInvoked,
			}
		}
	}

	// Unrecognised subcommand (or a mutating one like commit/push): pass through.
	if p.render == nil {
		result := yeetexec.Run(ctx, "git", args...)
		fmt.Print(result.Stdout)
		if result.Stderr != "" {
			fmt.Fprint(os.Stderr, result.Stderr)
		}
		if result.ExitCode != 0 {
			os.Exit(result.ExitCode)
		}
		return nil
	}

	result := yeetexec.Run(ctx, "git", p.runArgs...)
	if result.ExitCode != 0 {
		// Let git speak for itself rather than rendering a half-parsed result.
		fmt.Print(result.Stdout)
		if result.Stderr != "" {
			fmt.Fprint(os.Stderr, result.Stderr)
		}
		os.Exit(result.ExitCode)
	}

	rendered := p.render(result.Stdout)

	// Measure against the caller's own command, actually run.
	baseline := yeetexec.Run(ctx, "git", p.baselineArgs...)
	baselineOut := baseline.Stdout
	if baseline.ExitCode != 0 {
		baselineOut = result.Stdout // fall back to something real, never invented
	}

	printed, _ := printBetterN(baselineOut, rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "git",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(baselineOut),
			CharsRendered: len(rendered),
			CharsPrinted:  printed,
			BaselineCmd:   "git " + strings.Join(p.baselineArgs, " "),
			YeetCmd:       "yeet git " + strings.Join(args, " "),
			BaselineKind:  p.kind,
			ExitCode:      result.ExitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

// renderGitStatus turns porcelain output into a grouped, counted summary.
func renderGitStatus(raw string) string {
	var branch string
	staged, unstaged, untracked, conflicts := []string{}, []string{}, []string{}, []string{}

	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## ") {
			branch = strings.TrimPrefix(line, "## ")
			continue
		}
		if len(line) < 4 {
			continue
		}
		x, y, path := line[0], line[1], strings.TrimSpace(line[3:])
		switch {
		case x == '?' && y == '?':
			untracked = append(untracked, path)
		case x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D'):
			conflicts = append(conflicts, path)
		default:
			if x != ' ' && x != '?' {
				staged = append(staged, fmt.Sprintf("%c %s", x, path))
			}
			if y != ' ' && y != '?' {
				unstaged = append(unstaged, fmt.Sprintf("%c %s", y, path))
			}
		}
	}

	var b strings.Builder
	if branch != "" {
		fmt.Fprintf(&b, "branch: %s\n", branch)
	}
	if len(staged)+len(unstaged)+len(untracked)+len(conflicts) == 0 {
		b.WriteString("clean\n")
		return b.String()
	}
	writeGroup(&b, "conflicts", conflicts, 20)
	writeGroup(&b, "staged", staged, 20)
	writeGroup(&b, "unstaged", unstaged, 20)
	writeGroup(&b, "untracked", untracked, 10)
	return b.String()
}

func writeGroup(b *strings.Builder, label string, items []string, max int) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "%s (%d):\n", label, len(items))
	for i, it := range items {
		if i >= max {
			fmt.Fprintf(b, "  +%d more\n", len(items)-max)
			break
		}
		fmt.Fprintf(b, "  %s\n", it)
	}
}

// renderGitNumstat summarises a numstat into per-file +/- plus a total.
func renderGitNumstat(raw, label string) string {
	type row struct {
		add, del int
		path     string
	}
	var rows []row
	totalAdd, totalDel := 0, 0

	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 3)
		if len(parts) != 3 {
			continue
		}
		a, d := parseCount(parts[0]), parseCount(parts[1])
		rows = append(rows, row{a, d, parts[2]})
		totalAdd += a
		totalDel += d
	}
	if len(rows) == 0 {
		return "no changes\n"
	}
	// Biggest changes first — that is where an agent should look.
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].add+rows[i].del > rows[j].add+rows[j].del
	})

	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d files, +%d -%d\n", label, len(rows), totalAdd, totalDel)
	for i, r := range rows {
		if i >= 40 {
			fmt.Fprintf(&b, "  +%d more files\n", len(rows)-40)
			break
		}
		fmt.Fprintf(&b, "  +%-5d -%-5d %s\n", r.add, r.del, r.path)
	}
	return b.String()
}

func parseCount(s string) int {
	if s == "-" { // binary file
		return 0
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func renderGitLog(raw string) string {
	var b strings.Builder
	n := 0
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		f := strings.SplitN(sc.Text(), "|", 4)
		if len(f) != 4 {
			continue
		}
		n++
		fmt.Fprintf(&b, "%s %s (%s) %s\n", f[0], f[3], f[2], shortName(f[1]))
	}
	if n == 0 {
		return "no commits\n"
	}
	return fmt.Sprintf("%d commits:\n", n) + b.String()
}

func renderGitShow(raw string) string {
	lines := strings.SplitN(raw, "\n", 2)
	header := ""
	if f := strings.SplitN(lines[0], "|", 4); len(f) == 4 {
		header = fmt.Sprintf("%s %s (%s) %s\n", f[0], f[3], f[2], shortName(f[1]))
	} else {
		header = lines[0] + "\n"
	}
	body := ""
	if len(lines) > 1 {
		body = renderGitNumstat(lines[1], "changes")
	}
	return header + body
}

func renderGitBranch(raw string) string {
	var b strings.Builder
	n := 0
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		f := strings.SplitN(sc.Text(), "|", 3)
		if len(f) < 2 {
			continue
		}
		n++
		marker := " "
		if len(f) == 3 && strings.TrimSpace(f[2]) == "*" {
			marker = "*"
		}
		fmt.Fprintf(&b, "%s %s (%s)\n", marker, f[0], f[1])
	}
	if n == 0 {
		return "no branches\n"
	}
	return fmt.Sprintf("%d branches:\n", n) + b.String()
}

func renderGitStash(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "no stashes\n"
	}
	var b strings.Builder
	n := 0
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		f := strings.SplitN(sc.Text(), "|", 2)
		if len(f) != 2 {
			continue
		}
		n++
		fmt.Fprintf(&b, "%s %s\n", f[0], f[1])
	}
	return fmt.Sprintf("%d stashes:\n", n) + b.String()
}

// shortName keeps an author identifiable without spending a full name on it.
func shortName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.IndexByte(name, ' '); i > 0 {
		return name[:i]
	}
	return name
}
