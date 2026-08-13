package cli

import (
	"strings"
	"testing"
)

// Every renderer below is fed the same JSON shape gh returns for its --json
// flag. A renderer that fails to parse must return an error so the caller can
// fall back to gh's own output rather than printing a broken summary.

func TestRenderPRList(t *testing.T) {
	in := []byte(`[
	  {"number":29,"title":"Fix things","author":{"login":"alice"},"headRefName":"feat/x",
	   "isDraft":false,"reviewDecision":"APPROVED","updatedAt":"2026-08-13T10:00:00Z"},
	  {"number":30,"title":"WIP","author":{"login":"bob"},"headRefName":"feat/y",
	   "isDraft":true,"reviewDecision":"CHANGES_REQUESTED","updatedAt":"2026-08-13T09:00:00Z"}
	]`)
	got, err := renderPRList(in)
	if err != nil {
		t.Fatalf("renderPRList: %v", err)
	}
	for _, want := range []string{"2 PRs:", "#29", "Fix things", "alice", "approved", "#30", "draft", "changes-requested"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderPRList() missing %q:\n%s", want, got)
		}
	}
}

func TestRenderPRList_Empty(t *testing.T) {
	got, err := renderPRList([]byte(`[]`))
	if err != nil {
		t.Fatalf("renderPRList: %v", err)
	}
	if !strings.Contains(got, "no open PRs") {
		t.Errorf("an empty list should say so, got %q", got)
	}
}

func TestRenderPRView(t *testing.T) {
	in := []byte(`{"number":29,"title":"Fix things","author":{"login":"alice"},"state":"OPEN",
	  "isDraft":false,"reviewDecision":"APPROVED","mergeable":"MERGEABLE",
	  "headRefName":"feat/x","baseRefName":"main","additions":100,"deletions":20,
	  "changedFiles":5,"url":"https://github.com/o/r/pull/29"}`)
	got, err := renderPRView(in)
	if err != nil {
		t.Fatalf("renderPRView: %v", err)
	}
	// Everything an agent needs to decide what to do next.
	for _, want := range []string{
		"#29", "Fix things", "open", "alice", "feat/x", "main",
		"5 files", "+100", "-20", "mergeable", "approved",
		"https://github.com/o/r/pull/29",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderPRView() missing %q:\n%s", want, got)
		}
	}
}

func TestRenderChecks_OnlyFailuresGetDetail(t *testing.T) {
	in := []byte(`[
	  {"name":"build","state":"SUCCESS","bucket":"pass","link":"https://ci/1"},
	  {"name":"test","state":"SUCCESS","bucket":"pass","link":"https://ci/2"},
	  {"name":"lint","state":"FAILURE","bucket":"fail","link":"https://ci/3"},
	  {"name":"e2e","state":"PENDING","bucket":"pending","link":""}
	]`)
	got, err := renderChecks(in)
	if err != nil {
		t.Fatalf("renderChecks: %v", err)
	}
	if !strings.Contains(got, "4 checks") {
		t.Errorf("total count missing: %q", got)
	}
	if !strings.Contains(got, "2 pass") || !strings.Contains(got, "1 fail") || !strings.Contains(got, "1 pending") {
		t.Errorf("per-bucket counts missing: %q", got)
	}
	// The failing check earns a line with its link; the passing ones do not.
	if !strings.Contains(got, "lint") || !strings.Contains(got, "https://ci/3") {
		t.Errorf("the failing check needs its name and link: %q", got)
	}
	if strings.Contains(got, "https://ci/1") {
		t.Errorf("a passing check should not spend a line on its link: %q", got)
	}
}

func TestRenderChecks_Empty(t *testing.T) {
	got, err := renderChecks([]byte(`[]`))
	if err != nil {
		t.Fatalf("renderChecks: %v", err)
	}
	if !strings.Contains(got, "no checks") {
		t.Errorf("got %q", got)
	}
}

