package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var thresholdCmd = &cobra.Command{
	Use:   "threshold [N]",
	Short: "Get or set the big-file line threshold for yeet read",
	Long: `When yeet read opens a file with no filter flags and the line count exceeds
the threshold, it warns and stops instead of dumping the whole file.

  yeet threshold        # show current effective threshold
  yeet threshold 200    # persist a new threshold
  yeet threshold reset  # remove persisted value (falls back to env / default 150)`,
	Args: cobra.MaximumNArgs(1),
	RunE: runThreshold,
}

func init() {
	rootCmd.AddCommand(thresholdCmd)
}

func thresholdPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "yeet", "threshold"), nil
}

// PersistedThreshold returns the threshold stored by `yeet threshold N`, or 0
// if none is set. Used by getReadThreshold in read.go.
func PersistedThreshold() int {
	p, err := thresholdPath()
	if err != nil {
		return 0
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func runThreshold(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		fmt.Printf("threshold: %d\n", getReadThreshold())
		return nil
	}

	if args[0] == "reset" {
		p, err := thresholdPath()
		if err != nil {
			return err
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove config: %w", err)
		}
		fmt.Printf("threshold: reset (effective: %d)\n", getReadThreshold())
		return nil
	}

	n, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || n <= 0 {
		return fmt.Errorf("expected a positive integer, got %q", args[0])
	}
	if n < 100 {
		return fmt.Errorf("threshold must be at least 100 (got %d)", n)
	}

	p, err := thresholdPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(p, []byte(strconv.Itoa(n)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("threshold: %d\n", n)
	return nil
}
