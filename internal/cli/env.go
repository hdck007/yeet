package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/spf13/cobra"
)

var (
	envFilter  string
	envShowAll bool
)

var envCmd = &cobra.Command{
	Use:   "env [filter]",
	Short: "Show filtered environment variables (hides secrets)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runEnv,
}

func init() {
	envCmd.Flags().BoolVarP(&envShowAll, "all", "a", false, "Show secret values unmasked")
	rootCmd.AddCommand(envCmd)
}

func runEnv(cmd *cobra.Command, args []string) error {
	start := time.Now()

	filter := ""
	if len(args) > 0 {
		filter = strings.ToLower(args[0])
	}

	sensitivePatterns := []string{
		"secret", "password", "passwd", "token", "key", "apikey", "api_key",
		"auth", "credential", "private", "cert", "pwd", "pass",
	}

	type envEntry struct{ key, val string }
	var all []envEntry
	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		all = append(all, envEntry{parts[0], parts[1]})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].key < all[j].key })

	var path, lang, cloud, tool, other []envEntry
	for _, e := range all {
		if filter != "" && !strings.Contains(strings.ToLower(e.key), filter) {
			continue
		}

		sensitive := false
		for _, p := range sensitivePatterns {
			if strings.Contains(strings.ToLower(e.key), p) {
				sensitive = true
				break
			}
		}

		val := e.val
		if sensitive && !envShowAll {
			if len(val) <= 4 {
				val = "***"
			} else {
				val = val[:2] + strings.Repeat("*", len(val)-4) + val[len(val)-2:]
				if len(val) > 20 {
					val = val[:10] + "..." + val[len(val)-6:]
				}
			}
		} else if len(val) > 100 {
			val = val[:50] + fmt.Sprintf("... (%d chars)", len(val))
		}

		entry := envEntry{e.key, val}
		switch {
		case strings.Contains(e.key, "PATH"):
			path = append(path, entry)
		case isLangVar(e.key):
			lang = append(lang, entry)
		case isCloudVar(e.key):
			cloud = append(cloud, entry)
		case isToolVar(e.key):
			tool = append(tool, entry)
		default:
			if filter != "" || isInterestingVar(e.key) {
				other = append(other, entry)
			}
		}
	}

	var buf strings.Builder
	printSection := func(title string, entries []envEntry) {
		if len(entries) == 0 {
			return
		}
		buf.WriteString(title + "\n")
		for _, e := range entries {
			buf.WriteString(fmt.Sprintf("  %-30s = %s\n", e.key, e.val))
		}
		buf.WriteString("\n")
	}

	printSection("[PATH]", path)
	printSection("[Language/Runtime]", lang)
	printSection("[Cloud]", cloud)
	printSection("[Tools]", tool)
	printSection("[Other]", other)

	rendered := buf.String()
	if rendered == "" {
		rendered = "(no matching variables)\n"
	}
	raw := strings.Join(os.Environ(), "\n")
	improved := printBetter(raw, rendered)

	if improved && !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "env",
			ArgsSummary:   strings.Join(args, " "),
			CharsRaw:      len(raw),
			CharsRendered: len(rendered),
			ExitCode:      0,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

func isLangVar(key string) bool {
	prefixes := []string{"GO", "RUST", "PYTHON", "RUBY", "NODE", "JAVA", "DOTNET", "DENO", "BUN"}
	up := strings.ToUpper(key)
	for _, p := range prefixes {
		if strings.HasPrefix(up, p) {
			return true
		}
	}
	return false
}

func isCloudVar(key string) bool {
	prefixes := []string{"AWS_", "GCP_", "AZURE_", "GOOGLE_", "CLOUDFLARE_", "DO_", "HEROKU_", "VERCEL_"}
	up := strings.ToUpper(key)
	for _, p := range prefixes {
		if strings.HasPrefix(up, p) {
			return true
		}
	}
	return false
}

func isToolVar(key string) bool {
	prefixes := []string{"DOCKER", "K8S", "KUBE", "HELM", "NPM", "PNPM", "CARGO", "GIT", "GITHUB", "CI", "CD"}
	up := strings.ToUpper(key)
	for _, p := range prefixes {
		if strings.HasPrefix(up, p) {
			return true
		}
	}
	return false
}

func isInterestingVar(key string) bool {
	interesting := []string{
		"HOME", "USER", "SHELL", "TERM", "EDITOR", "LANG", "LC_ALL",
		"HOSTNAME", "LOGNAME", "TMPDIR", "XDG_", "DISPLAY",
	}
	up := strings.ToUpper(key)
	for _, p := range interesting {
		if strings.HasPrefix(up, p) {
			return true
		}
	}
	return false
}
