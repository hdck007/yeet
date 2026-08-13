package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"sort"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

// git is one of the noisiest command families an agent runs: `git status` prints
// paragraphs of advice, `git diff` prints entire hunks, `git log` prints a
// four-line block per commit. Each handler below keeps the facts an agent acts
// on and drops the prose.
//
// Anything not recognised is handed to git with the terminal attached, so
// `yeet git <anything>` behaves exactly like git — including commands that open
// an editor or prompt for credentials.

var gitCmd = &cobra.Command{
	Use:                "git <subcommand> [args...]",
	Short:              "Compact git output (status, diff, log, show, branch, stash, push, pull, fetch, worktree)",
	DisableFlagParsing: true,
	Args:               cobra.MinimumNArgs(1),
	RunE:               runGit,
}

// gitDiffContent shows the changed lines, not just which files changed. Off by
// default: the per-file summary is what an agent needs to decide where to look,
// and the hunks are what make a raw diff expensive.
var gitDiffContent bool

func init() {
	rootCmd.AddCommand(gitCmd)
}

// gitGlobalFlags are git's own options, which appear *before* the subcommand.
// They have to be recognised and carried through, otherwise `git -C path status`
// looks like a subcommand called "-C" and loses all compaction.
// The bool map value reports whether the flag takes a separate value argument.
var gitGlobalFlags = map[string]bool{
	"-C":                   true,
	"-c":                   true,
	"--git-dir":            true,
	"--work-tree":          true,
	"--namespace":          true,
	"--exec-path":          true,
	"--no-pager":           false,
	"--paginate":           false,
	"-p":                   false,
	"--no-optional-locks":  false,
	"--bare":               false,
	"--literal-pathspecs":  false,
	"--no-replace-objects": false,
	"--icase-pathspecs":    false,
}

// splitGitGlobals peels git's global options off the front of the argument list
// and returns them alongside the remaining subcommand and its arguments.
func splitGitGlobals(args []string) (globals []string, rest []string) {
	i := 0
	for i < len(args) {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			break
		}
		// --git-dir=path form carries its value inline.
		if eq := strings.IndexByte(a, '='); eq > 0 {
			if _, ok := gitGlobalFlags[a[:eq]]; ok {
				globals = append(globals, a)
				i++
				continue
			}
			break
		}
		takesValue, ok := gitGlobalFlags[a]
		if !ok {
			break // not a git global — leave it for the subcommand
		}
		globals = append(globals, a)
		i++
		if takesValue {
			if i >= len(args) {
				break
			}
			globals = append(globals, args[i])
			i++
		}
	}
	return globals, args[i:]
}

