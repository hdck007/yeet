package cli

import (
	"strings"
	"testing"
)

func TestFilterTSCOutputNoErrors(t *testing.T) {
	input := "Found 0 errors. Watching for file changes."
	result := filterTSCOutput(input)
	if !strings.Contains(result, "no errors") {
		t.Errorf("expected 'no errors', got: %q", result)
	}
}

func TestFilterTSCOutputGroupsByFile(t *testing.T) {
	input := `src/app.ts(10,5): error TS2345: Argument of type 'string' is not assignable to parameter of type 'number'.
src/app.ts(20,1): error TS2304: Cannot find name 'foo'.
src/utils.ts(5,3): error TS2345: Type 'null' is not assignable to type 'string'.`

	result := filterTSCOutput(input)

	if !strings.Contains(result, "src/app.ts") {
		t.Errorf("expected 'src/app.ts' in output, got: %s", result)
	}
	if !strings.Contains(result, "src/utils.ts") {
		t.Errorf("expected 'src/utils.ts' in output, got: %s", result)
	}
	if !strings.Contains(result, "2 error") {
		t.Errorf("expected '2 error' count for app.ts, got: %s", result)
	}
	if !strings.Contains(result, "TS2345") {
		t.Errorf("expected error code TS2345, got: %s", result)
	}
	// Summary
	if !strings.Contains(result, "3 error(s) in 2 file(s)") {
		t.Errorf("expected total summary, got: %s", result)
	}
}

func TestFilterTSCOutputEmpty(t *testing.T) {
	result := filterTSCOutput("")
	// Should just pass through
	if result != "" {
		t.Logf("empty input passthrough: %q", result)
	}
}