func TestRenderIssueList(t *testing.T) {
	in := []byte(`[{"number":5,"title":"Bug","author":{"login":"carol"},
	  "labels":[{"name":"bug"},{"name":"p1"}],"updatedAt":"2026-08-13T10:00:00Z"}]`)
	got, err := renderIssueList(in)
	if err != nil {
		t.Fatalf("renderIssueList: %v", err)
	}
	for _, want := range []string{"1 issues:", "#5", "Bug", "carol", "bug", "p1"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderIssueList() missing %q: %q", want, got)
		}
	}
	got, _ = renderIssueList([]byte(`[]`))
	if !strings.Contains(got, "no open issues") {
		t.Errorf("empty issue list: %q", got)
	}
}

func TestRenderIssueView(t *testing.T) {
	in := []byte(`{"number":5,"title":"Bug","author":{"login":"carol"},"state":"OPEN",
	  "labels":[{"name":"bug"}],"url":"https://github.com/o/r/issues/5"}`)
	got, err := renderIssueView(in)
	if err != nil {
		t.Fatalf("renderIssueView: %v", err)
	}
	for _, want := range []string{"#5", "Bug", "open", "carol", "bug", "issues/5"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderIssueView() missing %q: %q", want, got)
		}
	}
}

func TestRenderRunList_CollapsesRepeatedBranches(t *testing.T) {
	in := []byte(`[
	  {"databaseId":1,"name":"CI","status":"completed","conclusion":"success","headBranch":"main","event":"push","createdAt":"2026-08-13T10:00:00Z"},
	  {"databaseId":2,"name":"CI","status":"completed","conclusion":"success","headBranch":"main","event":"push","createdAt":"2026-08-13T09:00:00Z"},
	  {"databaseId":3,"name":"CI","status":"completed","conclusion":"failure","headBranch":"refs/pull/29/head","event":"pull_request","createdAt":"2026-08-13T08:00:00Z"}
	]`)
	ultraCompact = false
	got, err := renderRunList(in)
	if err != nil {
		t.Fatalf("renderRunList: %v", err)
	}
	// Run IDs are how you act on a run — they must all survive.
	for _, want := range []string{"3 runs:", "1", "2", "3", "FAIL"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderRunList() missing %q:\n%s", want, got)
		}
	}
	// The branch appears when it changes, not on every row.
	if n := strings.Count(got, "(main)"); n != 1 {
		t.Errorf("a repeated branch should be printed once, appeared %d times:\n%s", n, got)
	}
	// Ref plumbing is trimmed to something readable.
	if !strings.Contains(got, "pull/29") {
		t.Errorf("refs/pull/29/head should render as pull/29:\n%s", got)
	}
	if strings.Contains(got, "refs/pull") {
		t.Errorf("ref plumbing should be trimmed:\n%s", got)
	}
}

func TestRenderRunList_UltraCompactDropsBranch(t *testing.T) {
	in := []byte(`[{"databaseId":1,"name":"CI","status":"completed","conclusion":"success","headBranch":"main","event":"push","createdAt":""}]`)
	ultraCompact = false
	full, _ := renderRunList(in)
	ultraCompact = true
	ultra, _ := renderRunList(in)
	ultraCompact = false

	if strings.Contains(ultra, "main") {
		t.Errorf("-u should drop the branch, got %q", ultra)
	}
	if !strings.Contains(ultra, "1") || !strings.Contains(ultra, "CI") {
		t.Errorf("-u must keep the run id and workflow name, got %q", ultra)
	}
	if len(ultra) >= len(full) {
		t.Errorf("-u (%d) should be smaller than the default (%d)", len(ultra), len(full))
	}
}

func TestRenderRunList_InProgressUsesStatus(t *testing.T) {
	// A running job has no conclusion yet; the status has to stand in for it.
	in := []byte(`[{"databaseId":9,"name":"CI","status":"in_progress","conclusion":"","headBranch":"main","event":"push","createdAt":""}]`)
	got, err := renderRunList(in)
	if err != nil {
		t.Fatalf("renderRunList: %v", err)
	}
	if !strings.Contains(got, "run") {
		t.Errorf("an in-progress run should be marked as running: %q", got)
	}
}

