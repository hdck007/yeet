package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// codeExts are file extensions where yeet smart produces useful declaration+line-number output.
// For all other extensions (README.md, .yml, .sh, etc.) bare reads pass through unchanged.
var codeExts = map[string]bool{
	".go":  true,
	".rs":  true,
	".py":  true,
	".ts":  true,
	".tsx": true,
	".js":  true,
	".jsx": true,
	".rb":  true,
}

// rewriteRule maps a raw command prefix to its yeet equivalent.
type rewriteRule struct {
	// prefix is the literal string the raw command must start with.
	prefix string
	// yeetPrefix replaces the matched prefix in the output.
	yeetPrefix string
}

// rules is the single source of truth for all rewrite mappings.
// To add a new rewrite, add an entry here — do not touch the hook script.
var rules = []rewriteRule{
	{prefix: "cat ", yeetPrefix: "yeet read "},
	{prefix: "grep ", yeetPrefix: "yeet grep "},
	{prefix: "ls ", yeetPrefix: "yeet ls "},
	{prefix: "ls\n", yeetPrefix: "yeet ls"},
	{prefix: "find ", yeetPrefix: "yeet find "},
	{prefix: "diff ", yeetPrefix: "yeet diff "},

	// git and gh are among the noisiest commands an agent runs. Only the
	// read-only subcommands are rewritten — see gitReadOnly / ghReadOnly below;
	// anything that mutates state must reach the real binary untouched.
	{prefix: "git ", yeetPrefix: "yeet git "},
	{prefix: "gh ", yeetPrefix: "yeet gh "},
}

// gitReadOnly lists the git subcommands that are safe to rewrite. A rewrite of
// commit, push, rebase, or reset would route a state-changing command through
// yeet, so those are deliberately absent and pass through untouched.
var gitReadOnly = map[string]bool{
	"status":   true,
	"diff":     true,
	"log":      true,
	"show":     true,
	"branch":   true, // listing only — see gitBranchMutates
	"stash":    true, // `stash list` / `stash show` only
	"worktree": true, // `worktree list` only
}

// ghReadOnly lists the gh subcommand pairs that are safe to rewrite, keyed
// "<group> <sub>". Creating, merging, closing, or editing must never be
// rewritten.
var ghReadOnly = map[string]bool{
	"pr list":    true,
	"pr view":    true,
	"pr checks":  true,
	"issue list": true,
	"issue view": true,
	"run list":   true,
	"run view":   true,
}

// safeToRewrite guards the git/gh families. Returns false for anything not
// explicitly known to be read-only.
func safeToRewrite(cmd string) bool {
	fields := strings.Fields(cmd)
	if len(fields) < 2 {
		return false
	}
	switch fields[0] {
	case "git":
		switch fields[1] {
		case "stash":
			// Bare `git stash` stashes changes; pop/apply/drop change state.
			return len(fields) >= 3 && (fields[2] == "list" || fields[2] == "show")
		case "worktree":
			// add/remove/prune change state.
			return len(fields) >= 3 && fields[2] == "list"
		case "branch":
			// -d/-D/-m/-c or a bare name creates, renames, or deletes.
			return !gitBranchMutates(fields[2:])
		}
		return gitReadOnly[fields[1]]
	case "gh":
		if len(fields) < 3 {
			return false
		}
		return ghReadOnly[fields[1]+" "+fields[2]]
	}
	return true
}

// Exit codes consumed by the shell hook (mirrors rtk's protocol).
const (
	exitRewriteAllow = 0 // rewrite found, auto-allow
	exitNoMatch      = 1 // no rewrite rule matched, pass through
	exitDeny         = 2 // deny rule matched, pass through (host handles denial)
	exitRewriteAsk   = 3 // rewrite found, let host prompt user
)

var rewriteCmd = &cobra.Command{
	Use:    "rewrite <command>",
	Short:  "Rewrite a raw shell command to its yeet equivalent",
	Long:   "Used by the yeet-proxy.sh PreToolUse hook. Prints the rewritten command to stdout and exits with a code the hook uses to decide permission behavior.",
	Args:   cobra.ExactArgs(1),
	Hidden: true, // internal use — not shown in yeet --help
	RunE:   runRewrite,
}

func init() {
	rootCmd.AddCommand(rewriteCmd)
}

func runRewrite(cmd *cobra.Command, args []string) error {
	raw := args[0]

	// Skip heredocs — they can't be safely rewritten.
	if strings.Contains(raw, "<<") {
		os.Exit(exitNoMatch)
	}

	// Enforce reading ladder for known code files: bare `yeet read <file>` (no flags) → `yeet read -l aggressive`.
	// Gives signatures-only output (91% token reduction) in the SAME turn — no extra turn needed.
	// README, YAML, shell scripts, and other non-code files pass through unchanged.
	if strings.HasPrefix(raw, "yeet read ") {
		rest := strings.TrimPrefix(raw, "yeet read ")
		parts := strings.Fields(rest)
		hasFlags := false
		for _, p := range parts[1:] {
			if strings.HasPrefix(p, "-") {
				hasFlags = true
				break
			}
		}
		if !hasFlags && len(parts) == 1 && codeExts[strings.ToLower(filepath.Ext(parts[0]))] {
			fmt.Print("yeet read " + parts[0] + " -l aggressive")
			os.Exit(exitRewriteAllow)
		}
		os.Exit(exitNoMatch)
	}

	// Skip other commands that already use yeet.
	if strings.HasPrefix(raw, "yeet ") {
		os.Exit(exitNoMatch)
	}

	// Strip leading env var assignments (VAR=val VAR2=val2 cmd ...)
	// so "GIT_PAGER=cat grep foo" still matches the grep rule.
	stripped, envPrefix := stripEnvPrefix(raw)

	for _, rule := range rules {
		if strings.HasPrefix(stripped, rule.prefix) {
			// git/gh: never rewrite a subcommand that changes state.
			if (rule.prefix == "git " || rule.prefix == "gh ") && !safeToRewrite(stripped) {
				os.Exit(exitNoMatch)
			}
			rest := stripped[len(rule.prefix):]

			// Translate the arguments where the yeet command's shape differs
			// from the native one. Emitting a command that fails is worse than
			// not rewriting at all: the agent spends a turn on the error and
			// then retries, so a rule that cannot produce a valid command must
			// pass through instead.
			ok := true
			switch rule.prefix {
			case "cat ":
				rest, ok = translateCatArgs(rest)
			case "grep ":
				rest, ok = translateGrepArgs(rest)
			case "find ":
				rest, ok = translateFindArgs(rest)
			}
			if !ok {
				os.Exit(exitNoMatch)
			}

			rewritten := envPrefix + rule.yeetPrefix + rest
			fmt.Print(rewritten)
			os.Exit(exitRewriteAllow)
		}
		// Handle bare commands with no trailing space/args (e.g. "ls" alone).
		if strings.TrimSpace(stripped) == strings.TrimSpace(rule.prefix) {
			rewritten := envPrefix + strings.TrimSpace(rule.yeetPrefix)
			fmt.Print(rewritten)
			os.Exit(exitRewriteAllow)
		}
	}

	os.Exit(exitNoMatch)
	return nil
}

