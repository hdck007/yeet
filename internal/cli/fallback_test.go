package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn with stdout redirected and returns what it wrote.
// printBetterN reports a byte count that the analytics layer records as a
// saving, so the count has to match what actually came out.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	os.Stdout = orig
	return <-done
}

func TestPrintBetterN_PrintsFilteredWhenSmaller(t *testing.T) {
	raw := strings.Repeat("x", 100)
	filtered := "short"
	var printed int
	var shorter bool
	out := captureStdout(t, func() { printed, shorter = printBetterN(raw, filtered) })

	if out != filtered {
		t.Errorf("wrote %q, want the filtered form %q", out, filtered)
	}
	if !shorter {
		t.Error("shorter = false, want true")
	}
	if printed != len(filtered) {
		t.Errorf("printed = %d, want %d", printed, len(filtered))
	}
	if printed != len(out) {
		t.Errorf("the reported count (%d) must match what was written (%d)", printed, len(out))
	}
}

// The reported count is the load-bearing part: when the fallback fires, the raw
// output goes to the model, and recording the filtered length would book a
// saving that never happened.
func TestPrintBetterN_PrintsRawWhenFilteredIsBigger(t *testing.T) {
	raw := "small"
	filtered := strings.Repeat("y", 100)
	var printed int
	var shorter bool
	out := captureStdout(t, func() { printed, shorter = printBetterN(raw, filtered) })

	if out != raw {
		t.Errorf("wrote %q, want the raw form %q", out, raw)
	}
	if shorter {
		t.Error("shorter = true, want false")
	}
	if printed != len(raw) {
		t.Errorf("printed = %d, want %d (the raw length, not the filtered one)", printed, len(raw))
	}
	if printed == len(filtered) {
		t.Error("printed reported the filtered length — this is the bug that logged phantom savings")
	}
}

func TestPrintBetterN_EqualLengthsPrintRaw(t *testing.T) {
	raw, filtered := "12345", "abcde"
	var printed int
	out := captureStdout(t, func() { printed, _ = printBetterN(raw, filtered) })
	// No saving means no reason to reshape the output.
	if out != raw {
		t.Errorf("equal lengths should print raw, wrote %q", out)
	}
	if printed != len(raw) {
		t.Errorf("printed = %d, want %d", printed, len(raw))
	}
}

func TestPrintBetter_MatchesPrintBetterN(t *testing.T) {
	var gain bool
	captureStdout(t, func() { gain = printBetter(strings.Repeat("x", 50), "tiny") })
	if !gain {
		t.Error("printBetter should report a gain when the filtered form is smaller")
	}
	captureStdout(t, func() { gain = printBetter("tiny", strings.Repeat("x", 50)) })
	if gain {
		t.Error("printBetter should report no gain when the filtered form is bigger")
	}
}

// yeet's own flags must never reach the wrapped binary. git and gh set
// DisableFlagParsing, so they receive the argument list verbatim and reject
// anything they do not recognise.
func TestStripYeetFlags(t *testing.T) {
	saveA, saveR, saveU := noAnalytics, rawOutput, ultraCompact
	t.Cleanup(func() { noAnalytics, rawOutput, ultraCompact = saveA, saveR, saveU })

	noAnalytics, rawOutput, ultraCompact = false, false, false
	got := stripYeetFlags([]string{"status", "--no-analytics", "--short"})
	if strings.Join(got, " ") != "status --short" {
		t.Errorf("stripYeetFlags left yeet flags behind: %v", got)
	}
	if !noAnalytics {
		t.Error("--no-analytics should take effect even though cobra never parsed it")
	}

	noAnalytics, rawOutput, ultraCompact = false, false, false
	got = stripYeetFlags([]string{"pr", "list", "-u"})
	if strings.Join(got, " ") != "pr list" {
		t.Errorf("-u should be consumed, got %v", got)
	}
	if !ultraCompact {
		t.Error("-u should set ultraCompact")
	}

	noAnalytics, rawOutput, ultraCompact = false, false, false
	got = stripYeetFlags([]string{"log", "--raw", "--ultra-compact", "--no-analytics", "-5"})
	if strings.Join(got, " ") != "log -5" {
		t.Errorf("all yeet flags should be consumed, got %v", got)
	}
	if !rawOutput || !ultraCompact || !noAnalytics {
		t.Errorf("flags not applied: raw=%v ultra=%v noAnalytics=%v", rawOutput, ultraCompact, noAnalytics)
	}

	// Arguments that merely resemble yeet flags must survive untouched.
	noAnalytics, rawOutput, ultraCompact = false, false, false
	got = stripYeetFlags([]string{"grep", "--raw-ish", "-user", "--no-analytics-x"})
	if strings.Join(got, " ") != "grep --raw-ish -user --no-analytics-x" {
		t.Errorf("look-alike arguments were eaten: %v", got)
	}
	if noAnalytics || rawOutput || ultraCompact {
		t.Error("look-alike arguments should not set any flag")
	}

	// An empty list is not a crash.
	if got := stripYeetFlags(nil); len(got) != 0 {
		t.Errorf("stripYeetFlags(nil) = %v, want empty", got)
	}
}
