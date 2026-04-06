package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/hdck007/yeet/internal/filter"
	"github.com/spf13/cobra"
)

var lsCmd = &cobra.Command{
	Use:   "ls [path]",
	Short: "Token-optimized directory tree",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runLS,
}

func init() {
	rootCmd.AddCommand(lsCmd)
}

func runLS(cmd *cobra.Command, args []string) error {
	start := time.Now()

	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	opts := filter.DefaultTreeOpts()
	tree, err := filter.BuildTree(path, opts)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	filter.RenderTree(&buf, tree)
	rendered := buf.String()

	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		// Raw baseline = actual recursive ls output (what an agent would get without yeet)
		rawOutput := rawLSOutput(path)
		if err := db.RecordUsage(analytics.Usage{
			Command:       "ls",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(rawOutput),
			CharsRendered: len(rendered),
			ExitCode:      0,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}

	return nil
}

func rawLSOutput(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := yeetexec.Run(ctx, "ls", "-laR", path)
	return result.Stdout
}