// execInherit runs a command with the terminal attached and exits with its
// status. Required for anything that may open an editor or prompt: capturing
// stdout leaves the editor with nowhere to draw and the prompt invisible.
func execInherit(name string, args ...string) {
	c := osexec.Command(name, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		if ee, ok := err.(*osexec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "yeet git: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// gitPlan is how one subcommand is handled: which git invocation produces the
// data, which invocation the saving is fairly measured against, and how to
// render it. combineErr pulls in stderr, which is where git writes most of what
// push, pull, and fetch have to say.
type gitPlan struct {
	runArgs      []string
	baselineArgs []string
	render       func(raw string) string
	combineErr   bool
}

func runGit(cmd *cobra.Command, args []string) error {
	if !yeetexec.Available("git") {
		return fmt.Errorf("git not found in PATH")
	}
	start := time.Now()

	args = stripYeetFlags(args)
	gitDiffContent = false
	filtered := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--content" || a == "--patch-content" {
			gitDiffContent = true
			continue
		}
		filtered = append(filtered, a)
	}
	args = filtered
	if len(args) == 0 {
		return fmt.Errorf("git: no subcommand given")
	}

	globals, tail := splitGitGlobals(args)
	if len(tail) == 0 {
		execInherit("git", args...)
	}
	sub, rest := tail[0], tail[1:]

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	p := planGit(sub, rest)
	if p == nil {
		// Unrecognised, interactive, or state-changing in a way we do not
		// summarise: hand it to git with the terminal attached.
		execInherit("git", args...)
	}

	// Global flags belong ahead of the subcommand in both invocations.
	runArgs := append(append([]string{}, globals...), p.runArgs...)
	baseArgs := append(append([]string{}, globals...), p.baselineArgs...)

	result := yeetexec.Run(ctx, "git", runArgs...)
	raw := result.Stdout
	if p.combineErr {
		raw += result.Stderr
	}
	if result.ExitCode != 0 {
		// Let git speak for itself rather than rendering a half-parsed result.
		fmt.Print(result.Stdout)
		if result.Stderr != "" {
			fmt.Fprint(os.Stderr, result.Stderr)
		}
		os.Exit(result.ExitCode)
	}

	rendered := p.render(raw)

	// Measure against the caller's own command, actually run.
	baselineOut := raw
	kind := analytics.BaselineAsInvoked
	baselineLabel := "git " + strings.Join(runArgs, " ")
	if p.baselineArgs != nil {
		baseline := yeetexec.Run(ctx, "git", baseArgs...)
		baselineLabel = "git " + strings.Join(baseArgs, " ")
		baselineOut = baseline.Stdout
		if p.combineErr {
			baselineOut += baseline.Stderr
		}
		if baseline.ExitCode != 0 {
			baselineOut = raw // fall back to something real, never invented
			kind = analytics.BaselineSynthetic
		}
	}
	// For push/pull/fetch there is no second run to make — repeating them would
	// change state. The raw output of the run we just did *is* the native
	// output, so it is the honest baseline.

	printed, _ := printBetterN(baselineOut, rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "git",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(baselineOut),
			CharsRendered: len(rendered),
			CharsPrinted:  printed,
			BaselineCmd:   baselineLabel,
			YeetCmd:       "yeet git " + strings.Join(args, " "),
			BaselineKind:  kind,
			ExitCode:      result.ExitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

// planGit returns nil for anything that must reach git untouched.
func planGit(sub string, rest []string) *gitPlan {
	switch sub {
	case "status":
		return &gitPlan{
			runArgs:      append([]string{"status", "--porcelain=v1", "--branch"}, rest...),
			baselineArgs: append([]string{"status"}, rest...),
			render:       renderGitStatus,
		}
	case "diff":
		if gitDiffContent {
			// Keep the changed lines but drop the index/mode/context noise.
			return &gitPlan{
				runArgs:      append([]string{"diff", "--unified=0", "--no-color", "--no-prefix"}, rest...),
				baselineArgs: append([]string{"diff"}, rest...),
				render:       renderGitDiffContent,
			}
		}
		return &gitPlan{
			runArgs:      append([]string{"diff", "--numstat"}, rest...),
			baselineArgs: append([]string{"diff"}, rest...),
			render:       func(raw string) string { return renderGitNumstat(raw, "diff") },
		}
	case "log":
		return &gitPlan{
			runArgs:      append([]string{"log", "--pretty=format:%h|%an|%ar|%s", "--no-merges"}, rest...),
			baselineArgs: append([]string{"log"}, rest...),
			render:       renderGitLog,
		}
	case "show":
		return &gitPlan{
			runArgs:      append([]string{"show", "--numstat", "--pretty=format:%h|%an|%ar|%s"}, rest...),
			baselineArgs: append([]string{"show"}, rest...),
			render:       renderGitShow,
		}
	case "branch":
		// Anything beyond listing creates, moves, or deletes a branch.
		if gitBranchMutates(rest) {
			return nil
		}
		return &gitPlan{
			runArgs:      append([]string{"branch", "--format=%(refname:short)|%(committerdate:relative)|%(HEAD)"}, rest...),
			baselineArgs: append([]string{"branch"}, rest...),
			render:       renderGitBranch,
		}
	case "stash":
		if len(rest) == 0 {
			return nil // bare `git stash` stashes changes
		}
		switch rest[0] {
		case "list":
			return &gitPlan{
				runArgs:      []string{"stash", "list", "--pretty=format:%gd|%s"},
				baselineArgs: []string{"stash", "list"},
				render:       renderGitStash,
			}
		case "show":
			return &gitPlan{
				runArgs:      append([]string{"stash", "show", "--numstat"}, rest[1:]...),
				baselineArgs: append([]string{"stash", "show"}, rest[1:]...),
				render:       func(raw string) string { return renderGitNumstat(raw, "stash") },
			}
		}
		return nil // pop/apply/drop/push change state
	case "worktree":
		if len(rest) > 0 && rest[0] == "list" {
			return &gitPlan{
				runArgs:      []string{"worktree", "list", "--porcelain"},
				baselineArgs: []string{"worktree", "list"},
				render:       renderGitWorktree,
			}
		}
		return nil // add/remove/prune change state
	case "push":
		// Push is chatty on stderr and says one thing that matters: it worked,
		// and to where. Credential prompts still reach the terminal, because git
		// talks to the tty directly rather than through stderr.
		return &gitPlan{
			runArgs:      append([]string{"push"}, rest...),
			baselineArgs: nil, // never run a push twice
			render:       renderGitPush,
			combineErr:   true,
		}
	case "pull":
		return &gitPlan{
			runArgs:      append([]string{"pull"}, rest...),
			baselineArgs: nil,
			render:       renderGitPull,
			combineErr:   true,
		}
	case "fetch":
		return &gitPlan{
			runArgs:      append([]string{"fetch"}, rest...),
			baselineArgs: nil,
			render:       renderGitFetch,
			combineErr:   true,
		}
	}
	return nil
}

// gitBranchMutates reports whether a `git branch` invocation does more than list.
func gitBranchMutates(rest []string) bool {
	for _, a := range rest {
		switch a {
		case "-a", "--all", "-r", "--remotes", "-v", "-vv", "--verbose", "--list":
			continue
		}
		if strings.HasPrefix(a, "-") {
			return true // -d, -D, -m, -M, -c, --set-upstream-to, ...
		}
		return true // a bare name creates a branch
	}
	return false
}

// ─── Renderers ────────────────────────────────────────────────────────────────

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

// renderGitDiffContent keeps the changed lines and throws away everything that
// carries no information for a reader: index hashes, mode bits, and the @@ hunk
// arithmetic. Output is capped per file and overall — an uncapped diff is the
// thing that made the raw command expensive in the first place, so reproducing
// it in full would defeat the purpose.
const (
	diffMaxLinesPerFile = 40
	diffMaxLinesTotal   = 400
)

func renderGitDiffContent(raw string) string {
	type fileDiff struct {
		path  string
		lines []string
		adds  int
		dels  int
	}
	var files []*fileDiff
	var cur *fileDiff
	adds, dels := 0, 0

	sc := bufio.NewScanner(strings.NewReader(raw))
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			f := strings.Fields(line)
			path := ""
			if len(f) >= 4 {
				path = strings.TrimPrefix(f[2], "a/")
			}
			cur = &fileDiff{path: path}
			files = append(files, cur)
		case strings.HasPrefix(line, "index "),
			strings.HasPrefix(line, "old mode"), strings.HasPrefix(line, "new mode"),
			strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "),
			strings.HasPrefix(line, "similarity index"),
			strings.HasPrefix(line, "rename from"), strings.HasPrefix(line, "rename to"),
			strings.HasPrefix(line, "new file mode"), strings.HasPrefix(line, "deleted file mode"),
			strings.HasPrefix(line, "@@"):
			continue
		case strings.HasPrefix(line, "+"):
			adds++
			if cur != nil {
				cur.adds++
				cur.lines = append(cur.lines, line)
			}
		case strings.HasPrefix(line, "-"):
			dels++
			if cur != nil {
				cur.dels++
				cur.lines = append(cur.lines, line)
			}
		}
	}
	if adds+dels == 0 {
		return "no changes\n"
	}

	// Smallest changes first, so the per-file caps spend the budget on files a
	// reader can actually take in whole.
	sort.SliceStable(files, func(i, j int) bool {
		return len(files[i].lines) < len(files[j].lines)
	})

	var b strings.Builder
	fmt.Fprintf(&b, "diff: %d files, +%d -%d\n", len(files), adds, dels)
	shown := 0
	truncatedFiles := 0
	for _, f := range files {
		if shown >= diffMaxLinesTotal {
			truncatedFiles++
			continue
		}
		fmt.Fprintf(&b, "\n%s (+%d -%d)\n", f.path, f.adds, f.dels)
		for i, l := range f.lines {
			if i >= diffMaxLinesPerFile || shown >= diffMaxLinesTotal {
				fmt.Fprintf(&b, "  ... +%d more lines in this file\n", len(f.lines)-i)
				break
			}
			// Long lines are as expensive here as anywhere else.
			if len(l) > 200 {
				l = l[:197] + "..."
			}
			fmt.Fprintf(&b, "  %s\n", l)
			shown++
		}
	}
	if truncatedFiles > 0 {
		fmt.Fprintf(&b, "\n... %d more files not shown (use the summary form, then read them)\n", truncatedFiles)
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

// renderGitWorktree reduces porcelain worktree output to one line each.
func renderGitWorktree(raw string) string {
	var out []string
	var path, head, branch string
	flush := func() {
		if path == "" {
			return
		}
		short := head
		if len(short) > 8 {
			short = short[:8]
		}
		disp := path
		if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(disp, home) {
			disp = "~" + strings.TrimPrefix(disp, home)
		}
		if branch != "" {
			out = append(out, fmt.Sprintf("%s %s [%s]", disp, short, branch))
		} else {
			out = append(out, fmt.Sprintf("%s %s (detached)", disp, short))
		}
		path, head, branch = "", "", ""
	}
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	if len(out) == 0 {
		return "no worktrees\n"
	}
	return fmt.Sprintf("%d worktrees:\n", len(out)) + "  " + strings.Join(out, "\n  ") + "\n"
}

// renderGitPush keeps the destination and the ref update; the progress counters
// and the advice block carry nothing an agent needs.
func renderGitPush(raw string) string {
	dest, refs := "", []string{}
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "To "):
			dest = strings.TrimPrefix(line, "To ")
		case strings.HasPrefix(line, "*") || strings.HasPrefix(line, "+"),
			strings.Contains(line, "->"):
			if !strings.HasPrefix(line, "remote:") && strings.Contains(line, "->") {
				refs = append(refs, strings.Join(strings.Fields(line), " "))
			}
		case strings.Contains(line, "Everything up-to-date"):
			return "ok up-to-date\n"
		}
	}
	if dest == "" && len(refs) == 0 {
		return "ok\n"
	}
	var b strings.Builder
	b.WriteString("ok pushed")
	if dest != "" {
		fmt.Fprintf(&b, " -> %s", dest)
	}
	b.WriteString("\n")
	for _, r := range refs {
		fmt.Fprintf(&b, "  %s\n", r)
	}
	return b.String()
}

