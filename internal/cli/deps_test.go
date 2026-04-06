package cli

import (
	"strings"
	"testing"
)

func TestSummarizeGoMod(t *testing.T) {
	input := `module example.com/foo

go 1.21

require (
	github.com/spf13/cobra v1.8.0
	github.com/mattn/go-sqlite3 v1.14.22
)
`
	result := summarizeGoMod(input)
	if !strings.Contains(result, "github.com/spf13/cobra") {
		t.Errorf("expected cobra in output, got: %s", result)
	}
	if !strings.Contains(result, "go-sqlite3") {
		t.Errorf("expected sqlite3 in output, got: %s", result)
	}
}

func TestSummarizePackageJSON(t *testing.T) {
	input := []byte(`{
		"dependencies": {
			"react": "^18.0.0",
			"next": "^14.0.0"
		},
		"devDependencies": {
			"typescript": "^5.0.0"
		}
	}`)
	result := summarizePackageJSON(input)
	if !strings.Contains(result, "prod deps: 2") {
		t.Errorf("expected 'prod deps: 2', got: %s", result)
	}
	if !strings.Contains(result, "dev deps: 1") {
		t.Errorf("expected 'dev deps: 1', got: %s", result)
	}
	if !strings.Contains(result, "react") {
		t.Errorf("expected 'react', got: %s", result)
	}
}

func TestSummarizeRequirements(t *testing.T) {
	input := `# requirements
requests==2.31.0
flask>=3.0.0
# comment
pytest
`
	result := summarizeRequirements(input)
	if !strings.Contains(result, "requests==2.31.0") {
		t.Errorf("expected requests, got: %s", result)
	}
	if !strings.Contains(result, "flask>=3.0.0") {
		t.Errorf("expected flask, got: %s", result)
	}
	if strings.Contains(result, "# requirements") {
		t.Errorf("comments should be excluded, got: %s", result)
	}
}

func TestSummarizeCargoToml(t *testing.T) {
	input := `[package]
name = "myapp"
version = "0.1.0"

[dependencies]
serde = { version = "1.0", features = ["derive"] }
tokio = "1.0"

[dev-dependencies]
tempfile = "3"
`
	result := summarizeCargoToml(input)
	if !strings.Contains(result, "serde") {
		t.Errorf("expected serde, got: %s", result)
	}
	if !strings.Contains(result, "tokio") {
		t.Errorf("expected tokio, got: %s", result)
	}
}
