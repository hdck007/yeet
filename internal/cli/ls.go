package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var lsCmd = &cobra.Command{
	Use:                "ls [flags] [path]",
	Short:              "Token-optimized directory listing",
	DisableFlagParsing: true, // parse args manually like rtk to pass extra flags through
	RunE:               runLS,
}

func init() {
	rootCmd.AddCommand(lsCmd)
}

// parseLSArgs splits raw cobra args into (showAll, extraFlags, paths).
// It mirrors rtk's approach: always run ls -la, pass any extra short flags
// (stripping l/a/h which we always set), pass unknown long flags as-is.
var yeetPersistentFlags = map[string]bool{
	"--no-analytics": true,
	"--raw":          true,
}

func parseLSArgs(args []string) (showAll bool, wantsLong bool, extraFlags []string, paths []string) {
	for _, a := range args {
		if yeetPersistentFlags[a] {
			continue // strip yeet-owned flags before passing to system ls
		} else if a == "--all" {
			showAll = true
		} else if strings.HasPrefix(a, "--") {
			extraFlags = append(extraFlags, a)
		} else if strings.HasPrefix(a, "-") {
			stripped := strings.TrimLeft(a, "-")
			if strings.ContainsRune(stripped, 'a') {
				showAll = true
			}
			// Record the caller's intent before l/a/h are stripped below.
			// Whether they asked for the long format decides which native
			// command the saving is fairly measured against.
			if strings.ContainsRune(stripped, 'l') {
				wantsLong = true
			}
			// Keep any flags beyond l/a/h (e.g. R from -laR)
			extra := strings.Map(func(r rune) rune {
				if r == 'l' || r == 'a' || r == 'h' {
					return -1
				}
				return r
			}, stripped)
			if extra != "" {
				extraFlags = append(extraFlags, "-"+extra)
			}
		} else {
			paths = append(paths, a)
		}
	}
	return
}

func runLS(cmd *cobra.Command, args []string) error {
	showAll, wantsLong, extraFlags, paths := parseLSArgs(args)
	return runWithFallback("ls", args, func() error {
		return runLSImpl(showAll, wantsLong, extraFlags, paths)
	}, Fallback{
		Bin: "ls",
		Args: func(a []string) []string {
			if len(paths) == 0 {
				return append([]string{"-la"}, extraFlags...)
			}
			return append(append([]string{"-la"}, extraFlags...), paths...)
		},
	})
}

// lsNoiseDirs mirrors rtk's NOISE_DIRS constant.
var lsNoiseDirs = map[string]bool{
	"node_modules":  true,
	".git":          true,
	"target":        true,
	"__pycache__":   true,
	".next":         true,
	"dist":          true,
	"build":         true,
	".cache":        true,
	".turbo":        true,
	".vercel":       true,
	".pytest_cache": true,
	".mypy_cache":   true,
	".tox":          true,
	".venv":         true,
	"venv":          true,
	"env":           true,
	".env":          true,
	"coverage":      true,
	".nyc_output":   true,
	".DS_Store":     true,
	"Thumbs.db":     true,
	".idea":         true,
	".vscode":       true,
	".vs":           true,
	".eggs":         true,
	"vendor":        true,
}

func humanSize(bytes uint64) string {
	if bytes >= 1_048_576 {
		return fmt.Sprintf("%.1fM", float64(bytes)/1_048_576)
	} else if bytes >= 1024 {
		return fmt.Sprintf("%.1fK", float64(bytes)/1024)
	}
	return fmt.Sprintf("%dB", bytes)
}

