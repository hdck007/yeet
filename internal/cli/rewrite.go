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

// verdict is what the rewriter concluded about one command.
type verdict int

const (
	// vNever: leave this command alone. Either there is no yeet equivalent, or
	// routing it through yeet would change what it does.
	vNever verdict = iota
	// vAllow: rewrite and auto-allow. Reserved for commands that neither
	// change state nor execute project code, so skipping the permission prompt
	// costs the user nothing.
	vAllow
	// vAsk: rewrite, but let the host prompt as it would have anyway. The
	// output still gets filtered — the user just keeps the decision. Used for
	// anything that runs project code or mutates state.
	vAsk
)

// stronger reports the more cautious of two verdicts. A chain is only as
// auto-allowable as its least safe segment.
func (v verdict) stronger(o verdict) verdict {
	if v == vAsk || o == vAsk {
		return vAsk
	}
	if v == vAllow || o == vAllow {
		return vAllow
	}
	return vNever
}

// rewriteRule maps a raw command prefix to its yeet equivalent.
type rewriteRule struct {
	// prefix is the literal string the raw command must start with.
	prefix string
	// yeetPrefix replaces the matched prefix in the output.
	yeetPrefix string
	// guard classifies a specific invocation. nil means the whole family is
	// read-only and safe to auto-allow.
	guard func(fields []string) verdict
	// translate reshapes the argument tail where yeet's command shape differs
	// from the native one. Returning ok=false passes the command through:
	// emitting something that fails is worse than not rewriting, because the
	// agent spends a turn on the error and then retries.
	translate func(rest string) (string, bool)
}

