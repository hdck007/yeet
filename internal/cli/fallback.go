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
