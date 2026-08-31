package readcache

import (
	"os"
	"path/filepath"
	"testing"
)

// hermetic points the store at a temp dir and pins a session, so tests never
// touch the user's real analytics history and never depend on process ancestry.
func hermetic(t *testing.T, sessionID string) {
	t.Helper()
	t.Setenv("YEET_DATA_DIR", t.TempDir())
	t.Setenv("YEET_SESSION_ID", sessionID)
	t.Setenv("YEET_NO_READ_CACHE", "")
}

func writeFile(t *testing.T, body string) (string, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.go")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return path, data
}

const view = "l=;lines=;max=0;tail=0;n=false"

func TestSuppressesSecondIdenticalRead(t *testing.T) {
	hermetic(t, "session-a")
	path, data := writeFile(t, "package main\n")

	if _, hit := Lookup(path, view, data); hit {
		t.Fatal("first read must not be suppressed — nothing has been shown yet")
	}
	Record(path, view, data, "package main\n")

	notice, hit := Lookup(path, view, data)
	if !hit {
		t.Fatal("second identical read should be suppressed")
	}
	if notice == "" {
		t.Fatal("a suppressed read must still say something")
	}
}

// The property that makes the whole thing safe: a conversation that has never
// seen the file must be shown the file, not a pointer to output it never got.
func TestDoesNotLeakAcrossSessions(t *testing.T) {
	dir := t.TempDir()
	path, data := writeFile(t, "package main\n")

	t.Setenv("YEET_DATA_DIR", dir)
	t.Setenv("YEET_SESSION_ID", "session-a")
	Record(path, view, data, "package main\n")
	if _, hit := Lookup(path, view, data); !hit {
		t.Fatal("same session should hit")
	}

	t.Setenv("YEET_SESSION_ID", "session-b")
	if _, hit := Lookup(path, view, data); hit {
		t.Fatal("a different session must never be suppressed")
	}
}

func TestChangedContentIsNotSuppressed(t *testing.T) {
	hermetic(t, "session-a")
	path, data := writeFile(t, "package main\n")
	Record(path, view, data, "package main\n")

	changed := []byte("package main\n\nfunc main() {}\n")
	if err := os.WriteFile(path, changed, 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, hit := Lookup(path, view, changed); hit {
		t.Fatal("content changed on disk — must not be suppressed")
	}
}

// A formatter can rewrite a file to byte-identical content and bump its mtime.
// Identity is the bytes, not the timestamp, so that must still suppress.
func TestIdenticalBytesAfterTouchStillSuppressed(t *testing.T) {
	hermetic(t, "session-a")
	path, data := writeFile(t, "package main\n")
	Record(path, view, data, "package main\n")

	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, hit := Lookup(path, view, data); !hit {
		t.Fatal("byte-identical rewrite should still be suppressed")
	}
}

// `-l aggressive` and a plain read are different answers to different
// questions; one must not stand in for the other.
func TestDifferentViewIsNotSuppressed(t *testing.T) {
	hermetic(t, "session-a")
	path, data := writeFile(t, "package main\n")
	Record(path, view, data, "package main\n")

	if _, hit := Lookup(path, "l=aggressive;lines=;max=0;tail=0;n=false", data); hit {
		t.Fatal("a different view must be rendered, not suppressed")
	}
}

func TestDisabledWithoutSession(t *testing.T) {
	t.Setenv("YEET_DATA_DIR", t.TempDir())
	t.Setenv("YEET_SESSION_ID", "session-a")
	path, data := writeFile(t, "package main\n")
	Record(path, view, data, "package main\n")

	// No session can be established (no override, and ancestry is resolved
	// once per process so we assert via the kill switch instead).
	t.Setenv("YEET_NO_READ_CACHE", "1")
	if Enabled() {
		t.Fatal("kill switch must disable the cache")
	}
	if _, hit := Lookup(path, view, data); hit {
		t.Fatal("disabled cache must never suppress")
	}
}

func TestViewKeyDistinguishesFlags(t *testing.T) {
	base := ViewKey("", "", 0, 0, false)
	for name, other := range map[string]string{
		"level":     ViewKey("aggressive", "", 0, 0, false),
		"lines":     ViewKey("", "10-20", 0, 0, false),
		"max-lines": ViewKey("", "", 50, 0, false),
		"tail":      ViewKey("", "", 0, 50, false),
		"numbers":   ViewKey("", "", 0, 0, true),
	} {
		if other == base {
			t.Errorf("%s must produce a distinct view key", name)
		}
	}
}
