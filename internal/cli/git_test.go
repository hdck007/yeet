package cli

import (
	"strings"
	"testing"
)

// ─── status ───────────────────────────────────────────────────────────────────

func TestRenderGitStatus_GroupsByState(t *testing.T) {
	// Porcelain v1: first column is the index (staged), second the work tree.
	raw := strings.Join([]string{
		"## feat/x...origin/feat/x [ahead 2]",
		"M  staged.go",        // staged modification
		" M unstaged.go",      // unstaged modification
		"MM both.go",          // staged *and* unstaged
		"A  added.go",         // staged addition
		" D deleted.go",       // unstaged deletion
		"?? new.txt",          // untracked
		"UU conflict.go",      // conflict
		"AA both-added.go",    // conflict
		"R  old.go -> new.go", // rename
	}, "\n") + "\n"

	got := renderGitStatus(raw)

	if !strings.HasPrefix(got, "* feat/x...origin/feat/x [ahead 2]\n") {
		t.Errorf("branch line missing or wrong: %q", got)
	}
	for _, want := range []string{
		"conflicts (2):", "conflict.go", "both-added.go",
		"staged (", "staged.go", "added.go", "old.go -> new.go",
		"unstaged (", "unstaged.go", "deleted.go",
		"untracked (1):", "new.txt",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderGitStatus() missing %q\ngot:\n%s", want, got)
		}
	}
	// A file staged *and* modified has to appear on both sides, or the reader
	// cannot tell there is still work to stage.
	if strings.Count(got, "both.go") != 2 {
		t.Errorf("MM file should appear under staged and unstaged, got:\n%s", got)
	}
}

func TestRenderGitStatus_Clean(t *testing.T) {
	got := renderGitStatus("## main...origin/main\n")
	if !strings.Contains(got, "* main...origin/main") || !strings.Contains(got, "clean") {
		t.Errorf("clean status should name the branch and say clean, got %q", got)
	}
	// Nothing should be listed when there is nothing to list.
	for _, unwanted := range []string{"staged", "unstaged", "untracked", "conflicts"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("clean status should not mention %q, got %q", unwanted, got)
		}
	}
}

func TestRenderGitStatus_CapsLongLists(t *testing.T) {
	var lines []string
	lines = append(lines, "## main")
	for i := 0; i < 40; i++ {
		lines = append(lines, "?? file"+string(rune('a'+i%26))+string(rune('0'+i/26))+".txt")
	}
	got := renderGitStatus(strings.Join(lines, "\n") + "\n")
	if !strings.Contains(got, "untracked (40):") {
		t.Errorf("the true count must be reported even when the list is capped: %q", got)
	}
	if !strings.Contains(got, "more") {
		t.Errorf("a capped list must say how many were omitted: %q", got)
	}
}

// ─── numstat ──────────────────────────────────────────────────────────────────

func TestRenderGitNumstat(t *testing.T) {
	raw := "5\t2\tsmall.go\n100\t50\tbig.go\n-\t-\timage.png\n"
	got := renderGitNumstat(raw, "diff")

	if !strings.Contains(got, "diff: 3 files, +105 -52") {
		t.Errorf("totals wrong (binary files count as 0 lines): %q", got)
	}
	// Biggest change first — that is where a reader should look.
	if strings.Index(got, "big.go") > strings.Index(got, "small.go") {
		t.Errorf("files should be ordered by size of change, got:\n%s", got)
	}
	if !strings.Contains(got, "image.png") {
		t.Errorf("binary files must still be listed: %q", got)
	}
}

func TestRenderGitNumstat_Empty(t *testing.T) {
	if got := renderGitNumstat("", "diff"); got != "no changes\n" {
		t.Errorf("empty numstat = %q, want \"no changes\\n\"", got)
	}
}

