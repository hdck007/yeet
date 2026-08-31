package session

import (
	"os"
	"strings"
	"testing"
)

// An explicit override has to take effect on every call, because the ancestry
// walk is memoized and a test (or a user pinning a session) must still be able
// to change the answer afterwards.
func TestID_OverrideIsLiveAndNotMemoized(t *testing.T) {
	t.Setenv("YEET_SESSION_ID", "first")
	first := ID()
	if first == "" {
		t.Fatal("an explicit override must produce an id")
	}

	t.Setenv("YEET_SESSION_ID", "second")
	second := ID()
	if second == first {
		t.Error("changing YEET_SESSION_ID must change the id — the override is read every call")
	}

	// Same input, same id: the cache key has to be stable across processes.
	t.Setenv("YEET_SESSION_ID", "first")
	if ID() != first {
		t.Error("the same override must yield the same id")
	}
}

func TestID_OverrideIsHashedAndBounded(t *testing.T) {
	t.Setenv("YEET_SESSION_ID", "a-very-long-and-identifiable-session-name-with-details")
	got := ID()
	if len(got) != 16 {
		t.Errorf("id length = %d, want 16", len(got))
	}
	// The raw value must not leak through — ids end up in a shared database.
	if strings.Contains(got, "identifiable") {
		t.Errorf("id should be hashed, got %q", got)
	}
	for _, r := range got {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Errorf("id should be lowercase hex, got %q", got)
			break
		}
	}
}

func TestID_BlankOverrideFallsThroughToAncestry(t *testing.T) {
	// Whitespace is not an override. Treating it as one would key every session
	// in the environment to the same id and cross-suppress their reads.
	t.Setenv("YEET_SESSION_ID", "   ")
	blank := ID()

	t.Setenv("YEET_SESSION_ID", "explicit")
	explicit := ID()
	if blank == explicit {
		t.Error("a whitespace-only override must not be honoured")
	}
}

// The ancestry walk must either produce a stable id or nothing. An unstable id
// would scatter cache entries; a wrongly shared one would suppress output the
// reader has never seen.
func TestID_AncestryIsStableWithinAProcess(t *testing.T) {
	os.Unsetenv("YEET_SESSION_ID")
	a := ID()
	b := ID()
	if a != b {
		t.Errorf("ancestry id is not stable within one process: %q then %q", a, b)
	}
	if a != "" && len(a) != 16 {
		t.Errorf("a non-empty ancestry id should be 16 hex chars, got %q", a)
	}
}

func TestShort_IsDeterministicAndDistinguishing(t *testing.T) {
	if short("x") != short("x") {
		t.Error("short() must be deterministic")
	}
	if short("x") == short("y") {
		t.Error("different inputs must produce different ids")
	}
	if len(short("")) != 16 {
		t.Errorf("short(\"\") length = %d, want 16", len(short("")))
	}
}

// Shells are one-per-tool-call, so treating one as the session boundary would
// give every command its own id and defeat dedup entirely. `yeet` itself is in
// the set for the same reason.
func TestShells_CoverTheTransientProcesses(t *testing.T) {
	for _, name := range []string{"sh", "bash", "zsh", "fish", "dash", "yeet", "env"} {
		if !shells[name] {
			t.Errorf("%q should be treated as transient, not as the session", name)
		}
	}
	for _, name := range []string{"claude", "code", "node", "python3", "login"} {
		if shells[name] {
			t.Errorf("%q is a plausible session owner and must not be skipped", name)
		}
	}
}

func TestProcInfo_OnThisProcess(t *testing.T) {
	ppid, start, name, ok := procInfo(os.Getpid())
	if !ok {
		t.Skip("ps is unavailable or returned an unexpected shape on this platform")
	}
	if ppid <= 0 {
		t.Errorf("ppid = %d, want a positive pid", ppid)
	}
	if start == "" {
		t.Error("start time is what disambiguates a recycled pid; it must not be empty")
	}
	if name == "" {
		t.Error("process name should not be empty")
	}
	// The name is a base name, never a path — the shells map is keyed on it.
	if strings.Contains(name, "/") {
		t.Errorf("name = %q, want a base name", name)
	}
}

func TestProcInfo_RejectsAnImpossiblePid(t *testing.T) {
	if _, _, _, ok := procInfo(-1); ok {
		t.Error("procInfo(-1) reported success")
	}
}

func TestMaxHops_IsBounded(t *testing.T) {
	// An unbounded walk would climb into shared system processes, whose scope is
	// far too broad to key a read cache on.
	if maxHops <= 0 || maxHops > 32 {
		t.Errorf("maxHops = %d, want a small positive bound", maxHops)
	}
}
