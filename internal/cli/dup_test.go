package cli

import (
	"strings"
	"testing"
)

// vitest, playwright and next each pass their own subcommand to the underlying
// tool. Every one of those tools reads a second copy of it as a positional path
// filter rather than as a command, so `yeet vitest run` used to run
// `vitest run --reporter=json run`, match no files, and report that all zero
// tests passed. A silent wrong answer costs far more than the noise it saved.
func TestDropLeading(t *testing.T) {
	tests := []struct {
		args []string
		sub  string
		want []string
	}{
		{[]string{"run"}, "run", []string{}},
		{[]string{"run", "src/api"}, "run", []string{"src/api"}},
		{[]string{"src/api"}, "run", []string{"src/api"}},
		{nil, "run", nil},
		{[]string{"--reporter=verbose"}, "run", []string{"--reporter=verbose"}},
		// Only the leading position is dropped: a file genuinely named "run"
		// later in the list is an argument, not a duplicate subcommand.
		{[]string{"src", "run"}, "run", []string{"src", "run"}},
		{[]string{"test", "e2e/"}, "test", []string{"e2e/"}},
		{[]string{"build"}, "build", []string{}},
	}
	for _, tc := range tests {
		got := dropLeading(tc.args, tc.sub)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("dropLeading(%v, %q) = %v, want %v", tc.args, tc.sub, got, tc.want)
		}
	}
}

// `yeet next` renders a build and nothing else. Rewriting `next dev` would
// start a build instead of a dev server — not a smaller answer, a different one.
func TestRewriteCommand_NextOnlyWrapsBuild(t *testing.T) {
	runRewriteCases(t, []rewriteCase{
		{in: "next build", want: "yeet next build", v: vAsk, why: "the one subcommand the renderer understands"},
		{in: "next build --debug", want: "yeet next build --debug", v: vAsk, why: ""},
		{in: "next dev", v: vNever, why: "a dev server is not a build"},
		{in: "next start", v: vNever, why: "serving is not a build"},
		{in: "next lint", v: vNever, why: "not what this renderer parses"},
	})
}
