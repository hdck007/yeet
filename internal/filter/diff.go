package filter

import (
	"fmt"
	"strings"
)

func CompactDiff(rawDiff string) string {
	lines := strings.Split(rawDiff, "\n")

	var out []string
	var added, removed, hunks int

	for _, line := range lines {
		// Skip diff headers with timestamps
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			// Keep file names but strip timestamps
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				out = append(out, parts[0]+"\t"+parts[1])
			}
			continue
		}

		if strings.HasPrefix(line, "@@") {
			hunks++
			out = append(out, line)
			continue
		}

		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added++
			out = append(out, line)
			continue
		}

		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removed++
			out = append(out, line)
			continue
		}

		// Context lines: keep only 1 line of context around changes
		if len(out) > 0 {
			lastLine := out[len(out)-1]
			if strings.HasPrefix(lastLine, "+") || strings.HasPrefix(lastLine, "-") || strings.HasPrefix(lastLine, "@@") {
				out = append(out, line)
			}
		}
	}

	summary := fmt.Sprintf("(+%d -%d lines in %d hunks)", added, removed, hunks)
	return strings.Join(out, "\n") + "\n" + summary + "\n"
}