func TestParseCount(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{{"0", 0}, {"5", 5}, {"123", 123}, {"-", 0}, {"", 0}, {"12x", 12}}
	for _, c := range cases {
		if got := parseCount(c.in); got != c.want {
			t.Errorf("parseCount(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// ─── diff --content ───────────────────────────────────────────────────────────

func TestRenderGitDiffContent_StripsNoiseKeepsLines(t *testing.T) {
	raw := strings.Join([]string{
		"diff --git a/x.go b/x.go",
		"index 1234567..89abcde 100644",
		"--- x.go",
		"+++ x.go",
		"@@ -1,3 +1,3 @@",
		"-old line",
		"+new line",
	}, "\n") + "\n"

	got := renderGitDiffContent(raw)

	if !strings.Contains(got, "-old line") || !strings.Contains(got, "+new line") {
		t.Errorf("changed lines must be kept: %q", got)
	}
	if !strings.Contains(got, "x.go") {
		t.Errorf("the file name must be kept: %q", got)
	}
	for _, noise := range []string{"index 1234567", "@@", "--- x.go", "+++ x.go"} {
		if strings.Contains(got, noise) {
			t.Errorf("%q is noise and should be stripped, got:\n%s", noise, got)
		}
	}
}

func TestRenderGitDiffContent_CapsOutput(t *testing.T) {
	// An uncapped renderer reproduces the diff in full, which defeats the point.
	var b strings.Builder
	b.WriteString("diff --git a/big.go b/big.go\n@@ -1 +1 @@\n")
	for i := 0; i < 500; i++ {
		b.WriteString("+line\n")
	}
	got := renderGitDiffContent(b.String())

	if !strings.Contains(got, "more lines in this file") {
		t.Errorf("a per-file cap must be reported: %q", got[:min(len(got), 300)])
	}
	if n := strings.Count(got, "+line"); n > diffMaxLinesPerFile+1 {
		t.Errorf("per-file cap not applied: %d lines kept, cap is %d", n, diffMaxLinesPerFile)
	}
	// The true totals must survive the capping.
	if !strings.Contains(got, "+500") {
		t.Errorf("the real change count must be reported even when capped: %q", got[:min(len(got), 200)])
	}
}

func TestRenderGitDiffContent_TruncatesLongLines(t *testing.T) {
	long := strings.Repeat("x", 500)
	raw := "diff --git a/x.go b/x.go\n@@ -1 +1 @@\n+" + long + "\n"
	got := renderGitDiffContent(raw)
	for _, line := range strings.Split(got, "\n") {
		if len(line) > 220 {
			t.Errorf("a single line of %d chars escaped truncation", len(line))
		}
	}
	if !strings.Contains(got, "...") {
		t.Error("a truncated line should be marked with an ellipsis")
	}
}

func TestRenderGitDiffContent_Empty(t *testing.T) {
	if got := renderGitDiffContent(""); got != "no changes\n" {
		t.Errorf("empty diff = %q, want \"no changes\\n\"", got)
	}
	// A diff header with no +/- lines is still no changes.
	if got := renderGitDiffContent("diff --git a/x b/x\nindex 1..2 100644\n"); got != "no changes\n" {
		t.Errorf("header-only diff = %q, want \"no changes\\n\"", got)
	}
}

// ─── log / show / branch ──────────────────────────────────────────────────────

func TestRenderGitLog(t *testing.T) {
	raw := "abc1234|Jane Doe|2 hours ago|fix: the thing\ndef5678|Bob|3 days ago|feat: other\n"
	got := renderGitLog(raw)
	if !strings.HasPrefix(got, "2 commits:\n") {
		t.Errorf("commit count missing: %q", got)
	}
	for _, want := range []string{"abc1234", "fix: the thing", "2 hours ago", "Jane"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderGitLog() missing %q: %q", want, got)
		}
	}
	// Only the first name — a surname adds bytes without adding identity.
	if strings.Contains(got, "Jane Doe") {
		t.Errorf("author should be shortened to the first name: %q", got)
	}
	if got := renderGitLog(""); got != "no commits\n" {
		t.Errorf("empty log = %q", got)
	}
}

func TestRenderGitShow(t *testing.T) {
	raw := "abc1234|Jane|1 day ago|fix: thing\n\n10\t5\tfile.go\n"
	got := renderGitShow(raw)
	for _, want := range []string{"abc1234", "fix: thing", "file.go", "+10", "-5"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderGitShow() missing %q:\n%s", want, got)
		}
	}
}

func TestRenderGitBranch(t *testing.T) {
	raw := "main|2 days ago|*\nfeat/x|1 hour ago|\n"
	got := renderGitBranch(raw)
	if !strings.Contains(got, "2 branches:") {
		t.Errorf("branch count missing: %q", got)
	}
	// The checked-out branch has to be distinguishable.
	if !strings.Contains(got, "* main") {
		t.Errorf("current branch should be marked: %q", got)
	}
	if !strings.Contains(got, "feat/x") || !strings.Contains(got, "1 hour ago") {
		t.Errorf("branch or date lost: %q", got)
	}
	if got := renderGitBranch(""); got != "no branches\n" {
		t.Errorf("empty branch list = %q", got)
	}
}

func TestShortName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Jane Doe", "Jane"},
		{"Jane", "Jane"},
		{"  Jane Doe  ", "Jane"},
		{"", ""},
	}
	for _, c := range cases {
		if got := shortName(c.in); got != c.want {
			t.Errorf("shortName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ─── push / pull / fetch ──────────────────────────────────────────────────────

func TestRenderGitPush(t *testing.T) {
	raw := strings.Join([]string{
		"Enumerating objects: 21, done.",
		"Counting objects: 100% (21/21), done.",
		"Writing objects: 100% (12/12), 4.21 KiB | 4.21 MiB/s, done.",
		"To github.com:owner/repo.git",
		"   abc1234..def5678  main -> main",
	}, "\n") + "\n"
	got := renderGitPush(raw)

	if !strings.Contains(got, "ok pushed") {
		t.Errorf("push should confirm success: %q", got)
	}
	if !strings.Contains(got, "github.com:owner/repo.git") {
		t.Errorf("the destination must be kept: %q", got)
	}
	if !strings.Contains(got, "main -> main") {
		t.Errorf("the ref update must be kept: %q", got)
	}
	// Progress counters are pure noise.
	for _, noise := range []string{"Enumerating", "Counting objects", "KiB"} {
		if strings.Contains(got, noise) {
			t.Errorf("%q should be stripped: %q", noise, got)
		}
	}
}

func TestRenderGitPush_UpToDate(t *testing.T) {
	if got := renderGitPush("Everything up-to-date\n"); !strings.Contains(got, "up-to-date") {
		t.Errorf("an up-to-date push should say so: %q", got)
	}
}

func TestRenderGitPull_KeepsConflictsInFull(t *testing.T) {
	// A conflict is the one case where the detail *is* the answer. Summarising
	// it would strip the file list the user needs in order to fix it.
	raw := strings.Join([]string{
		"Auto-merging file.go",
		"CONFLICT (content): Merge conflict in file.go",
		"Automatic merge failed; fix conflicts and then commit the result.",
	}, "\n") + "\n"
	got := renderGitPull(raw)
	if got != raw {
		t.Errorf("a conflicting pull must be returned unchanged.\ngot:\n%s\nwant:\n%s", got, raw)
	}
}

func TestRenderGitPull_Success(t *testing.T) {
	raw := strings.Join([]string{
		"remote: Enumerating objects: 5, done.",
		"Updating abc1234..def5678",
		"Fast-forward",
		" file.go | 10 +++++-----",
		" 1 file changed, 5 insertions(+), 5 deletions(-)",
	}, "\n") + "\n"
	got := renderGitPull(raw)
	if !strings.Contains(got, "ok pulled") {
		t.Errorf("pull should confirm: %q", got)
	}
	if !strings.Contains(got, "Fast-forward") {
		t.Errorf("the merge strategy is worth keeping: %q", got)
	}
	if !strings.Contains(got, "1 file changed") {
		t.Errorf("the file summary must be kept: %q", got)
	}
	if strings.Contains(got, "Enumerating") {
		t.Errorf("progress noise should be stripped: %q", got)
	}
}

func TestRenderGitPull_AlreadyUpToDate(t *testing.T) {
	if got := renderGitPull("Already up to date.\n"); !strings.Contains(got, "up-to-date") {
		t.Errorf("got %q", got)
	}
}

func TestRenderGitFetch(t *testing.T) {
	raw := strings.Join([]string{
		"remote: Enumerating objects: 10, done.",
		"From github.com:owner/repo",
		"   abc1234..def5678  main       -> origin/main",
		" * [new branch]      feat/x     -> origin/feat/x",
	}, "\n") + "\n"
	got := renderGitFetch(raw)
	if !strings.Contains(got, "ok fetched (2 refs)") {
		t.Errorf("fetch should count the refs that moved: %q", got)
	}
	if !strings.Contains(got, "origin/main") || !strings.Contains(got, "origin/feat/x") {
		t.Errorf("the moved refs must be listed: %q", got)
	}
	if got := renderGitFetch(""); !strings.Contains(got, "no new refs") {
		t.Errorf("a no-op fetch should say so: %q", got)
	}
}

// ─── dispatch ─────────────────────────────────────────────────────────────────

// planGit returning nil is what routes a command to real git untouched. Getting
// this wrong either breaks a command or silently summarises a mutation.
func TestPlanGit_ReadOnlyGetsAPlan(t *testing.T) {
	gitDiffContent = false
	for _, c := range []struct {
		sub  string
		rest []string
	}{
		{"status", nil},
		{"status", []string{"--short"}},
		{"diff", nil},
		{"diff", []string{"HEAD~1"}},
		{"log", []string{"-5"}},
		{"show", []string{"HEAD"}},
		{"branch", nil},
		{"branch", []string{"-a"}},
		{"stash", []string{"list"}},
		{"stash", []string{"show"}},
		{"worktree", []string{"list"}},
		{"push", nil},
		{"pull", nil},
		{"fetch", nil},
	} {
		if planGit(c.sub, c.rest) == nil {
			t.Errorf("planGit(%q, %v) = nil, want a plan", c.sub, c.rest)
		}
	}
}

func TestPlanGit_MutatingAndUnknownGetNoPlan(t *testing.T) {
	gitDiffContent = false
	for _, c := range []struct {
		sub  string
		rest []string
	}{
		{"commit", []string{"-m", "x"}},
		{"add", []string{"."}},
		{"reset", []string{"--hard"}},
		{"rebase", []string{"-i"}},
		{"merge", []string{"main"}},
		{"checkout", []string{"-b", "x"}},
		{"cherry-pick", []string{"abc"}},
		{"branch", []string{"-d", "old"}},
		{"branch", []string{"newbranch"}},
		{"stash", nil},
		{"stash", []string{"pop"}},
		{"stash", []string{"drop"}},
		{"worktree", []string{"add", "../wt"}},
		{"worktree", nil},
		{"bisect", []string{"start"}},
		{"tag", []string{"v1"}},
	} {
		if planGit(c.sub, c.rest) != nil {
			t.Errorf("planGit(%q, %v) returned a plan — must reach git untouched", c.sub, c.rest)
		}
	}
}

func TestPlanGit_DiffContentSwitchesRenderer(t *testing.T) {
	gitDiffContent = false
	summary := planGit("diff", nil)
	gitDiffContent = true
	content := planGit("diff", nil)
	gitDiffContent = false

	if summary == nil || content == nil {
		t.Fatal("both diff modes need a plan")
	}
	if strings.Join(summary.runArgs, " ") == strings.Join(content.runArgs, " ") {
		t.Error("--content must ask git for a different thing than the summary does")
	}
	if !strings.Contains(strings.Join(summary.runArgs, " "), "--numstat") {
		t.Errorf("summary mode should use --numstat, got %v", summary.runArgs)
	}
	if strings.Contains(strings.Join(content.runArgs, " "), "--numstat") {
		t.Errorf("content mode should not use --numstat, got %v", content.runArgs)
	}
}

// push/pull/fetch must never be run a second time to obtain a baseline.
func TestPlanGit_MutatingReadsHaveNoBaselineRerun(t *testing.T) {
	for _, sub := range []string{"push", "pull", "fetch"} {
		p := planGit(sub, nil)
		if p == nil {
			t.Fatalf("planGit(%q) = nil", sub)
		}
		if p.baselineArgs != nil {
			t.Errorf("planGit(%q).baselineArgs = %v, want nil — rerunning it would repeat the side effect",
				sub, p.baselineArgs)
		}
		if !p.combineErr {
			t.Errorf("planGit(%q) must fold in stderr, which is where git writes this", sub)
		}
	}
}

func TestGitBranchMutates(t *testing.T) {
	listing := [][]string{nil, {}, {"-a"}, {"--all"}, {"-r"}, {"--remotes"}, {"-v"}, {"--list"}}
	for _, r := range listing {
		if gitBranchMutates(r) {
			t.Errorf("gitBranchMutates(%v) = true, want false", r)
		}
	}
	mutating := [][]string{
		{"-d", "x"}, {"-D", "x"}, {"-m", "a", "b"}, {"-M", "a", "b"},
		{"-c", "a", "b"}, {"newname"}, {"--set-upstream-to=x"}, {"-u", "origin/x"},
	}
	for _, r := range mutating {
		if !gitBranchMutates(r) {
			t.Errorf("gitBranchMutates(%v) = false, want true", r)
		}
	}
}
