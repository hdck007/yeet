package cli

import (
	"os"
	"strings"
	"testing"
)

// ─── regressions found by the live A/B benchmark ──────────────────────────────

// `yeet grep -l` did not exist: the flag was rejected with "unknown shorthand
// flag: 'l'". -l is one of the most common grep idioms, so an agent reaching for
// it by habit lost a turn to an error and then re-ran the search another way.
func TestGrepFilesOnly_FlagIsAccepted(t *testing.T) {
	for _, name := range []string{"files-with-matches"} {
		if grepCmd.Flags().Lookup(name) == nil {
			t.Errorf("yeet grep has no --%s flag", name)
		}
	}
	if f := grepCmd.Flags().ShorthandLookup("l"); f == nil {
		t.Error("yeet grep has no -l shorthand; `grep -l pattern .` will fail")
	}
}

// -l output is a machine-readable path list whose whole purpose is to be piped,
// and `grep -l ... | wc -l` is the common shape. A prepended note silently adds
// one to every such count -- which nearly slipped through here, because yeet
// finds 62 files where grep finds 63, so note+62 matched the expected 63.
func TestGrepFilesOnly_OutputIsPipeable(t *testing.T) {
	src, err := os.ReadFile("grep.go")
	if err != nil {
		t.Fatalf("read grep.go: %v", err)
	}
	body := string(src)
	i := strings.Index(body, "if grepFilesOnly {")
	if i < 0 {
		t.Fatal("the -l branch is gone from grep.go")
	}
	end := i + 800
	if end > len(body) {
		end = len(body)
	}
	branch := body[i:end]
	if !strings.Contains(branch, `printBetterNoteN(cmdOut, rendered, "")`) {
		t.Error("the -l branch must emit with an empty note; any note breaks `| wc -l`")
	}
}
