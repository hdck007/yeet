package ignore

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

var hardcodedIgnores = map[string]bool{
	".git":         true,
	"node_modules": true,
	"__pycache__":  true,
	".venv":        true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"target":       true,
	".next":        true,
	".cache":       true,
	".DS_Store":    true,
}

type Matcher struct {
	patterns []pattern
}

type pattern struct {
	glob     string
	negated  bool
	dirOnly  bool
	rootOnly bool // pattern had leading / — only match at root level
	fileOnly bool // no wildcards, no trailing / — likely a specific file (e.g., binary name)
}

func NewMatcher(root string) *Matcher {
	m := &Matcher{}
	m.loadGitignore(filepath.Join(root, ".gitignore"))
	return m
}

func (m *Matcher) ShouldIgnore(name string, isDir bool) bool {
	return m.ShouldIgnoreAt(name, isDir, false)
}

func (m *Matcher) ShouldIgnoreAt(name string, isDir bool, isRoot bool) bool {
	if hardcodedIgnores[name] {
		return true
	}

	for i := len(m.patterns) - 1; i >= 0; i-- {
		p := m.patterns[i]
		if p.dirOnly && !isDir {
			continue
		}
		// fileOnly patterns (e.g., "yeet" for binary) should not match directories
		if p.fileOnly && isDir {
			continue
		}
		// rootOnly patterns only match entries at the root level
		if p.rootOnly && !isRoot {
			continue
		}
		matched, _ := filepath.Match(p.glob, name)
		if matched {
			return !p.negated
		}
	}
	return false
}

func (m *Matcher) loadGitignore(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		p := pattern{}
		if strings.HasPrefix(line, "!") {
			p.negated = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		// Leading slash means root-relative only
		if strings.HasPrefix(line, "/") {
			p.rootOnly = true
			line = strings.TrimPrefix(line, "/")
		}
		// A plain name without wildcards or trailing slash is likely a file (e.g., binary)
		if !p.dirOnly && !strings.ContainsAny(line, "*?[/") {
			p.fileOnly = true
		}
		p.glob = line
		m.patterns = append(m.patterns, p)
	}
}
