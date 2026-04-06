package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	yeetexec "github.com/hdck007/yeet/internal/exec"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Rebuild and reinstall yeet from source",
	RunE:  runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	sourceDir, err := findSourceDir()
	if err != nil {
		return fmt.Errorf("cannot find yeet source: %w\nClone the repo and run 'yeet update' from within it", err)
	}

	fmt.Printf("Source: %s\n", sourceDir)

	fmt.Print("Building... ")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	buildResult := yeetexec.Run(ctx, "make", "-C", sourceDir, "build")
	if buildResult.ExitCode != 0 {
		fmt.Println("FAILED")
		fmt.Fprint(os.Stderr, buildResult.Stderr)
		return fmt.Errorf("build failed with exit code %d", buildResult.ExitCode)
	}
	fmt.Println("ok")

	builtBin := filepath.Join(sourceDir, "yeet")
	installPath := findInstalledBin(sourceDir)

	fmt.Printf("Installing to %s... ", installPath)
	copyResult := yeetexec.Run(ctx, "cp", builtBin, installPath)
	if copyResult.ExitCode != 0 {
		fmt.Println("FAILED (permission denied)")
		fmt.Printf("\nRun manually:\n  sudo cp %s %s\n", builtBin, installPath)
		return nil
	}
	fmt.Println("ok")

	verifyResult := yeetexec.Run(ctx, installPath, "version")
	fmt.Printf("Updated to: %s", verifyResult.Stdout)
	return nil
}

func findInstalledBin(sourceDir string) string {
	sourceDirAbs, _ := filepath.Abs(sourceDir)

	// Check PATH entries for yeet, skip the source directory
	pathEnv := os.Getenv("PATH")
	for _, dir := range strings.Split(pathEnv, ":") {
		candidate := filepath.Join(dir, "yeet")
		absDir, _ := filepath.Abs(dir)
		if absDir == sourceDirAbs {
			continue
		}
		if fileExists(candidate) {
			return candidate
		}
	}

	// Fallback to common locations
	for _, path := range []string{"/usr/local/bin/yeet", "/usr/bin/yeet"} {
		if fileExists(path) {
			return path
		}
	}

	return "/usr/local/bin/yeet"
}

func findSourceDir() (string, error) {
	if isYeetSource(".") {
		abs, _ := filepath.Abs(".")
		return abs, nil
	}

	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "Documents", "projects", "yeet"),
		filepath.Join(home, "projects", "yeet"),
		filepath.Join(home, "src", "yeet"),
	}

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = filepath.Join(home, "go")
	}
	candidates = append(candidates, filepath.Join(gopath, "src", "github.com", "hdck007", "yeet"))

	for _, dir := range candidates {
		if isYeetSource(dir) {
			return dir, nil
		}
	}

	return "", fmt.Errorf("searched %d locations on %s/%s", len(candidates), runtime.GOOS, runtime.GOARCH)
}

func isYeetSource(dir string) bool {
	gomod := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(gomod)
	if err != nil {
		return false
	}
	return len(data) > 0 && fileExists(filepath.Join(dir, "cmd", "yeet", "main.go"))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
