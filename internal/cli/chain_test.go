package cli

import (
	"strings"
	"testing"
)

// Reassembling an unmodified split must reproduce the input exactly. Every
// rewrite is a partial edit of a split command line, so if the join is not
// lossless then a chain with one rewritten segment silently corrupts the rest.
func TestSplitChain_RoundTripsExactly(t *testing.T) {
	inputs := []string{
		"cat a.ts",
		"cat a.ts; cat b.ts",
		"cat a.ts && ls src",
		"grep -rn foo src | head -20",
		"cat a.ts   &&   ls   src  ",
		"cat a.ts\nls src\n",
		"a || b && c ; d | e",
		"grep 'foo;bar' src",
		`grep "a|b" src`,
		"ls",
		"",
		"   ",
		"grep foo src 2>/dev/null",
		"cat a.ts > out.txt",
		`grep 'it\'s' src`,
	}
	for _, in := range inputs {
		parts, ok := splitChain(in)
		if !ok {
			continue // deliberately refused; nothing to round-trip
		}
		if got := joinChain(parts); got != in {
			t.Errorf("joinChain(splitChain(%q)) = %q, want identical", in, got)
		}
	}
}

func TestSplitChain_Separators(t *testing.T) {
	tests := []struct {
		in   string
		cmds []string
		seps []string
	}{
		{"cat a.ts", []string{"cat a.ts"}, []string{""}},
		{"cat a.ts; cat b.ts", []string{"cat a.ts", "cat b.ts"}, []string{"", ";"}},
		{"cat a && ls", []string{"cat a", "ls"}, []string{"", "&&"}},
		{"cat a || ls", []string{"cat a", "ls"}, []string{"", "||"}},
		{"grep foo src | head", []string{"grep foo src", "head"}, []string{"", "|"}},
		{"a\nb", []string{"a", "b"}, []string{"", "\n"}},
		{"a | b | c", []string{"a", "b", "c"}, []string{"", "|", "|"}},
	}
	for _, tc := range tests {
		parts, ok := splitChain(tc.in)
		if !ok {
			t.Errorf("splitChain(%q) refused, want a split", tc.in)
			continue
		}
		if len(parts) != len(tc.cmds) {
			t.Errorf("splitChain(%q) gave %d parts, want %d", tc.in, len(parts), len(tc.cmds))
			continue
		}
		for i := range parts {
			if parts[i].cmd != tc.cmds[i] {
				t.Errorf("splitChain(%q) part %d cmd = %q, want %q", tc.in, i, parts[i].cmd, tc.cmds[i])
			}
			if parts[i].sep != tc.seps[i] {
				t.Errorf("splitChain(%q) part %d sep = %q, want %q", tc.in, i, parts[i].sep, tc.seps[i])
			}
		}
	}
}

// A separator inside a quoted argument is data, not an operator. Splitting on
// it would break the command apart in the middle of a pattern.
func TestSplitChain_QuotesHideSeparators(t *testing.T) {
	for _, in := range []string{
		"grep 'foo;bar' src",
		`grep "foo|bar" src`,
		`grep "a&&b" src`,
		"grep 'a\nb' src",
	} {
		parts, ok := splitChain(in)
		if !ok {
			t.Errorf("splitChain(%q) refused, want one part", in)
			continue
		}
		if len(parts) != 1 {
			t.Errorf("splitChain(%q) gave %d parts, want 1 (separator was quoted)", in, len(parts))
		}
	}
}

// These constructs route one command's output into another or run it detached.
// Rewriting a piece of them in isolation cannot preserve what the whole line
// does, so the splitter refuses and the command passes through as it does today.
func TestSplitChain_RefusesUnsplittable(t *testing.T) {
	for _, in := range []string{
		"echo $(cat foo.txt)",
		"echo `cat foo.txt`",
		"(cat a.ts; cat b.ts)",
		"cat a.ts &",
		"case $x in a) ls ;; esac",
		"grep 'unbalanced src",
	} {
		if _, ok := splitChain(in); ok {
			t.Errorf("splitChain(%q) = ok, want refused", in)
		}
	}
}