// renderGitPull keeps the merge strategy line and the file summary.
func renderGitPull(raw string) string {
	var summary, strategy string
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.Contains(line, "Already up to date"):
			return "ok up-to-date\n"
		case strings.HasPrefix(line, "Fast-forward"), strings.HasPrefix(line, "Merge made"):
			strategy = line
		case strings.Contains(line, "file changed"), strings.Contains(line, "files changed"):
			summary = line
		case strings.HasPrefix(line, "CONFLICT"), strings.HasPrefix(line, "Automatic merge failed"):
			// Conflicts are the one case where the detail matters — keep it all.
			return raw
		}
	}
	out := "ok pulled"
	if strategy != "" {
		out += " (" + strategy + ")"
	}
	if summary != "" {
		out += "\n  " + summary
	}
	return out + "\n"
}

// renderGitFetch keeps only the refs that actually moved.
func renderGitFetch(raw string) string {
	var refs []string
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.Contains(line, "->") && !strings.HasPrefix(line, "remote:") {
			refs = append(refs, strings.Join(strings.Fields(line), " "))
		}
	}
	if len(refs) == 0 {
		return "ok fetched (no new refs)\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ok fetched (%d refs)\n", len(refs))
	for i, r := range refs {
		if i >= 20 {
			fmt.Fprintf(&b, "  +%d more\n", len(refs)-20)
			break
		}
		fmt.Fprintf(&b, "  %s\n", r)
	}
	return b.String()
}

// shortName keeps an author identifiable without spending a full name on it.
func shortName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.IndexByte(name, ' '); i > 0 {
		return name[:i]
	}
	return name
}
