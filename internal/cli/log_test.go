package cli

import (
	"strings"
	"testing"
)

func TestNormalizeLogLine(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			"2024-01-15T12:34:56 ERROR: something failed",
			"ERROR: something failed",
		},
		{
			"INFO: request id=550e8400-e29b-41d4-a716-446655440000 processed",
			"INFO: request id=<uuid> processed",
		},
		{
			"DEBUG: pointer 0xdeadbeef freed",
			"DEBUG: pointer <hex> freed",
		},
		{
			"ERROR: user 1234567890 not found",
			"ERROR: user <N> not found",
		},
	}

	for _, c := range cases {
		got := normalizeLogLine(c.input)
		if got != c.want {
			t.Errorf("normalizeLogLine(%q)\n  got:  %q\n  want: %q", c.input, got, c.want)
		}
	}
}

func TestAnalyzeLogContent(t *testing.T) {
	input := `2024-01-15T12:00:01 ERROR: connection refused
2024-01-15T12:00:02 ERROR: connection refused
2024-01-15T12:00:03 INFO: server started
2024-01-15T12:00:04 ERROR: connection refused`

	result := analyzeLogContent(input)

	if !strings.Contains(result, "4 lines") {
		t.Errorf("expected '4 lines' in output, got:\n%s", result)
	}
	if !strings.Contains(result, "2 unique") {
		t.Errorf("expected '2 unique' in output, got:\n%s", result)
	}
	if !strings.Contains(result, "x3") {
		t.Errorf("expected 'x3' count for repeated error, got:\n%s", result)
	}
}

func TestAnalyzeLogContentEmpty(t *testing.T) {
	result := analyzeLogContent("")
	if !strings.Contains(result, "0 lines") {
		t.Errorf("expected '0 lines', got: %q", result)
	}
}

func TestAnalyzeLogContentNoDuplicates(t *testing.T) {
	input := "INFO: one\nINFO: two\nINFO: three"
	result := analyzeLogContent(input)
	// All unique — no x-counts should appear
	if strings.Contains(result, "x2") || strings.Contains(result, "x3") {
		t.Errorf("no duplicates expected, got:\n%s", result)
	}
}
