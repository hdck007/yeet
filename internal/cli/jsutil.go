package cli

import yeetexec "github.com/hdck007/yeet/internal/exec"

// detectJSRunner finds the best way to run a JS tool.
// Returns the tool name directly if available, or "npx" as fallback.
func detectJSRunner(tool string) string {
	if yeetexec.Available(tool) {
		return tool
	}
	return "npx"
}

// dropLeading removes sub from the front of args if it is there. The wrappers
// below always pass their own subcommand to the underlying tool, and every one
// of those tools reads a second copy of it as a positional path filter rather
// than as a command — which silently narrows the run to nothing instead of
// failing loudly.
func dropLeading(args []string, sub string) []string {
	if len(args) > 0 && args[0] == sub {
		return args[1:]
	}
	return args
}
