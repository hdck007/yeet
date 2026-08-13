package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/hdck007/yeet/internal/analytics"
)

// Fallback defines the system command to run when a yeet command fails.
type Fallback struct {
	Bin  string
	Args func(cmdArgs []string) []string
}

// NoFallback is used for commands where no system equivalent exists.
var NoFallback = Fallback{}

// runWithFallback executes fn. On error:
//  1. Records the failure to the analytics DB
//  2. Runs the system fallback command (if defined), passing output through directly
//  3. Does NOT record analytics — token savings are 0 on fallback
func runWithFallback(subcmd string, cmdArgs []string, fn func() error, fb Fallback) error {
	err := fn()
	if err == nil {
		return nil
	}

	// Record the failure
	if !noAnalytics && db != nil {
		_ = db.RecordFailure(analytics.Failure{
			Subcmd:   subcmd,
			FullCmd:  "yeet " + subcmd + " " + strings.Join(cmdArgs, " "),
			ExitCode: 1,
			Stderr:   err.Error(),
		})
	}

	if fb.Bin == "" {
		// No fallback defined — surface the original error
		return err
	}

	fmt.Fprintf(os.Stderr, "yeet: %s failed (%v), falling back to %s\n", subcmd, err, fb.Bin)

	fbArgs := fb.Args(cmdArgs)
	fbCmd := exec.Command(fb.Bin, fbArgs...)
	fbCmd.Stdout = os.Stdout
	fbCmd.Stderr = os.Stderr
	fbCmd.Stdin = os.Stdin
	// Run the fallback and pass through its exit code, but don't propagate
	// as a Go error — cobra would print usage on non-nil returns.
	if fbErr := fbCmd.Run(); fbErr != nil {
		if exitErr, ok := fbErr.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
	return nil
}

// stripYeetFlags removes yeet's own persistent flags from an argument list.
// Commands that set DisableFlagParsing receive them verbatim, and forwarding
// something like --no-analytics to git or gh makes the underlying binary reject
// the whole command. Cobra never parses them in that mode either, so the flags
// are also applied here.
func stripYeetFlags(args []string) []string {
	clean := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--no-analytics":
			noAnalytics = true
		case "--raw":
			rawOutput = true
		case "-u", "--ultra-compact":
			ultraCompact = true
		default:
			clean = append(clean, a)
		}
	}
	return clean
}

// ultraCompact drops fields that are useful but not essential — branch names on
// CI runs, the language on a repo, the branch a stash came from. The default
// keeps them: a saving that costs the reader a follow-up command is not a
// saving. This flag is for callers who would rather have the smallest possible
// output and go without.
var ultraCompact bool

// printBetter prints filtered if it's shorter than raw; otherwise prints raw.
// Returns true if filtered was shorter (gain), false if raw was printed (loss).
func printBetter(raw, filtered string) bool {
	_, shorter := printBetterN(raw, filtered)
	return shorter
}

// printBetterN is printBetter but reports how many bytes actually reached
// stdout. Callers must record that number rather than the length of the
// filtered string: when the fallback fires, the model received the raw output,
// and logging the filtered length would book a saving that never happened.
func printBetterN(raw, filtered string) (printed int, shorter bool) {
	if len(filtered) < len(raw) {
		fmt.Print(filtered)
		return len(filtered), true
	}
	fmt.Print(raw)
	return len(raw), false
}
