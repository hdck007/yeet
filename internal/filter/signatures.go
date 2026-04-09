package filter

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type Language string

const (
	LangGo         Language = "Go"
	LangRust       Language = "Rust"
	LangPython     Language = "Python"
	LangTypeScript Language = "TypeScript"
	LangRuby       Language = "Ruby"
	LangUnknown    Language = "Unknown"
)

type FilterLevel int

const (
	FilterMinimal    FilterLevel = iota // line numbers only
	FilterModerate                      // strip comments, blank runs
	FilterAggressive                    // signatures only
)

func ParseFilterLevel(s string) FilterLevel {
	switch strings.ToLower(s) {
	case "aggressive", "agg", "a":
		return FilterAggressive
	case "moderate", "mod", "m":
		return FilterModerate
	case "minimal", "min", "":
		return FilterMinimal
	default:
		return FilterMinimal
	}
}

func (l FilterLevel) String() string {
	switch l {
	case FilterAggressive:
		return "aggressive"
	case FilterModerate:
		return "moderate"
	default:
		return "minimal"
	}
}

var extToLang = map[string]Language{
	".go":   LangGo,
	".rs":   LangRust,
	".py":   LangPython,
	".ts":   LangTypeScript,
	".tsx":  LangTypeScript,
	".js":   LangTypeScript,
	".jsx":  LangTypeScript,
	".java": LangUnknown,
	".c":    LangUnknown,
	".cpp":  LangUnknown,
	".h":    LangUnknown,
	".rb":   LangRuby,
}

var langPatterns = map[Language][]*regexp.Regexp{
	LangGo: {
		regexp.MustCompile(`^package\s+`),
		regexp.MustCompile(`^func\s+`),
		regexp.MustCompile(`^type\s+`),
		regexp.MustCompile(`^var\s+`),
		regexp.MustCompile(`^const\s+`),
	},
	LangRust: {
		regexp.MustCompile(`^pub\s+`),
		regexp.MustCompile(`^fn\s+`),
		regexp.MustCompile(`^struct\s+`),
		regexp.MustCompile(`^enum\s+`),
		regexp.MustCompile(`^trait\s+`),
		regexp.MustCompile(`^impl\s+`),
		regexp.MustCompile(`^mod\s+`),
		regexp.MustCompile(`^use\s+`),
	},
	LangPython: {
		regexp.MustCompile(`^def\s+`),
		regexp.MustCompile(`^class\s+`),
		regexp.MustCompile(`^import\s+`),
		regexp.MustCompile(`^from\s+`),
		regexp.MustCompile(`^async\s+def\s+`),
	},
	LangTypeScript: {
		regexp.MustCompile(`^export\s+`),
		regexp.MustCompile(`^function\s+`),
		regexp.MustCompile(`^class\s+`),
		regexp.MustCompile(`^interface\s+`),
		regexp.MustCompile(`^type\s+`),
		regexp.MustCompile(`^import\s+`),
		regexp.MustCompile(`^const\s+`),
		regexp.MustCompile(`^(public|private|protected|static|abstract|async|override|readonly)\s+`),
		regexp.MustCompile(`^(get|set)\s+\w+\s*\(`),
	},
	LangRuby: {
		regexp.MustCompile(`^def\s+`),
		regexp.MustCompile(`^class\s+`),
		regexp.MustCompile(`^module\s+`),
		regexp.MustCompile(`^attr_`),
	},
}

var commentPatterns = map[Language]*regexp.Regexp{
	LangGo:         regexp.MustCompile(`^\s*//`),
	LangRust:       regexp.MustCompile(`^\s*//`),
	LangPython:     regexp.MustCompile(`^\s*#`),
	LangTypeScript: regexp.MustCompile(`^\s*//`),
	LangRuby:       regexp.MustCompile(`^\s*#`),
}

func DetectLanguage(filename string) Language {
	ext := strings.ToLower(filepath.Ext(filename))
	if lang, ok := extToLang[ext]; ok {
		return lang
	}
	return LangUnknown
}

// FilterContent applies the given filter level to content.
// Returns (filtered content, whether filtering was applied).
func FilterContent(content string, lang Language, level FilterLevel) (string, bool) {
	switch level {
	case FilterAggressive:
		return extractSignatures(content, lang)
	case FilterModerate:
		return filterModerate(content, lang), true
	default:
		return content, false
	}
}

func extractSignatures(content string, lang Language) (string, bool) {
	patterns, ok := langPatterns[lang]
	if !ok {
		return content, false
	}

	lines := strings.Split(content, "\n")
	var sigs []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		for _, p := range patterns {
			if p.MatchString(trimmed) {
				sigs = append(sigs, line)
				break
			}
		}
	}

	if len(sigs) == 0 {
		return content, false
	}
	return strings.Join(sigs, "\n") + "\n", true
}

// filterModerate strips comment-only lines and collapses runs of blank lines.
func filterModerate(content string, lang Language) string {
	lines := strings.Split(content, "\n")
	commentRe := commentPatterns[lang]

	var out []string
	prevBlank := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Strip pure comment lines (keep inline comments)
		if commentRe != nil && commentRe.MatchString(line) {
			continue
		}

		// Collapse consecutive blank lines into one
		if trimmed == "" {
			if prevBlank {
				continue
			}
			prevBlank = true
			out = append(out, "")
			continue
		}

		prevBlank = false
		out = append(out, line)
	}

	return strings.Join(out, "\n")
}

// SmartTruncate keeps the first maxLines and appends a summary.
func SmartTruncate(content string, maxLines int, lang Language) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}

	kept := lines[:maxLines]
	remaining := len(lines) - maxLines
	return strings.Join(kept, "\n") + fmt.Sprintf("\n... (%d more lines)\n", remaining)
}

// TailLines returns the last n lines of content.
func TailLines(content string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= n {
		return content
	}
	start := len(lines) - n
	result := strings.Join(lines[start:], "\n")
	return result
}

// ExtractSignatures is the public API kept for backward compat with smart.go.
func ExtractSignatures(content string, lang Language) (string, bool) {
	return extractSignatures(content, lang)
}

// ExtractSignaturesWithLineNums returns the matched signature lines paired with
// their original 1-based line numbers. Used by the read command to always show
// line numbers in aggressive mode so callers can follow up with --lines N-M.
func ExtractSignaturesWithLineNums(content string, lang Language) (nums []int, lines []string, ok bool) {
	patterns, found := langPatterns[lang]
	if !found {
		return nil, nil, false
	}

	allLines := strings.Split(content, "\n")
	for i, line := range allLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		for _, p := range patterns {
			if p.MatchString(trimmed) {
				nums = append(nums, i+1)
				lines = append(lines, line)
				break
			}
		}
	}

	if len(lines) == 0 {
		return nil, nil, false
	}
	return nums, lines, true
}
