package cli

import (
	"strings"
	"testing"
)

func TestFilterWCOutputSingleFileFull(t *testing.T) {
	// wc with no flags outputs: lines words chars [file]
	input := "      30      96     978 file.go\n"
	result := filterWCOutput(input, []string{"file.go"})
	if !strings.Contains(result, "30L") {
		t.Errorf("expected '30L', got: %q", result)
	}
	if !strings.Contains(result, "96W") {
		t.Errorf("expected '96W', got: %q", result)
	}
	if !strings.Contains(result, "978B") {
		t.Errorf("expected '978B', got: %q", result)
	}
}

func TestFilterWCOutputLinesOnly(t *testing.T) {
	input := "      30 file.go\n"
	result := filterWCOutput(input, []string{"-l", "file.go"})
	if !strings.Contains(result, "30L") {
		t.Errorf("expected '30L', got: %q", result)
	}
}

func TestFilterWCOutputMultipleFiles(t *testing.T) {
	input := "  10  20  100 a.go\n  20  40  200 b.go\n  30  60  300 total\n"
	result := filterWCOutput(input, []string{"a.go", "b.go"})
	// Multi-file: should show tabular output
	if !strings.Contains(result, "10") || !strings.Contains(result, "20") {
		t.Errorf("expected multiple file counts, got: %q", result)
	}
}
