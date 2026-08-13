// Package session derives a stable identifier for the agent conversation a
// yeet invocation belongs to, using process ancestry.
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

const maxHops = 12

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
			return short(strconv.Itoa(pid) + "|" + start + "|" + name)
		}
		pid = ppid
	}
	return ""
}

func procInfo(pid int) (ppid int, start, name string, ok bool) {
	out, err := exec.Command("ps", "-o", "ppid=,lstart=,comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, "", "", false
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
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