// rules is the single source of truth for all rewrite mappings.
// To add a new rewrite, add an entry here — do not touch the hook script.
var rules = []rewriteRule{
	{prefix: "cat ", yeetPrefix: "yeet read ", translate: translateCatArgs},
	{prefix: "grep ", yeetPrefix: "yeet grep ", translate: translateGrepArgs},
	{prefix: "ls ", yeetPrefix: "yeet ls "},
	{prefix: "find ", yeetPrefix: "yeet find ", translate: translateFindArgs},
	{prefix: "diff ", yeetPrefix: "yeet diff "},

	// git and gh are among the noisiest commands an agent runs. Only the
	// read-only subcommands are rewritten — see gitReadOnly / ghReadOnly below;
	// anything that mutates state must reach the real binary untouched.
	{prefix: "git ", yeetPrefix: "yeet git ", guard: guardGit},
	{prefix: "gh ", yeetPrefix: "yeet gh ", guard: guardGH},

	// Process and disk listings. Both are read-only and both are pathological:
	// `ps aux` on a dev machine is several hundred lines of mostly-identical
	// helper processes, and `du` walking node_modules is unbounded.
	{prefix: "ps ", yeetPrefix: "yeet ps "},
	{prefix: "du ", yeetPrefix: "yeet du "},

	// Cluster and container inspection. Read-only subcommands only — the same
	// boundary as git/gh, for the same reason.
	{prefix: "kubectl ", yeetPrefix: "yeet kubectl ", guard: guardKubectl},
	{prefix: "docker ", yeetPrefix: "yeet docker ", guard: guardDocker},

	// Test runners, compilers and linters. These execute project code, so they
	// are rewritten as vAsk: the user keeps the prompt they get today and the
	// output arrives filtered instead of raw.
	{prefix: "npx vitest ", yeetPrefix: "yeet vitest ", guard: guardRunsCode},
	{prefix: "vitest ", yeetPrefix: "yeet vitest ", guard: guardRunsCode},
	{prefix: "npx tsc ", yeetPrefix: "yeet tsc ", guard: guardRunsCode},
	{prefix: "tsc ", yeetPrefix: "yeet tsc ", guard: guardRunsCode},
	{prefix: "npx eslint ", yeetPrefix: "yeet lint ", guard: guardRunsCode},
	{prefix: "eslint ", yeetPrefix: "yeet lint ", guard: guardRunsCode},
	{prefix: "npx playwright ", yeetPrefix: "yeet playwright ", guard: guardRunsCode},
	{prefix: "playwright ", yeetPrefix: "yeet playwright ", guard: guardRunsCode},
	{prefix: "npx prettier ", yeetPrefix: "yeet prettier ", guard: guardRunsCode},
	{prefix: "prettier ", yeetPrefix: "yeet prettier ", guard: guardRunsCode},
	{prefix: "npx prisma ", yeetPrefix: "yeet prisma ", guard: guardRunsCode},
	{prefix: "prisma ", yeetPrefix: "yeet prisma ", guard: guardRunsCode},
	// `yeet next` renders a build and nothing else, so `next dev` and
	// `next start` must reach the real binary — routing them here would run a
	// build instead of the thing that was asked for.
	{prefix: "next ", yeetPrefix: "yeet next ", guard: guardNextBuild},

	// Package managers. `npm install` is one of the largest single outputs an
	// agent ever sees, and it is worth filtering even though installing is a
	// mutation — hence vAsk for everything but the query subcommands.
	{prefix: "npm ", yeetPrefix: "yeet npm ", guard: guardPackageManager},
	{prefix: "pnpm ", yeetPrefix: "yeet pnpm ", guard: guardPackageManager},
	{prefix: "yarn ", yeetPrefix: "yeet yarn ", guard: guardPackageManager},
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

// kubectlReadOnly lists the kubectl verbs that only look. `exec`, `apply`,
// `delete`, `edit`, `scale`, `rollout`, `drain`, `port-forward` and friends are
// absent on purpose: yeet runs the real binary, so a rewrite of those would
// reach a live cluster through a wrapper built only to reformat output.
var kubectlReadOnly = map[string]bool{
	"get":           true,
	"describe":      true,
	"logs":          true,
	"top":           true,
	"events":        true,
	"explain":       true,
	"api-resources": true,
	"api-versions":  true,
	"cluster-info":  true,
	"version":       true,
}

// dockerReadOnly lists the docker subcommands that only look, keyed by verb or
// by "<group> <sub>" where docker uses one.
var dockerReadOnly = map[string]bool{
	"ps":           true,
	"images":       true,
	"logs":         true,
	"inspect":      true,
	"version":      true,
	"info":         true,
	"stats":        true,
	"top":          true,
	"port":         true,
	"history":      true,
	"image ls":     true,
	"container ls": true,
	"volume ls":    true,
	"network ls":   true,
	"system df":    true,
	"compose ps":   true,
	"compose logs": true,
}

// pkgManagerReadOnly lists the package-manager subcommands that only report.
// Everything else — install, add, remove, publish, run, test — either mutates
// the tree or executes a script from package.json, so it stays vAsk.
var pkgManagerReadOnly = map[string]bool{
	"ls": true, "list": true, "outdated": true, "audit": true,
	"why": true, "explain": true, "view": true, "info": true,
	"show": true, "ping": true, "doctor": true, "licenses": true,
}

// guardGit / guardGH keep the git and gh families to their read-only halves.
func guardGit(fields []string) verdict {
	if len(fields) < 2 {
		return vNever
	}
	switch fields[1] {
	case "stash":
		// Bare `git stash` stashes changes; pop/apply/drop change state.
		if len(fields) >= 3 && (fields[2] == "list" || fields[2] == "show") {
			return vAllow
		}
		return vNever
	case "worktree":
		// add/remove/prune change state.
		if len(fields) >= 3 && fields[2] == "list" {
			return vAllow
		}
		return vNever
	case "branch":
		// -d/-D/-m/-c or a bare name creates, renames, or deletes.
		if gitBranchMutates(fields[2:]) {
			return vNever
		}
		return vAllow
	}
	if gitReadOnly[fields[1]] {
		return vAllow
	}
	return vNever
}

func guardGH(fields []string) verdict {
	if len(fields) < 3 {
		return vNever
	}
	if ghReadOnly[fields[1]+" "+fields[2]] {
		return vAllow
	}
	return vNever
}

func guardKubectl(fields []string) verdict {
	if len(fields) < 2 {
		return vNever
	}
	if fields[1] == "config" {
		// `config view` and `config get-contexts` read; `use-context` and
		// `set-*` write to the kubeconfig.
		if len(fields) >= 3 && (fields[2] == "view" || fields[2] == "get-contexts" || fields[2] == "current-context") {
			return vAllow
		}
		return vNever
	}
	if kubectlReadOnly[fields[1]] {
		return vAllow
	}
	return vNever
}

func guardDocker(fields []string) verdict {
	if len(fields) < 2 {
		return vNever
	}
	if len(fields) >= 3 && dockerReadOnly[fields[1]+" "+fields[2]] {
		return vAllow
	}
	if dockerReadOnly[fields[1]] {
		return vAllow
	}
	return vNever
}

// guardRunsCode covers the tools that compile, lint, or execute the project.
// None of them are state-free, so none of them are auto-allowed — but all of
// them are worth filtering.
func guardRunsCode(fields []string) verdict {
	if len(fields) < 1 {
		return vNever
	}
	return vAsk
}

// guardNextBuild limits the next family to the one subcommand the renderer
// understands.
func guardNextBuild(fields []string) verdict {
	if len(fields) < 2 || fields[1] != "build" {
		return vNever
	}
	return vAsk
}

func guardPackageManager(fields []string) verdict {
	if len(fields) < 2 {
		return vNever
	}
	if pkgManagerReadOnly[fields[1]] {
		return vAllow
	}
	if fields[1] == "config" {
		if len(fields) >= 3 && fields[2] == "get" {
			return vAllow
		}
		return vAsk
	}
	return vAsk
}

// safeToRewrite guards the git/gh families. Returns false for anything not
// explicitly known to be read-only. Kept as the named boundary those families
// are tested against.
func safeToRewrite(cmd string) bool {
	fields := strings.Fields(cmd)
	if len(fields) < 2 {
		return false
	}
	switch fields[0] {
	case "git":
		return guardGit(fields) != vNever
	case "gh":
		return guardGH(fields) != vNever
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
	out, v := rewriteCommand(args[0])
	switch v {
	case vAllow:
		fmt.Print(out)
		os.Exit(exitRewriteAllow)
	case vAsk:
		fmt.Print(out)
		os.Exit(exitRewriteAsk)
	}
	os.Exit(exitNoMatch)
	return nil
}

// rewriteCommand is the whole decision, as a pure function: it returns the
// command to run and how the host should treat it. runRewrite is only the
// exit-code shim around it, which is what makes this testable.
func rewriteCommand(raw string) (string, verdict) {
	// Heredocs carry a body that is not shell at all; splitting on operators
	// inside it would corrupt the payload.
	if strings.Contains(raw, "<<") {
		return raw, vNever
	}

	parts, ok := splitChain(raw)
	if !ok {
		return raw, vNever
	}

	// A chain is auto-allowable only if every one of its segments is. Segments
	// yeet leaves alone still count: an untouched `rm -rf build` sitting after
	// a rewritten `cat` must not inherit the cat's auto-allow.
	worst := vAllow
	changed := false

	for i := range parts {
		seg := parts[i].cmd
		if seg == "" {
			continue
		}

		if !segmentRewritable(parts, i) {
			worst = worst.stronger(passthroughVerdict(seg))
			continue
		}

		out, v := rewriteSegment(seg)
		switch v {
		case vNever:
			worst = worst.stronger(passthroughVerdict(seg))
		default:
			parts[i].cmd = out
			changed = true
			worst = worst.stronger(v)
		}
	}

	if !changed {
		return raw, vNever
	}
	return joinChain(parts), worst
}

// passthroughVerdict classifies a segment yeet is not rewriting, purely for the
// permission decision. A yeet command or a verb that cannot affect anything but
// its own stdout keeps the chain auto-allowable; anything else means the user
// has to see the command, which is what they would have got without yeet.
func passthroughVerdict(seg string) verdict {
	stripped, _ := stripEnvPrefix(seg)
	fields := strings.Fields(stripped)
	if len(fields) == 0 {
		return vAllow
	}
	if fields[0] == "yeet" {
		return vAllow
	}
	if benignVerbs[fields[0]] {
		return vAllow
	}
	return vAsk
}

// rewriteSegment applies the rule table to a single command with no operators
// in it.
func rewriteSegment(seg string) (string, verdict) {
	// Enforce the reading ladder for known code files: bare `yeet read <file>`
	// (no flags) → `yeet read -l aggressive`. That gives signatures-only output
	// in the SAME turn — no extra turn needed. README, YAML, shell scripts and
	// other non-code files pass through unchanged.
	if rest, isRead := strings.CutPrefix(seg, "yeet read "); isRead {
		parts := strings.Fields(rest)
		if len(parts) == 1 && codeExts[strings.ToLower(filepath.Ext(parts[0]))] {
			return "yeet read " + parts[0] + " -l aggressive", vAllow
		}
		return seg, vNever
	}

	// Anything else already using yeet is left alone.
	if seg == "yeet" || strings.HasPrefix(seg, "yeet ") {
		return seg, vNever
	}

	// Strip leading env var assignments (VAR=val VAR2=val2 cmd ...)
	// so "GIT_PAGER=cat grep foo" still matches the grep rule.
	stripped, envPrefix := stripEnvPrefix(seg)

	// A trailing `2>/dev/null` is not an argument. Leaving it in the tail would
	// send it through argument translation, which shell-quotes what it does not
	// recognise — turning the redirect into a filename to search for.
	stripped, redir := splitRedirects(stripped)
	if redir != "" {
		redir = " " + redir
	}

	for _, rule := range rules {
		matched := strings.HasPrefix(stripped, rule.prefix)
		bare := strings.TrimSpace(stripped) == strings.TrimSpace(rule.prefix)
		if !matched && !bare {
			continue
		}

		if rule.guard != nil {
			if v := rule.guard(strings.Fields(stripped)); v == vNever {
				// A guarded family that refuses this invocation must not fall
				// through to a shorter prefix in the table.
				return seg, vNever
			} else if bare {
				return envPrefix + strings.TrimSpace(rule.yeetPrefix) + redir, v
			} else {
				rest := stripped[len(rule.prefix):]
				if rule.translate != nil {
					var ok bool
					if rest, ok = rule.translate(rest); !ok {
						return seg, vNever
					}
				}
				return envPrefix + rule.yeetPrefix + rest + redir, v
			}
		}

		if bare {
			return envPrefix + strings.TrimSpace(rule.yeetPrefix) + redir, vAllow
		}
		rest := stripped[len(rule.prefix):]
		if rule.translate != nil {
			var ok bool
			if rest, ok = rule.translate(rest); !ok {
				return seg, vNever
			}
		}
		return envPrefix + rule.yeetPrefix + rest + redir, vAllow
	}

	return seg, vNever
}

// splitRedirects separates a command from its trailing redirection operators.
// Only stderr redirections reach this point — segmentRewritable has already
// refused anything that moves stdout — but the scan is written generally so a
// future relaxation there cannot silently start quoting redirects as arguments.
func splitRedirects(cmd string) (body, redir string) {
	var quote byte
	tokenStart := true
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			tokenStart = false
			continue
		case ' ', '\t':
			tokenStart = true
			continue
		}
		if tokenStart && isRedirStart(cmd[i:]) {
			return strings.TrimRight(cmd[:i], " \t"), cmd[i:]
		}
		tokenStart = false
	}
	return cmd, ""
}

// isRedirStart reports whether a token begins a redirection: `>`, `>>`, `<`,
// `&>`, or a file descriptor followed by one of those (`2>`, `2>>`, `1>`).
func isRedirStart(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '>' || s[0] == '<' {
		return true
	}
	if strings.HasPrefix(s, "&>") {
		return true
	}
	for j := 0; j < len(s); j++ {
		if s[j] == '>' || s[j] == '<' {
			return j > 0 // digits then a redirect operator
		}
		if s[j] < '0' || s[j] > '9' {
			return false
		}
	}
	return false
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
