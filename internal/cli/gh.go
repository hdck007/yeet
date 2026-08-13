package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

// gh's human output is built for a terminal: box drawing, colour, full PR
// bodies, wrapped CI tables. Asking gh for JSON and rendering only the fields
// an agent acts on is a large, reliable win.
//
// Unrecognised subcommands pass straight through, so `yeet gh <anything>` is
// always safe.

var ghCmd = &cobra.Command{
	Use:                "gh <subcommand> [args...]",
	Short:              "Compact GitHub CLI output (pr, issue, run, repo)",
	DisableFlagParsing: true,
	Args:               cobra.MinimumNArgs(1),
	RunE:               runGh,
}

func init() {
	rootCmd.AddCommand(ghCmd)
}

// ghPlan describes how to get JSON for a subcommand and how to render it.
type ghPlan struct {
	jsonArgs     []string
	baselineArgs []string
	render       func([]byte) (string, error)
}

func planGh(args []string) *ghPlan {
	if len(args) < 2 {
		return nil
	}
	group, sub := args[0], args[1]
	rest := args[2:]

	switch group {
	case "pr":
		switch sub {
		case "list":
			return &ghPlan{
				jsonArgs:     append([]string{"pr", "list", "--json", "number,title,author,headRefName,isDraft,reviewDecision,updatedAt"}, rest...),
				baselineArgs: append([]string{"pr", "list"}, rest...),
				render:       renderPRList,
			}
		case "view":
			return &ghPlan{
				jsonArgs:     append([]string{"pr", "view", "--json", "number,title,author,state,isDraft,reviewDecision,mergeable,headRefName,baseRefName,additions,deletions,changedFiles,url"}, rest...),
				baselineArgs: append([]string{"pr", "view"}, rest...),
				render:       renderPRView,
			}
		case "checks":
			return &ghPlan{
				jsonArgs:     append([]string{"pr", "checks", "--json", "name,state,bucket,link"}, rest...),
				baselineArgs: append([]string{"pr", "checks"}, rest...),
				render:       renderChecks,
			}
		}
	case "issue":
		switch sub {
		case "list":
			return &ghPlan{
				jsonArgs:     append([]string{"issue", "list", "--json", "number,title,author,labels,updatedAt"}, rest...),
				baselineArgs: append([]string{"issue", "list"}, rest...),
				render:       renderIssueList,
			}
		case "view":
			return &ghPlan{
				jsonArgs:     append([]string{"issue", "view", "--json", "number,title,author,state,labels,url"}, rest...),
				baselineArgs: append([]string{"issue", "view"}, rest...),
				render:       renderIssueView,
			}
		}
	case "repo":
		switch sub {
		case "view":
			return &ghPlan{
				jsonArgs:     append([]string{"repo", "view", "--json", "nameWithOwner,description,isPrivate,isArchived,defaultBranchRef,stargazerCount,forkCount,primaryLanguage,url"}, rest...),
				baselineArgs: append([]string{"repo", "view"}, rest...),
				render:       renderRepoView,
			}
		case "list":
			return &ghPlan{
				jsonArgs:     append([]string{"repo", "list", "--json", "nameWithOwner,description,isPrivate,isArchived,stargazerCount,primaryLanguage"}, rest...),
				baselineArgs: append([]string{"repo", "list"}, rest...),
				render:       renderRepoList,
			}
		}
	case "run":
		switch sub {
		case "list":
			return &ghPlan{
				jsonArgs:     append([]string{"run", "list", "--json", "databaseId,name,status,conclusion,headBranch,event,createdAt"}, rest...),
				baselineArgs: append([]string{"run", "list"}, rest...),
				render:       renderRunList,
			}
		case "view":
			return &ghPlan{
				jsonArgs:     append([]string{"run", "view", "--json", "databaseId,name,status,conclusion,headBranch,jobs"}, rest...),
				baselineArgs: append([]string{"run", "view"}, rest...),
				render:       renderRunView,
			}
		}
	}
	return nil
}

