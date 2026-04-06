package filter

import (
	"fmt"
	"regexp"
	"strings"
)

type langDeclPattern struct {
	re    *regexp.Regexp
	label string // e.g., "struct", "fn", "class"
}

var declPatterns = map[Language][]langDeclPattern{
	LangGo: {
		{regexp.MustCompile(`^func\s+(\w+)`), ""},
		{regexp.MustCompile(`^func\s+\([^)]+\)\s+(\w+)`), ""},
		{regexp.MustCompile(`^type\s+(\w+)\s+struct`), "struct"},
		{regexp.MustCompile(`^type\s+(\w+)\s+interface`), "interface"},
		{regexp.MustCompile(`^type\s+(\w+)`), "type"},
		{regexp.MustCompile(`^var\s+(\w+)`), "var"},
		{regexp.MustCompile(`^const\s+(\w+)`), "const"},
	},
	LangRust: {
		{regexp.MustCompile(`^(?:pub\s+)?fn\s+(\w+)`), ""},
		{regexp.MustCompile(`^(?:pub\s+)?struct\s+(\w+)`), "struct"},
		{regexp.MustCompile(`^(?:pub\s+)?enum\s+(\w+)`), "enum"},
		{regexp.MustCompile(`^(?:pub\s+)?trait\s+(\w+)`), "trait"},
		{regexp.MustCompile(`^impl\s+(\w+)`), "impl"},
	},
	LangPython: {
		{regexp.MustCompile(`^def\s+(\w+)`), ""},
		{regexp.MustCompile(`^class\s+(\w+)`), "class"},
	},
	LangTypeScript: {
		{regexp.MustCompile(`^(?:export\s+)?function\s+(\w+)`), ""},
		{regexp.MustCompile(`^(?:export\s+)?class\s+(\w+)`), "class"},
		{regexp.MustCompile(`^(?:export\s+)?interface\s+(\w+)`), "interface"},
		{regexp.MustCompile(`^(?:export\s+)?type\s+(\w+)`), "type"},
		{regexp.MustCompile(`^(?:export\s+)?const\s+(\w+)`), "const"},
	},
}

func FileSummary(content string, filename string, fileSize int64) string {
	lang := DetectLanguage(filename)
	lines := strings.Split(content, "\n")
	lineCount := len(lines)

	langName := string(lang)
	if lang == LangUnknown {
		langName = "text"
	}

	sizeStr := formatSize(fileSize)
	line1 := fmt.Sprintf("%s source | %d lines | %s", langName, lineCount, sizeStr)

	// Extract declarations
	decls := extractDeclarations(content, lang)
	if len(decls) == 0 {
		return line1 + "\n(no declarations found)\n"
	}

	maxShow := 10
	shown := decls
	suffix := ""
	if len(decls) > maxShow {
		shown = decls[:maxShow]
		suffix = fmt.Sprintf(", ... and %d more", len(decls)-maxShow)
	}

	line2 := "exports: " + strings.Join(shown, ", ") + suffix
	return line1 + "\n" + line2 + "\n"
}

func extractDeclarations(content string, lang Language) []string {
	patterns, ok := declPatterns[lang]
	if !ok {
		return nil
	}

	lines := strings.Split(content, "\n")
	var decls []string
	seen := map[string]bool{}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		for _, p := range patterns {
			matches := p.re.FindStringSubmatch(trimmed)
			if len(matches) >= 2 {
				name := matches[1]
				if seen[name] {
					break
				}
				seen[name] = true
				entry := name
				if p.label != "" {
					entry = name + " (" + p.label + ")"
				}
				decls = append(decls, entry)
				break
			}
		}
	}
	return decls
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
