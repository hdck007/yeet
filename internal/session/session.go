// Package session derives a stable identifier for the agent conversation that a
// yeet invocation belongs to, using process ancestry alone.
//
// Why not just read CLAUDE_CODE_SESSION_ID: it is an undocumented implementation
// detail of one vendor's CLI. It can be renamed or dropped in any release, it is
// absent under GitHub Copilot (which yeet also supports), and it is absent for a
// human at a terminal. Anything built on it inherits that fragility.
//
// Process ancestry is observable without anyone's cooperation. Agents run each
// shell command as a short-lived child of one long-lived agent process:
//
//	claude (69775, started 15:45:20)   <- stable for the whole conversation
//	 └─ sh -c "yeet read foo.go"       <- new PID per tool call
//	     └─ yeet
//
// So the nearest ancestor that is not a shell identifies the conversation. Its
// start time is carried alongside the PID because PIDs are recycled, and a
// recycled PID would otherwise inherit a dead conversation's cache.
//
// When no such ancestor exists the ID is empty and callers must not dedup.
// Guessing wrong in that direction suppresses output the reader has never seen.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// maxHops bounds the ancestry walk. Real chains are 2-4 deep; anything longer
// means we are lost and should give up rather than wander into shared
// system processes whose scope would be far too broad.
const maxHops = 12

// shells are transient — one per tool call — so they are never the session.
var shells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true,
	"ksh": true, "tcsh": true, "csh": true, "ash": true,
	"pwsh": true, "powershell": true, "powershell.exe": true, "cmd.exe": true,
	"env": true, "yeet": true,
}

var (
	once   sync.Once
	cached string
)

// ID returns the conversation identifier, or "" when one cannot be established.
//
// An explicit override is read every call so it stays live — pinning a session
// must take effect immediately, and tests depend on being able to switch. Only
// the ancestry walk is memoized, since it shells out to `ps` and cannot change
// for the lifetime of the process.
func ID() string {
	if v := strings.TrimSpace(os.Getenv("YEET_SESSION_ID")); v != "" {
		return short(v)
	}
	once.Do(func() { cached = fromAncestry() })
	return cached
}

func fromAncestry() string {
	pid := os.Getppid()
	for hop := 0; hop < maxHops && pid > 1; hop++ {
		ppid, start, name, ok := procInfo(pid)
		if !ok {
			return ""
		}
		if !shells[name] {
			// First non-shell ancestor: the agent (or, for a human, the
			// terminal session leader). Either is the right boundary.
			return short(strconv.Itoa(pid) + "|" + start + "|" + name)
		}
		pid = ppid
	}
	return ""
}

// procInfo reports a process's parent, start time and executable base name.
// `ps` is used rather than /proc so the same code path serves macOS and Linux.
func procInfo(pid int) (ppid int, start, name string, ok bool) {
	out, err := exec.Command("ps", "-o", "ppid=,lstart=,comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, "", "", false
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	// ppid | lstart (5 fields: Thu Aug 13 15:45:20 2026) | comm (may contain spaces)
	if len(fields) < 7 {
		return 0, "", "", false
	}
	ppid, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, "", "", false
	}
	start = strings.Join(fields[1:6], " ")
	name = filepath.Base(strings.Join(fields[6:], " "))
	return ppid, start, name, true
}

func short(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}
