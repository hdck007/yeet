package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/spf13/cobra"
)

var (
	jsonMaxDepth   int
	jsonSchemaOnly bool
)

var jsonCmd = &cobra.Command{
	Use:   "json <file>",
	Short: "Inspect JSON structure without verbose values",
	Args:  cobra.ExactArgs(1),
	RunE:  runJSON,
}

func init() {
	jsonCmd.Flags().IntVarP(&jsonMaxDepth, "depth", "d", 4, "Max nesting depth to show")
	jsonCmd.Flags().BoolVarP(&jsonSchemaOnly, "schema", "s", false, "Show schema/types only (no values)")
	rootCmd.AddCommand(jsonCmd)
}

func runJSON(cmd *cobra.Command, args []string) error {
	start := time.Now()
	file := args[0]

	// Reject obviously non-JSON files
	lower := strings.ToLower(file)
	for _, ext := range []string{".toml", ".yaml", ".yml", ".xml", ".csv", ".ini"} {
		if strings.HasSuffix(lower, ext) {
			return fmt.Errorf("%s is not a JSON file. Use `yeet read` for non-JSON files.", file)
		}
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", file, err)
	}

	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	var rendered string
	if jsonSchemaOnly {
		rendered = compactJSONSchema(v, 0, jsonMaxDepth) + "\n"
	} else {
		rendered = compactJSON(v, 0, jsonMaxDepth) + "\n"
	}

	fmt.Print(rendered)

	if !noAnalytics && db != nil {
		if err := db.RecordUsage(analytics.Usage{
			Command:       "json",
			ArgsSummary:   file,
			CharsRaw:      len(data),
			CharsRendered: len(rendered),
			ExitCode:      0,
			DurationMs:    time.Since(start).Milliseconds(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "yeet: analytics error: %v\n", err)
		}
	}
	return nil
}

func isSimpleValue(v interface{}) bool {
	switch v.(type) {
	case nil, bool, float64, string:
		return true
	}
	return false
}

// isInlineValue returns true if the value will render as a single line.
func isInlineValue(v interface{}) bool {
	if isSimpleValue(v) {
		return true
	}
	switch val := v.(type) {
	case []interface{}:
		if len(val) > 5 {
			return false
		}
		for _, item := range val {
			if !isSimpleValue(item) {
				return false
			}
		}
		return true
	case map[string]interface{}:
		if len(val) > 4 {
			return false
		}
		for _, child := range val {
			if !isSimpleValue(child) {
				return false
			}
		}
		return true
	}
	return false
}

// compactJSON renders a value as a compact string (rtk-style).
func compactJSON(v interface{}, depth, maxDepth int) string {
	indent := strings.Repeat("  ", depth)

	if depth > maxDepth {
		return indent + "..."
	}

	switch val := v.(type) {
	case nil:
		return indent + "null"
	case bool:
		return fmt.Sprintf("%s%v", indent, val)
	case float64:
		// Format without unnecessary decimal places
		if val == float64(int64(val)) {
			return fmt.Sprintf("%s%d", indent, int64(val))
		}
		return fmt.Sprintf("%s%g", indent, val)
	case string:
		if len(val) > 80 {
			return fmt.Sprintf("%s%q", indent, val[:77]+"...")
		}
		return fmt.Sprintf("%s%q", indent, val)

	case []interface{}:
		if len(val) == 0 {
			return indent + "[]"
		}
		// All simple? render inline.
		allSimple := true
		for _, item := range val {
			if !isSimpleValue(item) {
				allSimple = false
				break
			}
		}
		if allSimple && len(val) <= 5 {
			items := make([]string, len(val))
			for i, item := range val {
				items[i] = strings.TrimSpace(compactJSON(item, 0, maxDepth))
			}
			return fmt.Sprintf("%s[%s]", indent, strings.Join(items, ", "))
		}
		// Show first item as sample, then count the rest.
		first := strings.TrimSpace(compactJSON(val[0], depth+1, maxDepth))
		return fmt.Sprintf("%s[%s, ... +%d more]", indent, first, len(val)-1)

	case map[string]interface{}:
		if len(val) == 0 {
			return indent + "{}"
		}
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		// Small objects with all-simple values: render inline
		if len(val) <= 4 {
			allSimple := true
			for _, child := range val {
				if !isSimpleValue(child) {
					allSimple = false
					break
				}
			}
			if allSimple {
				parts := make([]string, 0, len(keys))
				for _, k := range keys {
					childStr := strings.TrimSpace(compactJSON(val[k], 0, maxDepth))
					parts = append(parts, fmt.Sprintf("%s: %s", k, childStr))
				}
				return fmt.Sprintf("%s{ %s }", indent, strings.Join(parts, ", "))
			}
		}

		var lines []string
		lines = append(lines, indent+"{")

		for i, k := range keys {
			if i >= 20 {
				lines = append(lines, fmt.Sprintf("%s  ... +%d more keys", indent, len(keys)-i))
				break
			}
			child := val[k]
			if isInlineValue(child) {
				childStr := strings.TrimSpace(compactJSON(child, 0, maxDepth))
				lines = append(lines, fmt.Sprintf("%s  %s: %s", indent, k, childStr))
			} else {
				// Nested: key on its own line, then value expanded one level deeper
				childStr := compactJSON(child, depth+1, maxDepth)
				lines = append(lines, fmt.Sprintf("%s  %s:", indent, k))
				lines = append(lines, childStr)
			}
		}
		lines = append(lines, indent+"}")
		return strings.Join(lines, "\n")
	}
	// Fallback
	b, _ := json.Marshal(v)
	return indent + string(b)
}

// compactJSONSchema renders types only, no values.
func compactJSONSchema(v interface{}, depth, maxDepth int) string {
	indent := strings.Repeat("  ", depth)

	if depth > maxDepth {
		return indent + "..."
	}

	switch val := v.(type) {
	case nil:
		return indent + "null"
	case bool:
		return indent + "bool"
	case float64:
		return indent + "number"
	case string:
		return indent + "string"
	case []interface{}:
		if len(val) == 0 {
			return indent + "array (empty)"
		}
		sample := compactJSONSchema(val[0], depth+1, maxDepth)
		return fmt.Sprintf("%sarray (%d items, sample:\n%s\n%s)", indent, len(val), sample, indent)
	case map[string]interface{}:
		if len(val) == 0 {
			return indent + "object (empty)"
		}
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var lines []string
		lines = append(lines, indent+"{")
		for _, k := range keys {
			childType := compactJSONSchema(val[k], depth+1, maxDepth)
			lines = append(lines, fmt.Sprintf("%s  %s: %s", indent, k, strings.TrimSpace(childType)))
		}
		lines = append(lines, indent+"}")
		return strings.Join(lines, "\n")
	}
	return indent + "unknown"
}
