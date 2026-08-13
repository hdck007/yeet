package cli

import (
	"strings"
	"testing"
)

// A rewrite sends the command to yeet instead of the real binary. For git and
// gh that is only ever acceptable for read-only subcommands: rewriting
// `git push` or `gh pr merge` would route a state-changing command through a
// wrapper that was never built to perform it. These tests pin that boundary —
// when a new subcommand is added to gitReadOnly/ghReadOnly, it has to be a
// deliberate act, not a side effect of a prefix match.
func TestSafeToRewrite_ReadOnlyIsAllowed(t *testing.T) {
	readOnly := []string{
		"git status",
		"git status --short",
		"git diff",
		"git diff HEAD~1",
		"git log -5",
		"git log --oneline",
		"git show HEAD",
		"git branch",
		"git branch -a",
		"git stash list",
		"gh pr list",
		"gh pr view 29",
		"gh pr checks 29",
		"gh issue list",
		"gh issue view 5",
		"gh run list",
		"gh run view 123",
	}
	for _, cmd := range readOnly {
		if !safeToRewrite(cmd) {
			t.Errorf("safeToRewrite(%q) = false, want true (read-only command should be rewritten)", cmd)
		}
	}
}

func TestSafeToRewrite_MutatingIsRefused(t *testing.T) {
	// Every one of these changes state. A rewrite here is a bug with real
	// consequences, so they are enumerated explicitly rather than assumed.
	mutating := []string{
		"git commit -m 'x'",
		"git commit --amend",
		"git push origin main",
		"git push --force",
		"git pull",
		"git fetch",
		"git add .",
		"git rm file",
		"git mv a b",
		"git reset --hard",
		"git revert HEAD",
		"git rebase -i main",
		"git merge main",
		"git checkout -b foo",
		"git switch main",
		"git restore .",
		"git clean -fd",
		"git tag v1",
		"git cherry-pick abc",
		"git init",
		"git clone https://example.com/r.git",
		"git remote add origin x",
		"git config user.name x",
		"git apply patch.diff",
		"git worktree add ../wt",
		"git submodule update",
		"git gc",
		"git stash",         // bare stash stashes changes
		"git stash pop",     // mutates
		"git stash drop",    // destroys
		"git stash push -m", // mutates
		"gh pr create --title x",
		"gh pr merge 29",
		"gh pr close 29",
		"gh pr edit 29",
		"gh pr review 29",
		"gh pr comment 29",
		"gh pr checkout 29",
		"gh issue create",
		"gh issue close 5",
		"gh issue edit 5",
		"gh issue comment 5",
		"gh repo delete foo",
		"gh repo create foo",
		"gh repo clone foo",
		"gh run rerun 1",
		"gh run cancel 1",
		"gh run delete 1",
		"gh release create v1",
		"gh secret set FOO",
		"gh auth login",
		"gh api -X DELETE /repos/o/r",
		"gh workflow run deploy",
		"gh cache delete",
	}
	for _, cmd := range mutating {
		if safeToRewrite(cmd) {
			t.Errorf("safeToRewrite(%q) = true, want false — rewriting a state-changing command is unsafe", cmd)
		}
	}
}

func TestSafeToRewrite_MalformedInput(t *testing.T) {
	// Too few fields to classify: refuse rather than guess.
	for _, cmd := range []string{"", "git", "gh", "   ", "git ", "gh "} {
		if safeToRewrite(cmd) {
			t.Errorf("safeToRewrite(%q) = true, want false for an unclassifiable command", cmd)
		}
	}
	// A `gh` group with no subcommand cannot be judged safe.
	for _, cmd := range []string{"gh pr", "gh issue", "gh run"} {
		if safeToRewrite(cmd) {
			t.Errorf("safeToRewrite(%q) = true, want false (no subcommand to classify)", cmd)
		}
	}
}

// hasLongFlag decides whether `ls -la` is a fair baseline to measure a saving
// against. Getting it wrong reintroduces the inflated-savings bug.
func TestHasLongFlag(t *testing.T) {
	cases := []struct {
		flags []string
		want  bool
	}{
		{nil, false},
		{[]string{}, false},
		{[]string{"-R"}, false},
		{[]string{"-l"}, true},
		{[]string{"-la"}, true},
		{[]string{"-al"}, true},
		{[]string{"-R", "-l"}, true},
		{[]string{"--color"}, false},
		{[]string{"-1"}, false},
	}
	for _, c := range cases {
		if got := hasLongFlag(c.flags); got != c.want {
			t.Errorf("hasLongFlag(%v) = %v, want %v", c.flags, got, c.want)
		}
	}
}