func TestRenderRunView_OnlyFailingStepsListed(t *testing.T) {
	in := []byte(`{"databaseId":42,"name":"CI","status":"completed","conclusion":"failure","headBranch":"main",
	  "jobs":[
	    {"name":"build","status":"completed","conclusion":"success","steps":[{"name":"compile","conclusion":"success"}]},
	    {"name":"test","status":"completed","conclusion":"failure","steps":[
	      {"name":"unit","conclusion":"success"},{"name":"integration","conclusion":"failure"}]}
	  ]}`)
	got, err := renderRunView(in)
	if err != nil {
		t.Fatalf("renderRunView: %v", err)
	}
	for _, want := range []string{"42", "failure", "build", "test", "integration"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderRunView() missing %q:\n%s", want, got)
		}
	}
	// A green job needs no step-by-step breakdown.
	if strings.Contains(got, "compile") {
		t.Errorf("steps of a passing job should not be listed:\n%s", got)
	}
	if strings.Contains(got, "unit") {
		t.Errorf("passing steps of a failing job should not be listed:\n%s", got)
	}
}

func TestRenderRepoView(t *testing.T) {
	in := []byte(`{"nameWithOwner":"o/r","description":"a thing","isPrivate":false,"isArchived":false,
	  "defaultBranchRef":{"name":"main"},"stargazerCount":2,"forkCount":1,
	  "primaryLanguage":{"name":"Go"},"url":"https://github.com/o/r"}`)
	ultraCompact = false
	got, err := renderRepoView(in)
	if err != nil {
		t.Fatalf("renderRepoView: %v", err)
	}
	for _, want := range []string{"o/r", "public", "a thing", "@main", "2 stars", "1 forks", "https://github.com/o/r"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderRepoView() missing %q:\n%s", want, got)
		}
	}
	// The language is visible from the files; it does not earn a line here.
	if strings.Contains(got, "Go") {
		t.Errorf("primary language should be omitted:\n%s", got)
	}

	// -u drops the default branch too.
	ultraCompact = true
	ultra, _ := renderRepoView(in)
	ultraCompact = false
	if strings.Contains(ultra, "@main") {
		t.Errorf("-u should drop the default branch: %q", ultra)
	}
	if len(ultra) >= len(got) {
		t.Errorf("-u (%d) should be smaller than default (%d)", len(ultra), len(got))
	}
}

func TestRenderRepoView_PrivateAndArchived(t *testing.T) {
	in := []byte(`{"nameWithOwner":"o/r","isPrivate":true,"isArchived":true,
	  "defaultBranchRef":{"name":"main"},"stargazerCount":0,"forkCount":0}`)
	got, err := renderRepoView(in)
	if err != nil {
		t.Fatalf("renderRepoView: %v", err)
	}
	if !strings.Contains(got, "private") || !strings.Contains(got, "archived") {
		t.Errorf("private and archived must both be surfaced: %q", got)
	}
}

func TestRenderRepoList(t *testing.T) {
	in := []byte(`[{"nameWithOwner":"o/a","description":"x","isPrivate":true,"isArchived":false,
	  "stargazerCount":1,"primaryLanguage":{"name":"Go"}}]`)
	got, err := renderRepoList(in)
	if err != nil {
		t.Fatalf("renderRepoList: %v", err)
	}
	for _, want := range []string{"1 repos:", "o/a", "private"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderRepoList() missing %q: %q", want, got)
		}
	}
	got, _ = renderRepoList([]byte(`[]`))
	if !strings.Contains(got, "no repos") {
		t.Errorf("empty repo list: %q", got)
	}
}

// Malformed JSON must produce an error, not a half-rendered summary — the caller
// falls back to gh's own output on error.
func TestGhRenderers_RejectMalformedJSON(t *testing.T) {
	bad := []byte(`{not json`)
	renderers := map[string]func([]byte) (string, error){
		"renderPRList":    renderPRList,
		"renderPRView":    renderPRView,
		"renderChecks":    renderChecks,
		"renderIssueList": renderIssueList,
		"renderIssueView": renderIssueView,
		"renderRunList":   renderRunList,
		"renderRunView":   renderRunView,
		"renderRepoView":  renderRepoView,
		"renderRepoList":  renderRepoList,
	}
	for name, fn := range renderers {
		if _, err := fn(bad); err == nil {
			t.Errorf("%s accepted malformed JSON — the caller would print a broken summary", name)
		}
	}
}