func runGh(cmd *cobra.Command, args []string) error {
	if !yeetexec.Available("gh") {
		return fmt.Errorf("gh not found in PATH")
	}
	start := time.Now()
	args = stripYeetFlags(args)
	if len(args) == 0 {
		return fmt.Errorf("gh: no subcommand given")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	p := planGh(args)
	if p == nil {
		result := yeetexec.Run(ctx, "gh", args...)
		fmt.Print(result.Stdout)
		if result.Stderr != "" {
			fmt.Fprint(os.Stderr, result.Stderr)
		}
		if result.ExitCode != 0 {
			os.Exit(result.ExitCode)
		}
		return nil
	}

	result := yeetexec.Run(ctx, "gh", p.jsonArgs...)
	if result.ExitCode != 0 {
		fmt.Print(result.Stdout)
		if result.Stderr != "" {
			fmt.Fprint(os.Stderr, result.Stderr)
		}
		os.Exit(result.ExitCode)
	}

	rendered, err := p.render([]byte(result.Stdout))
	if err != nil {
		// Rendering failed — hand back what gh said rather than a broken summary.
		fmt.Print(result.Stdout)
		return nil
	}

	// The baseline is the human command the caller would have run.
	baseline := yeetexec.Run(ctx, "gh", p.baselineArgs...)
	baselineOut := baseline.Stdout
	kind := analytics.BaselineAsInvoked
	if baseline.ExitCode != 0 || strings.TrimSpace(baselineOut) == "" {
		baselineOut = result.Stdout
		kind = analytics.BaselineSynthetic
	}

	printed, _ := printBetterN(baselineOut, rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "gh",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(baselineOut),
			CharsRendered: len(rendered),
			CharsPrinted:  printed,
			BaselineCmd:   "gh " + strings.Join(p.baselineArgs, " "),
			YeetCmd:       "yeet gh " + strings.Join(args, " "),
			BaselineKind:  kind,
			ExitCode:      result.ExitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

// ─── Renderers ────────────────────────────────────────────────────────────────

type ghUser struct{ Login string }
type ghLabel struct{ Name string }

func renderPRList(b []byte) (string, error) {
	var prs []struct {
		Number         int
		Title          string
		Author         ghUser
		HeadRefName    string
		IsDraft        bool
		ReviewDecision string
		UpdatedAt      string
	}
	if err := json.Unmarshal(b, &prs); err != nil {
		return "", err
	}
	if len(prs) == 0 {
		return "no open PRs\n", nil
	}
	var s strings.Builder
	fmt.Fprintf(&s, "%d PRs:\n", len(prs))
	for _, p := range prs {
		flags := ""
		if p.IsDraft {
			flags += " [draft]"
		}
		if d := shortReview(p.ReviewDecision); d != "" {
			flags += " [" + d + "]"
		}
		fmt.Fprintf(&s, "  #%d %s (%s)%s\n", p.Number, p.Title, p.Author.Login, flags)
	}
	return s.String(), nil
}

func renderPRView(b []byte) (string, error) {
	var p struct {
		Number         int
		Title          string
		Author         ghUser
		State          string
		IsDraft        bool
		ReviewDecision string
		Mergeable      string
		HeadRefName    string
		BaseRefName    string
		Additions      int
		Deletions      int
		ChangedFiles   int
		URL            string
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return "", err
	}
	var s strings.Builder
	fmt.Fprintf(&s, "#%d %s\n", p.Number, p.Title)
	fmt.Fprintf(&s, "%s by %s | %s -> %s\n", strings.ToLower(p.State), p.Author.Login, p.HeadRefName, p.BaseRefName)
	fmt.Fprintf(&s, "%d files, +%d -%d", p.ChangedFiles, p.Additions, p.Deletions)
	if p.Mergeable != "" {
		fmt.Fprintf(&s, " | mergeable: %s", strings.ToLower(p.Mergeable))
	}
	if d := shortReview(p.ReviewDecision); d != "" {
		fmt.Fprintf(&s, " | review: %s", d)
	}
	if p.IsDraft {
		s.WriteString(" | draft")
	}
	s.WriteString("\n")
	if p.URL != "" {
		fmt.Fprintf(&s, "%s\n", p.URL)
	}
	return s.String(), nil
}

func renderChecks(b []byte) (string, error) {
	var checks []struct {
		Name   string
		State  string
		Bucket string
		Link   string
	}
	if err := json.Unmarshal(b, &checks); err != nil {
		return "", err
	}
	if len(checks) == 0 {
		return "no checks\n", nil
	}
	// Failures are the only thing worth spending lines on; everything else is a count.
	byBucket := map[string]int{}
	var failed []string
	for _, c := range checks {
		key := c.Bucket
		if key == "" {
			key = strings.ToLower(c.State)
		}
		byBucket[key]++
		if key == "fail" || key == "cancel" {
			line := "  " + c.Name
			if c.Link != "" {
				line += " " + c.Link
			}
			failed = append(failed, line)
		}
	}
	var s strings.Builder
	var parts []string
	for _, k := range []string{"pass", "fail", "pending", "skipping", "cancel"} {
		if n := byBucket[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, k))
		}
	}
	fmt.Fprintf(&s, "%d checks: %s\n", len(checks), strings.Join(parts, ", "))
	for _, f := range failed {
		s.WriteString(f + "\n")
	}
	return s.String(), nil
}

func renderIssueList(b []byte) (string, error) {
	var issues []struct {
		Number int
		Title  string
		Author ghUser
		Labels []ghLabel
	}
	if err := json.Unmarshal(b, &issues); err != nil {
		return "", err
	}
	if len(issues) == 0 {
		return "no open issues\n", nil
	}
	var s strings.Builder
	fmt.Fprintf(&s, "%d issues:\n", len(issues))
	for _, i := range issues {
		labels := ""
		if len(i.Labels) > 0 {
			var ls []string
			for _, l := range i.Labels {
				ls = append(ls, l.Name)
			}
			labels = " [" + strings.Join(ls, ",") + "]"
		}
		fmt.Fprintf(&s, "  #%d %s (%s)%s\n", i.Number, i.Title, i.Author.Login, labels)
	}
	return s.String(), nil
}

func renderIssueView(b []byte) (string, error) {
	var i struct {
		Number int
		Title  string
		Author ghUser
		State  string
		Labels []ghLabel
		URL    string
	}
	if err := json.Unmarshal(b, &i); err != nil {
		return "", err
	}
	var s strings.Builder
	fmt.Fprintf(&s, "#%d %s\n", i.Number, i.Title)
	fmt.Fprintf(&s, "%s by %s", strings.ToLower(i.State), i.Author.Login)
	if len(i.Labels) > 0 {
		var ls []string
		for _, l := range i.Labels {
			ls = append(ls, l.Name)
		}
		fmt.Fprintf(&s, " | %s", strings.Join(ls, ","))
	}
	s.WriteString("\n")
	if i.URL != "" {
		fmt.Fprintf(&s, "%s\n", i.URL)
	}
	return s.String(), nil
}

func renderRunList(b []byte) (string, error) {
	var runs []struct {
		DatabaseId int64
		Name       string
		Status     string
		Conclusion string
		HeadBranch string
		Event      string
	}
	if err := json.Unmarshal(b, &runs); err != nil {
		return "", err
	}
	if len(runs) == 0 {
		return "no runs\n", nil
	}
	var s strings.Builder
	fmt.Fprintf(&s, "%d runs:\n", len(runs))
	prevBranch := ""
	for _, r := range runs {
		state := r.Conclusion
		if state == "" {
			state = r.Status
		}
		branch := shortRef(r.HeadBranch)
		// Runs come back newest-first and usually cluster on one branch, so the
		// branch is printed only when it changes. Dropping it outright would be
		// smaller still, but which branch a run tested is the whole point of
		// looking at the list.
		suffix := ""
		if !ultraCompact && branch != prevBranch {
			suffix = " (" + branch + ")"
			prevBranch = branch
		}
		fmt.Fprintf(&s, "  %s %d %s%s\n", runMarker(state), r.DatabaseId, r.Name, suffix)
	}
	return s.String(), nil
}

// shortRef trims the ref plumbing off a branch name: refs/pull/29/head is
// pull/29 to anyone reading it.
func shortRef(ref string) string {
	if r := strings.TrimPrefix(ref, "refs/pull/"); r != ref {
		return "pull/" + strings.TrimSuffix(r, "/head")
	}
	return strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/")
}

// runMarker compresses a CI conclusion to a short token. Anything unexpected
// keeps its full name rather than being silently flattened to "?".
func runMarker(state string) string {
	switch state {
	case "success":
		return "ok"
	case "failure":
		return "FAIL"
	case "cancelled":
		return "cxl"
	case "skipped":
		return "skip"
	case "in_progress":
		return "run"
	case "queued", "waiting", "pending", "requested":
		return "wait"
	}
	return state
}

func renderRepoView(b []byte) (string, error) {
	var r struct {
		NameWithOwner    string
		Description      string
		IsPrivate        bool
		IsArchived       bool
		DefaultBranchRef struct{ Name string }
		StargazerCount   int
		ForkCount        int
		PrimaryLanguage  struct{ Name string }
		URL              string
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", err
	}
	var s strings.Builder
	vis := "public"
	if r.IsPrivate {
		vis = "private"
	}
	fmt.Fprintf(&s, "%s [%s]", r.NameWithOwner, vis)
	if r.IsArchived {
		s.WriteString(" [archived]")
	}
	s.WriteString("\n")
	if r.Description != "" {
		fmt.Fprintf(&s, "  %s\n", r.Description)
	}
	var facts []string
	// The primary language is visible from the files themselves, so it is not
	// worth a line here. The default branch is not inferable and matters for
	// anything that opens a PR, so it stays unless -u was asked for.
	if r.DefaultBranchRef.Name != "" && !ultraCompact {
		facts = append(facts, "@"+r.DefaultBranchRef.Name)
	}
	facts = append(facts, fmt.Sprintf("%d stars", r.StargazerCount), fmt.Sprintf("%d forks", r.ForkCount))
	fmt.Fprintf(&s, "  %s\n", strings.Join(facts, " | "))
	if r.URL != "" {
		fmt.Fprintf(&s, "  %s\n", r.URL)
	}
	return s.String(), nil
}

func renderRepoList(b []byte) (string, error) {
	var repos []struct {
		NameWithOwner   string
		Description     string
		IsPrivate       bool
		IsArchived      bool
		StargazerCount  int
		PrimaryLanguage struct{ Name string }
	}
	if err := json.Unmarshal(b, &repos); err != nil {
		return "", err
	}
	if len(repos) == 0 {
		return "no repos\n", nil
	}
	var s strings.Builder
	fmt.Fprintf(&s, "%d repos:\n", len(repos))
	for _, r := range repos {
		flags := ""
		if r.IsPrivate {
			flags += " [private]"
		}
		if r.IsArchived {
			flags += " [archived]"
		}
		lang := ""
		if r.PrimaryLanguage.Name != "" {
			lang = " " + r.PrimaryLanguage.Name
		}
		fmt.Fprintf(&s, "  %s%s%s\n", r.NameWithOwner, lang, flags)
	}
	return s.String(), nil
}

func renderRunView(b []byte) (string, error) {
	var r struct {
		DatabaseId int64
		Name       string
		Status     string
		Conclusion string
		HeadBranch string
		Jobs       []struct {
			Name       string
			Status     string
			Conclusion string
			Steps      []struct {
				Name       string
				Conclusion string
			}
		}
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", err
	}
	state := r.Conclusion
	if state == "" {
		state = r.Status
	}
	var s strings.Builder
	fmt.Fprintf(&s, "run %d %s: %s (%s)\n", r.DatabaseId, state, r.Name, r.HeadBranch)
	for _, j := range r.Jobs {
		js := j.Conclusion
		if js == "" {
			js = j.Status
		}
		fmt.Fprintf(&s, "  %-10s %s\n", js, j.Name)
		// Only failing steps earn a line — a green job needs no detail.
		if js == "failure" {
			for _, st := range j.Steps {
				if st.Conclusion == "failure" {
					fmt.Fprintf(&s, "    failed: %s\n", st.Name)
				}
			}
		}
	}
	return s.String(), nil
}

func shortReview(d string) string {
	switch d {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes-requested"
	case "REVIEW_REQUIRED":
		return "review-required"
	}
	return ""
}