// A rewrite that produces an invalid command is worse than no rewrite: the
// agent spends a turn on the error, then retries. These pin the argument
// translation for the forms that used to emit failing commands.
func TestTranslateGrepArgs(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		// -r/-n are implied by yeet grep; forwarding them made it reject the command.
		{"-rn 'func ' .", "'func ' .", true},
		{"-r foo .", "foo .", true},
		{"-n foo .", "foo .", true},
		{"--recursive foo .", "foo .", true},
		{"foo .", "foo .", true},
		// Flags yeet grep does accept survive.
		{"-C 2 foo .", "-C 2 foo .", true},
		{"-i foo .", "-i foo .", true},
		// A flag yeet grep does not understand: pass through untouched.
		{"-l foo .", "", false},
		{"--include=*.go foo .", "", false},
		{"-A 3 foo .", "", false},
		// Nothing left after stripping: pass through.
		{"-rn", "", false},
	}
	for _, c := range cases {
		got, ok := translateGrepArgs(c.in)
		if ok != c.wantOK {
			t.Errorf("translateGrepArgs(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if ok && got != c.want {
			t.Errorf("translateGrepArgs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTranslateFindArgs(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		// Native is `find <path> -name <pat>`; yeet is `yeet find <pat> [path]`.
		{". -name '*.md'", "'*.md' .", true},
		{"src -name '*.go'", "'*.go' src", true},
		{"-name '*.md'", "'*.md'", true},
		{". -iname '*.MD'", "'*.MD' .", true},
		// Predicates with no yeet equivalent must pass through.
		{". -type f", "", false},
		{". -name '*.go' -type f", "", false},
		{". -mtime -1", "", false},
		{". -name a -name b", "", false},
		{"a b -name x", "", false},
		{".", "", false},
		{"-name", "", false},
	}
	for _, c := range cases {
		got, ok := translateFindArgs(c.in)
		if ok != c.wantOK {
			t.Errorf("translateFindArgs(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if ok && got != c.want {
			t.Errorf("translateFindArgs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTranslateCatArgs(t *testing.T) {
	// `yeet read` takes exactly one file, so multi-file cat is not expressible.
	if _, ok := translateCatArgs("a.go b.go"); ok {
		t.Error("translateCatArgs(\"a.go b.go\") ok = true, want false (yeet read takes one file)")
	}
	if _, ok := translateCatArgs("a.go b.go c.go"); ok {
		t.Error("multi-file cat should pass through")
	}
	if _, ok := translateCatArgs("README.md"); !ok {
		t.Error("single-file cat should be rewritten")
	}
	if _, ok := translateCatArgs(""); ok {
		t.Error("empty cat args should pass through")
	}
}

// The rewritten command is re-parsed by the shell, so quoting has to survive.
// An unquoted glob would be expanded before yeet runs; a pattern with a
// trailing space would silently lose it and match different lines.
func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"with/slash.go", "with/slash.go"},
		{"-C", "-C"},
		{"", "''"},
		{"func ", "'func '"},
		{"*.md", "'*.md'"},
		{"a b", "'a b'"},
		{"$(rm -rf /)", "'$(rm -rf /)'"},
		{"semi;colon", "'semi;colon'"},
		{"it's", `'it'\''s'`},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitArgs_KeepsQuotedRunsTogether(t *testing.T) {
	got := splitArgs(`-rn 'func main' ./src`)
	want := []string{"-rn", "func main", "./src"}
	if len(got) != len(want) {
		t.Fatalf("splitArgs gave %d fields (%q), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitArgs field %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// Regression: `git branch -d foo` deletes a branch but was being rewritten
// because the guard only looked at the subcommand name. Execution was still
// correct — it fell through to real git — but the invariant is that a
// state-changing command is never rewritten at all.
func TestSafeToRewrite_BranchStashWorktreeArgs(t *testing.T) {
	safe := []string{
		"git branch", "git branch -a", "git branch --all", "git branch -r", "git branch -v",
		"git stash list", "git stash show", "git worktree list",
	}
	for _, c := range safe {
		if !safeToRewrite(c) {
			t.Errorf("safeToRewrite(%q) = false, want true", c)
		}
	}
	unsafe := []string{
		"git branch -d foo", "git branch -D foo", "git branch -m old new",
		"git branch -c a b", "git branch newbranch", "git branch --set-upstream-to=x",
		"git stash", "git stash pop", "git stash apply", "git stash drop", "git stash push -m x",
		"git worktree add ../wt", "git worktree remove ../wt", "git worktree prune",
	}
	for _, c := range unsafe {
		if safeToRewrite(c) {
			t.Errorf("safeToRewrite(%q) = true, want false — this changes state", c)
		}
	}
}

// shortRef trims ref plumbing so a run list reads like something a person wrote.
func TestShortRef(t *testing.T) {
	cases := []struct{ in, want string }{
		{"main", "main"},
		{"refs/pull/29/head", "pull/29"},
		{"refs/pull/1234/head", "pull/1234"},
		{"refs/heads/feat/x", "feat/x"},
		{"feat/install-uninstall", "feat/install-uninstall"},
		{"", ""},
	}
	for _, c := range cases {
		if got := shortRef(c.in); got != c.want {
			t.Errorf("shortRef(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// isHexish decides whether a leading token in a stash subject is the commit
// hash git put there (droppable) or the start of the message (must be kept).
func TestIsHexish(t *testing.T) {
	for _, s := range []string{"171c8e5", "ea9380e", "abc123", "0123456789abcdef"} {
		if !isHexish(s) {
			t.Errorf("isHexish(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "abc", "chore:", "make", "zzzzzzz", "Fix", "12345"} {
		if isHexish(s) {
			t.Errorf("isHexish(%q) = true, want false", s)
		}
	}
}

// The stash renderer drops git's "WIP on" boilerplate and the redundant hash,
// but must keep the branch — that is what tells you whether to pop it.
func TestRenderGitStash(t *testing.T) {
	raw := "stash@{0}|WIP on grep-drill-down: 171c8e5 chore: make grep faster\n" +
		"stash@{1}|WIP on main: ea9380e fix: thing\n"

	ultraCompact = false
	got := renderGitStash(raw)
	for _, want := range []string{"stash@{0}", "grep-drill-down", "chore: make grep faster", "stash@{1}", "main"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderGitStash() = %q, missing %q", got, want)
		}
	}
	for _, unwanted := range []string{"WIP on", "171c8e5", "ea9380e"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("renderGitStash() = %q, should not contain %q", got, unwanted)
		}
	}

	// -u drops the branch as well; the ref and message still have to survive.
	ultraCompact = true
	u := renderGitStash(raw)
	ultraCompact = false
	if strings.Contains(u, "grep-drill-down") {
		t.Errorf("ultra-compact stash should omit the branch, got %q", u)
	}
	if !strings.Contains(u, "chore: make grep faster") || !strings.Contains(u, "stash@{0}") {
		t.Errorf("ultra-compact stash lost the ref or message: %q", u)
	}
	if len(u) >= len(got) {
		t.Errorf("ultra-compact stash (%d) should be smaller than default (%d)", len(u), len(got))
	}

	if renderGitStash("") != "no stashes\n" {
		t.Error("empty stash list should say so")
	}
}

// A single worktree needs no count line and no indentation.
func TestRenderGitWorktree(t *testing.T) {
	one := "worktree /tmp/wt\nHEAD 1234567890abcdef\nbranch refs/heads/feat/x\n"
	got := renderGitWorktree(one)
	if strings.Contains(got, "worktrees:") {
		t.Errorf("single worktree should have no header, got %q", got)
	}
	if !strings.Contains(got, "feat/x") || !strings.Contains(got, "1234567") {
		t.Errorf("worktree lost the branch or hash: %q", got)
	}
	if strings.Contains(got, "1234567890") {
		t.Errorf("hash should be abbreviated to 7 chars, got %q", got)
	}

	two := one + "worktree /tmp/wt2\nHEAD abcdef1234567890\nbranch refs/heads/other\n"
	if g := renderGitWorktree(two); !strings.Contains(g, "2 worktrees:") {
		t.Errorf("two worktrees should be counted, got %q", g)
	}
	// A detached worktree has no branch line and must still render.
	det := "worktree /tmp/d\nHEAD 1234567890abcdef\ndetached\n"
	if g := renderGitWorktree(det); !strings.Contains(g, "detached") {
		t.Errorf("detached worktree should say so, got %q", g)
	}
	if renderGitWorktree("") != "no worktrees\n" {
		t.Error("no worktrees should say so")
	}
}

// git's global options come before the subcommand; missing them cost all
// compaction on `git -C path status` and `git --no-pager log`.
func TestSplitGitGlobals(t *testing.T) {
	cases := []struct {
		in              []string
		wantG, wantRest []string
	}{
		{[]string{"status"}, nil, []string{"status"}},
		{[]string{"-C", "/tmp", "status"}, []string{"-C", "/tmp"}, []string{"status"}},
		{[]string{"--no-pager", "log", "-5"}, []string{"--no-pager"}, []string{"log", "-5"}},
		{[]string{"-c", "core.pager=cat", "status"}, []string{"-c", "core.pager=cat"}, []string{"status"}},
		{[]string{"--git-dir=/tmp/.git", "status"}, []string{"--git-dir=/tmp/.git"}, []string{"status"}},
		{[]string{"-C", "/a", "--no-pager", "diff"}, []string{"-C", "/a", "--no-pager"}, []string{"diff"}},
		// A subcommand flag is not a global and must be left alone.
		{[]string{"log", "--oneline"}, nil, []string{"log", "--oneline"}},
		{[]string{"--unknown-flag", "status"}, nil, []string{"--unknown-flag", "status"}},
	}
	for _, c := range cases {
		g, rest := splitGitGlobals(c.in)
		if strings.Join(g, " ") != strings.Join(c.wantG, " ") {
			t.Errorf("splitGitGlobals(%v) globals = %v, want %v", c.in, g, c.wantG)
		}
		if strings.Join(rest, " ") != strings.Join(c.wantRest, " ") {
			t.Errorf("splitGitGlobals(%v) rest = %v, want %v", c.in, rest, c.wantRest)
		}
	}
}