// compactLS parses `ls -la` output (including recursive -laR) into compact format.
// Lines with <9 fields (like directory headers in -laR output) are skipped.
// Returns (entries, summary) so caller can suppress summary when piped.
func compactLS(raw string, showAll bool) (string, string) {
	var dirs []string
	var files []struct{ name, size string }
	byExt := map[string]int{}

	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "total ") || line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 9 {
			continue // skip dir headers in -R output (e.g. "./path:")
		}
		name := strings.Join(parts[8:], " ")
		if name == "." || name == ".." {
			continue
		}
		if !showAll && lsNoiseDirs[name] {
			continue
		}

		switch {
		case strings.HasPrefix(parts[0], "d"):
			dirs = append(dirs, name)
		case strings.HasPrefix(parts[0], "-") || strings.HasPrefix(parts[0], "l"):
			var sz uint64
			fmt.Sscan(parts[4], &sz)
			ext := "no ext"
			if i := strings.LastIndex(name, "."); i >= 0 {
				ext = name[i:]
			}
			byExt[ext]++
			files = append(files, struct{ name, size string }{name, humanSize(sz)})
		}
	}

	if len(dirs) == 0 && len(files) == 0 {
		return "(empty)\n", ""
	}

	var sb strings.Builder
	for _, d := range dirs {
		sb.WriteString(d)
		sb.WriteString("/\n")
	}
	for _, f := range files {
		sb.WriteString(f.name)
		sb.WriteString("  ")
		sb.WriteString(f.size)
		sb.WriteByte('\n')
	}
	entries := sb.String()

	summary := fmt.Sprintf("\nSummary: %d files, %d dirs", len(files), len(dirs))
	if len(byExt) > 0 {
		type kv struct {
			ext   string
			count int
		}
		var exts []kv
		for k, v := range byExt {
			exts = append(exts, kv{k, v})
		}
		sort.Slice(exts, func(i, j int) bool { return exts[i].count > exts[j].count })
		var parts []string
		for i, e := range exts {
			if i >= 5 {
				break
			}
			parts = append(parts, fmt.Sprintf("%d %s", e.count, e.ext))
		}
		summary += " (" + strings.Join(parts, ", ")
		if len(exts) > 5 {
			summary += fmt.Sprintf(", +%d more", len(exts)-5)
		}
		summary += ")"
	}
	summary += "\n"

	return entries, summary
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// hasLongFlag reports whether any pass-through flag requests the long listing
// format. Only short clusters are inspected: a long option such as --color
// contains an 'l' without asking for anything of the sort.
func hasLongFlag(flags []string) bool {
	for _, f := range flags {
		if strings.HasPrefix(f, "--") || !strings.HasPrefix(f, "-") {
			continue
		}
		if strings.ContainsRune(f[1:], 'l') {
			return true
		}
	}
	return false
}

func runLSImpl(showAll, wantsLong bool, extraFlags, paths []string) error {
	start := time.Now()

	targets := paths
	if len(targets) == 0 {
		targets = []string{"."}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// yeet needs the long format to report sizes, so it always runs `ls -la`
	// internally — but that output is not the baseline. `yeet ls` stands in for
	// whatever the caller actually typed, and `ls -la` prints far more than a
	// bare `ls`. Measuring against -la when a bare listing was asked for would
	// inflate the saving on every single call, so the baseline is the caller's
	// own command, actually run and actually measured.
	renderArgs := append(append([]string{"-la"}, extraFlags...), targets...)
	result := yeetexec.Run(ctx, "ls", renderArgs...)
	raw := result.Stdout

	baseline := raw
	baselineArgs := renderArgs
	if !wantsLong && !hasLongFlag(extraFlags) {
		plainArgs := append(append([]string{}, extraFlags...), targets...)
		if b := yeetexec.Run(ctx, "ls", plainArgs...); b.ExitCode == 0 {
			baseline = b.Stdout
			baselineArgs = plainArgs
		}
	}

	entries, summary := compactLS(raw, showAll)

	isTTY := isTerminal()
	filtered := entries
	if isTTY {
		filtered = entries + summary
	}
	// Compare against the baseline, not the internal `ls -la` output. Comparing
	// against -la would let a filtered form that is larger than the caller's own
	// command still win, which is how `yeet ls` ended up printing more than a
	// bare `ls` while reporting a saving.
	printed, _ := printBetterN(baseline, filtered)

	if !noAnalytics && db != nil {
		argsSummary := strings.Join(append(extraFlags, paths...), " ")
		if err := db.RecordUsage(analytics.Usage{
			Command:       "ls",
			ArgsSummary:   argsSummary,
			CharsRaw:      len(baseline),
			CharsRendered: len(filtered),
			CharsPrinted:  printed,
			BaselineCmd:   "ls " + strings.Join(baselineArgs, " "),
			YeetCmd:       "yeet ls " + argsSummary,
			BaselineKind:  analytics.BaselineAsInvoked,
			ExitCode:      0,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}

	return nil
}
