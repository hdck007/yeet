package cli

import (
	"fmt"
	"strings"
	"testing"
)

// psAuxFixture builds a `ps aux` output of the shape a working machine
// produces: a long tail of near-identical helper processes around a handful
// that are actually doing something.
func psAuxFixture(helpers int) string {
	var b strings.Builder
	b.WriteString("USER               PID  %CPU %MEM      VSZ    RSS   TT  STAT STARTED      TIME COMMAND\n")
	b.WriteString("dev               4213  92.4  1.1 41203344  98123   ??  R     9:14AM   4:12.88 /usr/local/bin/node /repo/node_modules/.bin/vitest run --reporter=verbose\n")
	b.WriteString("dev               4310   3.2 14.7 42103344 998123   ??  S     9:01AM   1:02.10 /Applications/Visual Studio Code.app/Contents/Frameworks/Code Helper (Plugin).app/Contents/MacOS/Code Helper (Plugin)\n")
	b.WriteString("root                 1   0.1  0.1  4321012   9123   ??  Ss    8:59AM   0:12.00 /sbin/launchd\n")
	b.WriteString("dev               5001  11.0  2.2 41000000 180000   ??  S     9:10AM   0:44.10 /usr/bin/python3 /repo/scripts/ingest.py --watch\n")
	for i := 0; i < helpers; i++ {
		b.WriteString(fmt.Sprintf(
			"dev              %5d   0.0  0.2 41888888  17%03d   ??  S     9:0%dAM   0:00.%02d /Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/126.0/Helpers/Google Chrome Helper (Renderer).app/Contents/MacOS/Google Chrome Helper (Renderer) --type=renderer --instance=%d\n",
			20000+i, i%1000, i%10, i%100, i))
	}
	return b.String()
}

func TestRenderPS_CollapsesTheHelperTail(t *testing.T) {
	raw := psAuxFixture(300)
	got := renderPS(raw)

	if len(got) >= len(raw) {
		t.Fatalf("renderPS did not shrink the output: %d bytes in, %d out", len(raw), len(got))
	}
	// 300 near-identical helper rows are the whole reason `ps aux` is expensive.
	if ratio := float64(len(got)) / float64(len(raw)); ratio > 0.10 {
		t.Errorf("renderPS kept %.1f%% of the bytes, want under 10%%", ratio*100)
	}

	// The heavy processes are exactly what the reader came for and must survive
	// by name, not be folded into a count.
	for _, want := range []string{"vitest", "python3:ingest", "304 procs"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderPS dropped %q:\n%s", want, got)
		}
	}
	// The collapsed tail must still be accounted for.
	if !strings.Contains(got, "x300") {
		t.Errorf("renderPS did not report the 300 grouped helpers:\n%s", got)
	}
}

// A short listing has nothing to summarise. Reformatting it would cost the
// caller the columns they asked for and save nothing.
func TestRenderPS_LeavesShortListingsAlone(t *testing.T) {
	raw := psAuxFixture(2)
	if got := renderPS(raw); got != raw {
		t.Errorf("renderPS rewrote a 6-row listing; want it passed through:\n%s", got)
	}
}

// `ps -ef` has no %CPU or %MEM columns. The grouping still works; the leader
// tables have to be omitted rather than printed full of zeros.
func TestRenderPS_HandlesEFLayout(t *testing.T) {
	var b strings.Builder
	b.WriteString("  UID   PID  PPID   C STIME   TTY           TIME CMD\n")
	for i := 0; i < 40; i++ {
		b.WriteString(fmt.Sprintf("  501 %5d     1   0  9:01AM ??         0:00.01 /usr/libexec/some-daemon-%d --flag\n", 3000+i, i%4))
	}
	got := renderPS(b.String())

	if strings.Contains(got, "top cpu") {
		t.Errorf("renderPS printed a CPU table for a layout with no %%CPU column:\n%s", got)
	}
	if !strings.Contains(got, "by program") {
		t.Errorf("renderPS lost the grouping for the -ef layout:\n%s", got)
	}
	if !strings.Contains(got, "no cpu/mem columns") {
		t.Errorf("renderPS did not say why the load figures are absent:\n%s", got)
	}
}

// An unrecognised layout must pass through. A summary of a misparsed table is
// a confident description of something that is not there.
func TestRenderPS_PassesThroughUnparseableOutput(t *testing.T) {
	for _, raw := range []string{
		"",
		"not a ps table at all\n",
		"COL1 COL2\nfoo bar\n",
		"USER PID SOMETHINGELSE\na b c\n",
	} {
		if got := renderPS(raw); got != raw {
			t.Errorf("renderPS(%q) = %q, want the input unchanged", raw, got)
		}
	}
}

// The interpreter's own name says nothing about what the process is: fourteen
// rows reading "node" answer no question, while "node:vitest" answers the one
// that was asked.
func TestPSLabel(t *testing.T) {
	tests := []struct{ cmd, want string }{
		{"/usr/local/bin/node /repo/node_modules/.bin/vitest run", "node:vitest"},
		{"/usr/bin/python3 /repo/scripts/ingest.py --watch", "python3:ingest"},
		{"node --inspect /app/dist/server.js", "node:server"},
		{"/sbin/launchd", "launchd"},
		{"[kworker/2:1-events]", "[kworker/2:1-events]"},
		{"/usr/bin/ruby /app/bin/rails s", "ruby:rails"},
		{"node", "node"},
		{"", "?"},
		{"/opt/homebrew/bin/bash -lc 'make build'", "bash:make build"},
	}
	for _, tc := range tests {
		if got := psLabel(tc.cmd); got != tc.want {
			t.Errorf("psLabel(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

func TestSplitLeadingFields(t *testing.T) {
	line := "dev    4213  92.4 /usr/bin/node a b   c"
	got := splitLeadingFields(line, 4)
	want := []string{"dev", "4213", "92.4", "/usr/bin/node a b   c"}
	if len(got) != len(want) {
		t.Fatalf("splitLeadingFields gave %d fields, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTruncateMiddle(t *testing.T) {
	if got := truncateMiddle("short", 20); got != "short" {
		t.Errorf("truncateMiddle shortened a string that fit: %q", got)
	}
	long := "/very/long/path/to/some/deeply/nested/binary --with-flags"
	got := truncateMiddle(long, 30)
	if len(got) > 30 {
		t.Errorf("truncateMiddle(%q, 30) = %q (%d chars), want at most 30", long, got, len(got))
	}
	// Both ends carry information: which install it came from, and what it is.
	if !strings.HasPrefix(got, "/very") || !strings.HasSuffix(got, "flags") {
		t.Errorf("truncateMiddle dropped an end: %q", got)
	}
}
