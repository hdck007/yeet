package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/spf13/cobra"
)

var depsCmd = &cobra.Command{
	Use:   "deps [path]",
	Short: "Summarize project dependencies from lock files and manifests",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDeps,
}

func init() {
	rootCmd.AddCommand(depsCmd)
}

func runDeps(cmd *cobra.Command, args []string) error {
	start := time.Now()

	dir := "."
	if len(args) > 0 {
		dir = args[0]
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("cannot access %s: %w", dir, err)
		}
		if !info.IsDir() {
			dir = "."
		}
	}

	var buf strings.Builder
	found := false

	// Go
	if data, err := os.ReadFile(dir + "/go.mod"); err == nil {
		found = true
		buf.WriteString("Go (go.mod):\n")
		buf.WriteString(summarizeGoMod(string(data)))
		buf.WriteString("\n")
	}

	// Node.js
	if data, err := os.ReadFile(dir + "/package.json"); err == nil {
		found = true
		buf.WriteString("Node.js (package.json):\n")
		buf.WriteString(summarizePackageJSON(data))
		buf.WriteString("\n")
	}

	// Python
	if data, err := os.ReadFile(dir + "/requirements.txt"); err == nil {
		found = true
		buf.WriteString("Python (requirements.txt):\n")
		buf.WriteString(summarizeRequirements(string(data)))
		buf.WriteString("\n")
	}

	// Rust
	if data, err := os.ReadFile(dir + "/Cargo.toml"); err == nil {
		found = true
		buf.WriteString("Rust (Cargo.toml):\n")
		buf.WriteString(summarizeCargoToml(string(data)))
		buf.WriteString("\n")
	}

	if !found {
		buf.WriteString("No known dependency files found (go.mod, package.json, requirements.txt, Cargo.toml)\n")
	}

	rendered := buf.String()
	// Raw = what you'd get reading all dep files individually with cat
	var rawBuf strings.Builder
	for _, f := range []string{dir + "/go.mod", dir + "/package.json", dir + "/requirements.txt", dir + "/Cargo.toml"} {
		if d, err2 := os.ReadFile(f); err2 == nil {
			rawBuf.Write(d)
		}
	}
	rawOutput := rawBuf.String()
	improved := printBetter(rawOutput, rendered)

	if improved && !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "deps",
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

var goRequireRE = regexp.MustCompile(`^\s*(\S+)\s+v[\d.]+`)

func summarizeGoMod(content string) string {
	var deps []string
	inRequire := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "require (" {
			inRequire = true
			continue
		}
		if inRequire && trimmed == ")" {
			inRequire = false
			continue
		}
		if inRequire || strings.HasPrefix(trimmed, "require ") {
			if m := goRequireRE.FindStringSubmatch(trimmed); m != nil {
				if !strings.Contains(m[1], "//") {
					deps = append(deps, "  "+trimmed)
				}
			}
		}
	}
	if len(deps) == 0 {
		return "  (no dependencies)\n"
	}
	return strings.Join(deps, "\n") + "\n"
}

type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func summarizePackageJSON(data []byte) string {
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "  (parse error)\n"
	}
	var buf strings.Builder
	if len(pkg.Dependencies) > 0 {
		buf.WriteString(fmt.Sprintf("  prod deps: %d\n", len(pkg.Dependencies)))
		for name, ver := range pkg.Dependencies {
			buf.WriteString(fmt.Sprintf("    %s@%s\n", name, ver))
		}
	}
	if len(pkg.DevDependencies) > 0 {
		buf.WriteString(fmt.Sprintf("  dev deps: %d\n", len(pkg.DevDependencies)))
		for name, ver := range pkg.DevDependencies {
			buf.WriteString(fmt.Sprintf("    %s@%s\n", name, ver))
		}
	}
	if buf.Len() == 0 {
		return "  (no dependencies)\n"
	}
	return buf.String()
}

func summarizeRequirements(content string) string {
	var deps []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		deps = append(deps, "  "+trimmed)
	}
	if len(deps) == 0 {
		return "  (no dependencies)\n"
	}
	return strings.Join(deps, "\n") + "\n"
}

var cargoDepsRE = regexp.MustCompile(`^\s*(\w[\w-]*)\s*=`)

func summarizeCargoToml(content string) string {
	var deps []string
	inDeps := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[dependencies]" || trimmed == "[dev-dependencies]" {
			inDeps = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inDeps = false
		}
		if inDeps && cargoDepsRE.MatchString(trimmed) {
			deps = append(deps, "  "+trimmed)
		}
	}
	if len(deps) == 0 {
		return "  (no dependencies)\n"
	}
	return strings.Join(deps, "\n") + "\n"
}
