package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var prismaCmd = &cobra.Command{
	Use:   "prisma [args...]",
	Short: "Prisma CLI — strips ASCII art and decoration",
	Args:  cobra.ArbitraryArgs,
	RunE:  runPrisma,
}

func init() {
	rootCmd.AddCommand(prismaCmd)
}

func runPrisma(cmd *cobra.Command, args []string) error {
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	var result yeetexec.Result
	if yeetexec.Available("prisma") {
		result = yeetexec.Run(ctx, "prisma", args...)
	} else {
		result = yeetexec.Run(ctx, "npx", append([]string{"prisma"}, args...)...)
	}

	raw := result.Stdout + result.Stderr
	rendered := filterPrismaOutput(raw, result.ExitCode)
	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "prisma",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(raw),
			CharsRendered: len(rendered),
			ExitCode:      result.ExitCode,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

// Prisma decoration patterns to strip
var prismaNoisePatterns = []string{
	"Prisma schema loaded",
	"✔ ", "✓ ", "◟ ", "◝ ", "◞ ", "◜ ",
	"Running ", "Applying ", "Already in ",
}

func filterPrismaOutput(output string, exitCode int) string {
	clean := ansiRE.ReplaceAllString(output, "")

	var lines []string
	for _, line := range strings.Split(clean, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip pure decoration/box-drawing lines
		if isPrismaDecoration(trimmed) {
			continue
		}
		lines = append(lines, trimmed)
	}

	if len(lines) == 0 {
		if exitCode == 0 {
			return "prisma: done\n"
		}
		return "prisma: error (no output)\n"
	}
	return strings.Join(lines, "\n") + "\n"
}

func isPrismaDecoration(line string) bool {
	// Box-drawing characters
	for _, r := range line {
		if r == '─' || r == '│' || r == '╔' || r == '╗' || r == '╚' || r == '╝' || r == '═' || r == '║' {
			return true
		}
	}
	// Spinner/progress noise
	spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	for _, s := range spinners {
		if strings.HasPrefix(line, s) {
			return true
		}
	}
	return false
}