// stripEnvPrefix splits "KEY=val KEY2=val2 cmd args" into ("cmd args", "KEY=val KEY2=val2 ").
// Returns the original string unchanged if no env prefix is found.
func stripEnvPrefix(cmd string) (stripped string, prefix string) {
	parts := strings.Fields(cmd)
	i := 0
	for i < len(parts) && strings.Contains(parts[i], "=") && !strings.HasPrefix(parts[i], "-") {
		i++
	}
	if i == 0 {
		return cmd, ""
	}
	prefix = strings.Join(parts[:i], " ") + " "
	stripped = strings.Join(parts[i:], " ")
	return stripped, prefix
}

// ─── Argument translation ─────────────────────────────────────────────────────
// `yeet <cmd>` does not always take the same arguments as the native command it
// replaces. Where the shapes differ, translate; where a valid yeet command
// cannot be produced, return ok=false so the caller passes the command through
// untouched.

// translateCatArgs handles `cat`. `yeet read` takes exactly one file, so a
// multi-file cat cannot be expressed and is passed through.
func translateCatArgs(rest string) (string, bool) {
	fields := splitArgs(rest)
	files := 0
	for _, f := range fields {
		if !strings.HasPrefix(f, "-") {
			files++
		}
	}
	if files != 1 {
		return rest, false
	}
	return rest, true
}

// translateGrepArgs handles `grep`. `yeet grep` is always recursive and always
// prints line numbers, so -r/-R/--recursive and -n/--line-number are implied;
// passing them through makes yeet's flag parser reject the command. Flags yeet
// does not understand mean the command is passed through instead.
func translateGrepArgs(rest string) (string, bool) {
	fields := splitArgs(rest)
	var out []string
	for _, f := range fields {
		switch f {
		case "-r", "-R", "--recursive", "-n", "--line-number":
			continue // implied by yeet grep
		case "-rn", "-nr", "-rln", "-Rn", "-nR":
			continue // common clusters of the same two flags
		}
		// An unrecognised flag would be forwarded and rejected. Only let
		// through the ones yeet grep actually accepts.
		if strings.HasPrefix(f, "-") && !grepFlagOK(f) {
			return rest, false
		}
		out = append(out, shellQuote(f))
	}
	if len(out) == 0 {
		return rest, false
	}
	return strings.Join(out, " "), true
}

// grepFlagOK reports whether yeet grep accepts a flag it would be handed.
func grepFlagOK(f string) bool {
	base := f
	if i := strings.IndexByte(base, '='); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "-C", "--context", "--trim", "--type", "-v", "--verbose",
		"--max-results", "--max-line-len", "--max-per-file",
		"-i", "--ignore-case": // -i is understood by the underlying search
		return true
	}
	return false
}

// translateFindArgs handles `find`. Native form is `find <path> -name <pattern>`
// while yeet's is `yeet find <pattern> [path]`, so the two have to be swapped.
// Any other predicate (-type, -mtime, -exec, ...) has no yeet equivalent and is
// passed through.
func translateFindArgs(rest string) (string, bool) {
	fields := splitArgs(rest)
	var path, pattern string
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "-name", "-iname":
			if i+1 >= len(fields) {
				return rest, false
			}
			if pattern != "" {
				return rest, false // more than one pattern: not expressible
			}
			pattern = fields[i+1]
			i++
		default:
			if strings.HasPrefix(fields[i], "-") {
				return rest, false // an unsupported predicate
			}
			if path != "" {
				return rest, false // more than one search root
			}
			path = fields[i]
		}
	}
	if pattern == "" {
		return rest, false
	}
	if path == "" {
		return shellQuote(pattern), true
	}
	return shellQuote(pattern) + " " + shellQuote(path), true
}

// splitArgs splits a command tail on whitespace while keeping quoted runs
// together, so a quoted pattern with spaces stays one argument.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// shellQuote makes an argument survive re-parsing by the shell. Reassembling a
// command means the shell sees the result again: an unquoted glob such as *.md
// would be expanded before yeet ever runs, and a pattern with a trailing space
// would silently lose it.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '_', '-', '.', '/', '=', ':', '@', '+', ',':
			continue
		}
		safe = false
		break
	}
	if safe {
		return s
	}
	// Single quotes protect everything except a single quote itself, which is
	// closed, escaped, and reopened.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
