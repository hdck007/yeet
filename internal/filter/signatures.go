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

// langSpec splits patterns by where a declaration may appear: topLevel matches
// only at indentation zero, member matches at any depth.
type langSpec struct {
	topLevel []*regexp.Regexp
	member   []*regexp.Regexp
}

var langSpecs = map[Language]langSpec{
	LangGo: {
		topLevel: []*regexp.Regexp{
			regexp.MustCompile(`^package\s+`),
			regexp.MustCompile(`^import\s`),
			regexp.MustCompile(`^func\s+`),
			regexp.MustCompile(`^type\s+`),
			regexp.MustCompile(`^var\s+`),
			regexp.MustCompile(`^const\s+`),
		},
	},
	LangRust: {
		topLevel: []*regexp.Regexp{
			regexp.MustCompile(`^pub\s+`),
			regexp.MustCompile(`^fn\s+`),
			regexp.MustCompile(`^struct\s+`),
			regexp.MustCompile(`^enum\s+`),
			regexp.MustCompile(`^trait\s+`),
			regexp.MustCompile(`^impl\s+`),
			regexp.MustCompile(`^mod\s+`),
			regexp.MustCompile(`^use\s+`),
		},
		member: []*regexp.Regexp{
			regexp.MustCompile(`^(pub\s+)?(async\s+)?fn\s+`),
		},
	},
	LangPython: {
		topLevel: []*regexp.Regexp{
			regexp.MustCompile(`^import\s+`),
			regexp.MustCompile(`^from\s+\S+\s+import`),
		},
		member: []*regexp.Regexp{
			regexp.MustCompile(`^@\w`),
			regexp.MustCompile(`^class\s+`),
			regexp.MustCompile(`^(async\s+)?def\s+`),
		},
	},
	LangRuby: {
		topLevel: []*regexp.Regexp{
			regexp.MustCompile(`^require(_relative)?\s+`),
		},
		member: []*regexp.Regexp{
			regexp.MustCompile(`^(class|module)\s+`),
			regexp.MustCompile(`^def\s+`),
			regexp.MustCompile(`^attr_(reader|writer|accessor)\b`),
			regexp.MustCompile(`^(has_many|has_one|belongs_to|has_and_belongs_to_many)\b`),
			regexp.MustCompile(`^(scope|validates|validate|before_\w+|after_\w+|around_\w+)\b`),
			regexp.MustCompile(`^(private|public|protected)\s*$`),
			regexp.MustCompile(`^(include|extend|prepend)\s+[A-Z]`),
		},
	},
	// Serves both TypeScript and JavaScript; .js and .jsx map here.
	LangTypeScript: {
		topLevel: []*regexp.Regexp{
			regexp.MustCompile(`^export\b`),
			regexp.MustCompile(`^import\s`),
			regexp.MustCompile(`^declare\s`),
			regexp.MustCompile(`^(async\s+)?function\s`),
			regexp.MustCompile(`^(abstract\s+)?class\s`),
			regexp.MustCompile(`^(interface|type|enum|namespace)\s`),
			regexp.MustCompile(`^(module\.exports|exports)\s*[.=]`),
			regexp.MustCompile(`^(const|let|var)\s+[\w$]+\s*(:[^=]+)?=\s*(async\s*)?(\([^)]*\)|[\w$]+)\s*=>`),
			regexp.MustCompile(`^(const|let|var)\s+[\w$]+\s*=\s*(async\s+)?function\b`),
			regexp.MustCompile(`^(const|let|var)\s+[\w$]+\s*=\s*require\(`),
		},
		member: []*regexp.Regexp{
			regexp.MustCompile(`^constructor\s*\(`),
			regexp.MustCompile(`^(get|set)\s+[\w$]+\s*\(`),
			regexp.MustCompile(`^(public|private|protected|static|abstract|async|override|readonly)\s`),
			regexp.MustCompile(`^[\w$]+\s*(<[^>]*>)?\s*\([^)]*\)\s*(:\s*[^{;]+)?\s*\{`),
		},
	},
}

func matchesSpec(spec langSpec, raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	if len(raw) == len(trimmed) {
		for _, p := range spec.topLevel {
			if p.MatchString(trimmed) {
				return true
			}
		}
	}
	for _, p := range spec.member {
		if p.MatchString(trimmed) {
			return true
		}
	}
	return false
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
	spec, ok := langSpecs[lang]
	if !ok {
		return content, false
	}

	var sigs []string
	for _, line := range strings.Split(content, "\n") {
		if matchesSpec(spec, line) {
			sigs = append(sigs, line)
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
	spec, found := langSpecs[lang]
	if !found {
		return nil, nil, false
	}

	for i, line := range strings.Split(content, "\n") {
		if matchesSpec(spec, line) {
			nums = append(nums, i+1)
			lines = append(lines, line)
		}
	}

	if len(lines) == 0 {
		return nil, nil, false
	}
	return nums, lines, true
}
