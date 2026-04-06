package cli

import (
	"strings"
	"testing"
)

func TestIsLangVar(t *testing.T) {
	cases := []struct{ key string; want bool }{
		{"GOPATH", true},
		{"GOROOT", true},
		{"RUSTUP_HOME", true},
		{"PYTHON_PATH", true},
		{"NODE_ENV", true},
		{"HOME", false},
		{"PATH", false},
	}
	for _, c := range cases {
		if got := isLangVar(c.key); got != c.want {
			t.Errorf("isLangVar(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestIsCloudVar(t *testing.T) {
	cases := []struct{ key string; want bool }{
		{"AWS_REGION", true},
		{"GCP_PROJECT", true},
		{"AZURE_TENANT_ID", true},
		{"GOOGLE_APPLICATION_CREDENTIALS", true},
		{"HOME", false},
	}
	for _, c := range cases {
		if got := isCloudVar(c.key); got != c.want {
			t.Errorf("isCloudVar(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestIsInterestingVar(t *testing.T) {
	cases := []struct{ key string; want bool }{
		{"HOME", true},
		{"SHELL", true},
		{"EDITOR", true},
		{"FOO_BAR", false},
	}
	for _, c := range cases {
		if got := isInterestingVar(c.key); got != c.want {
			t.Errorf("isInterestingVar(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestIsToolVar(t *testing.T) {
	cases := []struct{ key string; want bool }{
		{"DOCKER_HOST", true},
		{"GIT_AUTHOR", true},
		{"GITHUB_TOKEN", true},
		{"CI", true},
		{"FOO", false},
	}
	for _, c := range cases {
		if got := isToolVar(c.key); got != c.want {
			t.Errorf("isToolVar(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestEnvCmdSmoke(t *testing.T) {
	// Just verify the command is registered and runnable
	if envCmd == nil {
		t.Fatal("envCmd should be registered")
	}
	if !strings.Contains(envCmd.Use, "env") {
		t.Errorf("envCmd.Use should contain 'env', got %q", envCmd.Use)
	}
}
