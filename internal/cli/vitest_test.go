package cli

import (
	"strings"
	"testing"
)

func TestExtractJSONObject(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`{"a":1}`, `{"a":1}`},
		{`prefix {"a":1} suffix`, `{"a":1}`},
		{`no json here`, ``},
		{`{"nested":{"x":1}}`, `{"nested":{"x":1}}`},
	}
	for _, c := range cases {
		got := extractJSONObject(c.input)
		if got != c.want {
			t.Errorf("extractJSONObject(%q)\n  got:  %q\n  want: %q", c.input, got, c.want)
		}
	}
}

func TestFormatVitestJSONAllPass(t *testing.T) {
	out := &vitestJSON{
		NumTotalTests:  5,
		NumPassedTests: 5,
		NumFailedTests: 0,
	}
	result := formatVitestJSON(out)
	if !strings.Contains(result, "all 5 tests passed") {
		t.Errorf("expected all passed message, got: %q", result)
	}
}

func TestFormatVitestJSONWithFailures(t *testing.T) {
	out := &vitestJSON{
		NumTotalTests:  3,
		NumPassedTests: 1,
		NumFailedTests: 2,
		TestResults: []vitestTestFile{
			{
				Name: "src/foo.test.ts",
				AssertionResults: []vitestResult{
					{Status: "passed", FullName: "foo passes"},
					{Status: "failed", FullName: "foo fails", FailureMessages: []string{"Expected 1 to equal 2"}},
				},
			},
		},
	}
	result := formatVitestJSON(out)
	if !strings.Contains(result, "[FAIL]") {
		t.Errorf("expected [FAIL] header, got: %q", result)
	}
	if !strings.Contains(result, "foo fails") {
		t.Errorf("expected failure name, got: %q", result)
	}
	if !strings.Contains(result, "Expected 1 to equal 2") {
		t.Errorf("expected failure message, got: %q", result)
	}
}

func TestFormatTestSummaryPlain(t *testing.T) {
	input := "\x1b[32mTests: 5 passed\x1b[0m\nDuration: 1.2s"
	result := formatTestSummaryPlain(input, true)
	// ANSI stripped
	if strings.Contains(result, "\x1b") {
		t.Errorf("ANSI codes should be stripped, got: %q", result)
	}
	if !strings.Contains(result, "5 passed") {
		t.Errorf("expected summary line, got: %q", result)
	}
}
