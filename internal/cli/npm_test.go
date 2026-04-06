package cli

import (
	"strings"
	"testing"
)

func TestNPMBuiltins(t *testing.T) {
	builtins := []string{"install", "i", "ci", "uninstall", "run", "test", "start"}
	for _, b := range builtins {
		if !npmBuiltins[b] {
			t.Errorf("expected %q to be a builtin npm command", b)
		}
	}
}

func TestFilterNPMOutputSuccess(t *testing.T) {
	input := `
added 42 packages in 3s
npm warn deprecated some-pkg: use other-pkg instead
`
	result := filterNPMOutput(input, 0)

	if !strings.Contains(result, "added 42 packages") {
		t.Errorf("expected 'added 42 packages', got: %s", result)
	}
	if !strings.Contains(result, "1 warnings") {
		t.Errorf("expected warning count, got: %s", result)
	}
}

func TestFilterNPMOutputFailure(t *testing.T) {
	input := `npm error code ENOENT
npm error syscall open
npm error path /nonexistent/package.json`

	result := filterNPMOutput(input, 1)

	if !strings.Contains(result, "[FAIL]") {
		t.Errorf("expected [FAIL] header, got: %s", result)
	}
	if !strings.Contains(result, "ENOENT") {
		t.Errorf("expected error details, got: %s", result)
	}
}

func TestFilterNPMOutputNoWarnings(t *testing.T) {
	input := "added 5 packages in 1s"
	result := filterNPMOutput(input, 0)
	if strings.Contains(result, "warnings") {
		t.Errorf("should not show warnings line when none present, got: %s", result)
	}
}
