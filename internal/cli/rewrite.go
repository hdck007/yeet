package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// codeExts are file extensions where yeet smart produces useful declaration+line-number output.
// For all other extensions (README.md, .yml, .sh, etc.) bare reads pass through unchanged.
var codeExts = map[string]bool{
	".go":  true,
	".rs":  true,
	".py":  true,
	".ts":  true,
	".tsx": true,
	".js":  true,
	".jsx": true,
	".rb":  true,
}

// rewriteRule maps a raw command prefix to its yeet equivalent.
type rewriteRule struct {
	// prefix is the literal string the raw command must start with.
	prefix string
	// yeetPrefix replaces the matched prefix in the output.
	yeetPrefix string
}

// rules is the single source of truth for all rewrite mappings.
// To add a new rewrite, add an entry here — do not touch the hook script.
var rules = []rewriteRule{
	{prefix: "cat ", yeetPrefix: "yeet read "},
	{prefix: "grep ", yeetPrefix: "yeet grep "},
	{prefix: "ls ", yeetPrefix: "yeet ls "},
	{prefix: "ls\n", yeetPrefix: "yeet ls"},
	{prefix: "find ", yeetPrefix: "yeet find "},
	{prefix: "diff ", yeetPrefix: "yeet diff "},
}

// Exit codes consumed by the shell hook (mirrors rtk's protocol).
const (
	exitRewriteAllow = 0 // rewrite found, auto-allow
	exitNoMatch      = 1 // no rewrite rule matched, pass through
	exitDeny         = 2 // deny rule matched, pass through (host handles denial)
	exitRewriteAsk   = 3 // rewrite found, let host prompt user
)

var rewriteCmd = &cobra.Command{
	Use:    "rewrite <command>",
	Short:  "Rewrite a raw shell command to its yeet equivalent",
	Long:   "Used by the yeet-proxy.sh PreToolUse hook. Prints the rewritten command to stdout and exits with a code the hook uses to decide permission behavior.",
	Args:   cobra.ExactArgs(1),
	Hidden: true, // internal use — not shown in yeet --help
	RunE:   runRewrite,
}

func init() {
	rootCmd.AddCommand(rewriteCmd)
}

func runRewrite(cmd *cobra.Command, args []string) error {
	raw := args[0]

	// Skip heredocs — they can't be safely rewritten.
	if strings.Contains(raw, "<<") {
		os.Exit(exitNoMatch)
	}

	// Enforce reading ladder for known code files: bare `yeet read <file>` (no flags) → `yeet read -l aggressive`.
	// Gives signatures-only output (91% token reduction) in the SAME turn — no extra turn needed.
	// README, YAML, shell scripts, and other non-code files pass through unchanged.
	if strings.HasPrefix(raw, "yeet read ") {
		rest := strings.TrimPrefix(raw, "yeet read ")
		parts := strings.Fields(rest)
		hasFlags := false
		for _, p := range parts[1:] {
			if strings.HasPrefix(p, "-") {
				hasFlags = true
				break
			}
		}
		if !hasFlags && len(parts) == 1 && codeExts[strings.ToLower(filepath.Ext(parts[0]))] {
			fmt.Print("yeet read " + parts[0] + " -l aggressive")
			os.Exit(exitRewriteAllow)
		}
		os.Exit(exitNoMatch)
	}

	// Skip other commands that already use yeet.
	if strings.HasPrefix(raw, "yeet ") {
		os.Exit(exitNoMatch)
	}

	// Strip leading env var assignments (VAR=val VAR2=val2 cmd ...)
	// so "GIT_PAGER=cat grep foo" still matches the grep rule.
	stripped, envPrefix := stripEnvPrefix(raw)

	for _, rule := range rules {
		if strings.HasPrefix(stripped, rule.prefix) {
			rest := stripped[len(rule.prefix):]
			rewritten := envPrefix + rule.yeetPrefix + rest
			fmt.Print(rewritten)
			os.Exit(exitRewriteAllow)
		}
		// Handle bare commands with no trailing space/args (e.g. "ls" alone).
		if strings.TrimSpace(stripped) == strings.TrimSpace(rule.prefix) {
			rewritten := envPrefix + strings.TrimSpace(rule.yeetPrefix)
			fmt.Print(rewritten)
			os.Exit(exitRewriteAllow)
		}
	}

	os.Exit(exitNoMatch)
	return nil
}

// stripEnvPrefix splits "KEY=val KEY2=val2 cmd args" into ("cmd args", "KEY=val KEY2=val2 ").
// Returns the original string unchanged if no env prefix is found.
func stripEnvPrefix(cmd string) (stripped string, prefix string) {
	parts := strings.Fields(cmd)
	i := 0
	for i < len(parts) && strings.Contains(parts[i], "=") && !strings.HasPrefix(parts[i], "-") {
		i++
	}
	if i == 0 {
		return cmd, ""
	}
	prefix = strings.Join(parts[:i], " ") + " "
	stripped = strings.Join(parts[i:], " ")
	return stripped, prefix
}
