package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var autoAllowCmd = &cobra.Command{
	Use:   "auto-allow [true|false]",
	Short: "Get or set auto-allow for yeet commands in Claude Code hooks",
	Long: `When auto-allow is enabled, the yeet PreToolUse hook grants permission
for all yeet commands without prompting Claude Code to ask the user.

  yeet auto-allow        # show current setting
  yeet auto-allow true   # enable  (no more permission prompts for yeet)
  yeet auto-allow false  # disable (Claude Code prompts as normal)`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAutoAllow,
}

func init() {
	rootCmd.AddCommand(autoAllowCmd)
}

func autoAllowPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "yeet", "auto-allow"), nil
}

// AutoAllowEnabled returns true if auto-allow is set to "true" in the config file.
// Exported so yeet rewrite can read it when needed.
func AutoAllowEnabled() bool {
	p, err := autoAllowPath()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(b)) == "true"
}

func runAutoAllow(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		if AutoAllowEnabled() {
			fmt.Println("auto-allow: true")
		} else {
			fmt.Println("auto-allow: false")
		}
		return nil
	}

	val := strings.ToLower(strings.TrimSpace(args[0]))
	if val != "true" && val != "false" {
		return fmt.Errorf("expected true or false, got %q", args[0])
	}

	p, err := autoAllowPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(p, []byte(val+"\n"), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("auto-allow: %s\n", val)
	return nil
}