// 2>&1 and &>log contain an ampersand that is part of a redirection, not a
// request to background the command. Treating it as backgrounding would refuse
// a very common and perfectly rewritable form.
func TestSplitChain_RedirectAmpersandIsNotBackgrounding(t *testing.T) {
	for _, in := range []string{
		"grep foo src 2>&1",
		"tsc --noEmit 2>&1 | head",
		"make build &>log.txt",
	} {
		if _, ok := splitChain(in); !ok {
			t.Errorf("splitChain(%q) refused, want a split (& belongs to a redirect)", in)
		}
	}
}

func TestStdoutRedirect(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"cat a.ts", false},
		{"grep foo src 2>/dev/null", false},
		{"grep foo src 2>&1", false},
		{"tsc 2>>errors.log", false},
		{"cat a.ts > b.ts", true},
		{"cat a.ts >> b.ts", true},
		{"ls 1>out", true},
		{"make &>log", true},
		{"wc -l < a.ts", true},
		{`grep "a > b" src`, false}, // inside quotes it is a pattern
	}
	for _, tc := range tests {
		if got := stdoutRedirect(tc.cmd); got != tc.want {
			t.Errorf("stdoutRedirect(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

// A stage that reads stdin cannot be rewritten: its arguments say nothing about
// where its input came from. `git log | grep fix` is the case that would
// otherwise produce a confidently wrong answer, because `yeet grep` searches
// the working tree rather than the piped text.
func TestSegmentRewritable_PipeRules(t *testing.T) {
	tests := []struct {
		in   string
		idx  int
		want bool
		why  string
	}{
		{"grep -rn foo src | head -20", 0, true, "head only trims the stream"},
		{"grep -rn foo src | head -20", 1, false, "stage 1 reads stdin"},
		{"cat a.json | jq .name", 0, false, "jq reads structure out of its input"},
		{"ls | wc -l", 0, false, "wc counts lines of the raw form"},
		{"git log | grep fix", 0, false, "grep is not a display-only consumer"},
		{"cat a.ts | head -5 | less", 0, true, "every downstream stage only pages"},
		{"cat a.ts | sort | head", 0, false, "sort reorders the raw form"},
		{"cat a.ts > b.ts", 0, false, "stdout goes to a file"},
		{"cat a.ts; wc -l b.ts", 0, true, "; ends the pipeline"},
		{"cat a.ts; wc -l b.ts", 1, true, "a new command, not a pipe stage"},
	}
	for _, tc := range tests {
		parts, ok := splitChain(tc.in)
		if !ok {
			t.Fatalf("splitChain(%q) refused", tc.in)
		}
		if got := segmentRewritable(parts, tc.idx); got != tc.want {
			t.Errorf("segmentRewritable(%q, %d) = %v, want %v — %s", tc.in, tc.idx, got, tc.want, tc.why)
		}
	}
}

func TestDedupConsecutive(t *testing.T) {
	in := []string{"a", "a", "a", "b", "a"}
	got := dedupConsecutive(in)
	want := []string{"a  (x3)", "b", "a"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("dedupConsecutive(%v) = %v, want %v", in, got, want)
	}
}

func TestCapLines(t *testing.T) {
	if got := capLines([]string{"a", "b"}, 10); len(got) != 2 {
		t.Errorf("capLines under the cap changed the input: %v", got)
	}
	in := make([]string, 100)
	for i := range in {
		in[i] = "line"
	}
	got := capLines(in, 11)
	if len(got) != 11 {
		t.Errorf("capLines gave %d lines, want 11", len(got))
	}
	// The omission has to be stated — a silently truncated listing reads as a
	// complete one.
	if !strings.Contains(strings.Join(got, "\n"), "omitted") {
		t.Errorf("capLines did not report what it dropped: %v", got)
	}
}
