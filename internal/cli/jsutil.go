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