func TestRunMarker(t *testing.T) {
	cases := []struct{ in, want string }{
		{"success", "ok"},
		{"failure", "FAIL"},
		{"cancelled", "cxl"},
		{"skipped", "skip"},
		{"in_progress", "run"},
		{"queued", "wait"},
		{"waiting", "wait"},
		// Anything unexpected keeps its own name rather than being flattened.
		{"neutral", "neutral"},
		{"timed_out", "timed_out"},
	}
	for _, c := range cases {
		if got := runMarker(c.in); got != c.want {
			t.Errorf("runMarker(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShortReview(t *testing.T) {
	cases := []struct{ in, want string }{
		{"APPROVED", "approved"},
		{"CHANGES_REQUESTED", "changes-requested"},
		{"REVIEW_REQUIRED", "review-required"},
		{"", ""},
		{"SOMETHING_ELSE", ""},
	}
	for _, c := range cases {
		if got := shortReview(c.in); got != c.want {
			t.Errorf("shortReview(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// planGh decides what gets compacted. Returning nil sends the command to gh
// untouched, which is what keeps mutations safe.
func TestPlanGh_ReadOnlyGetsAPlan(t *testing.T) {
	for _, args := range [][]string{
		{"pr", "list"}, {"pr", "view", "29"}, {"pr", "checks", "29"},
		{"issue", "list"}, {"issue", "view", "5"},
		{"run", "list"}, {"run", "view", "1"},
		{"repo", "view"}, {"repo", "list"},
	} {
		if planGh(args) == nil {
			t.Errorf("planGh(%v) = nil, want a plan", args)
		}
	}
}

func TestPlanGh_MutatingAndUnknownGetNoPlan(t *testing.T) {
	for _, args := range [][]string{
		{"pr", "create"}, {"pr", "merge", "29"}, {"pr", "close", "29"},
		{"pr", "edit", "29"}, {"pr", "review", "29"}, {"pr", "comment", "29"},
		{"pr", "checkout", "29"},
		{"issue", "create"}, {"issue", "close", "5"}, {"issue", "edit", "5"},
		{"run", "rerun", "1"}, {"run", "cancel", "1"}, {"run", "delete", "1"},
		{"repo", "delete", "x"}, {"repo", "create", "x"}, {"repo", "clone", "x"},
		{"release", "create"}, {"secret", "set", "X"}, {"auth", "login"},
		{"api", "/repos"}, {"workflow", "run"},
		{"pr"}, {"issue"}, {"run"}, {"repo"}, {},
	} {
		if planGh(args) != nil {
			t.Errorf("planGh(%v) returned a plan — must reach gh untouched", args)
		}
	}
}

// The JSON request has to actually ask for every field the renderer reads,
// otherwise the summary silently comes out blank.
func TestPlanGh_JSONFieldsCoverWhatRenderersRead(t *testing.T) {
	cases := []struct {
		args   []string
		fields []string
	}{
		{[]string{"pr", "view", "1"}, []string{"number", "title", "author", "state", "additions", "deletions", "changedFiles", "url"}},
		{[]string{"pr", "list"}, []string{"number", "title", "author", "isDraft", "reviewDecision"}},
		{[]string{"run", "view", "1"}, []string{"databaseId", "status", "conclusion", "jobs"}},
		{[]string{"repo", "view"}, []string{"nameWithOwner", "description", "isPrivate", "defaultBranchRef", "stargazerCount", "forkCount"}},
	}
	for _, c := range cases {
		p := planGh(c.args)
		if p == nil {
			t.Fatalf("planGh(%v) = nil", c.args)
		}
		joined := strings.Join(p.jsonArgs, " ")
		for _, f := range c.fields {
			if !strings.Contains(joined, f) {
				t.Errorf("planGh(%v) does not request %q, so the renderer would read nothing", c.args, f)
			}
		}
	}
}
