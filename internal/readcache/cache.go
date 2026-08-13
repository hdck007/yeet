// Package readcache suppresses re-reads of unchanged files within one agent
// conversation, mirroring the native Read tool's readFileState dedup that is
// lost when reads are routed through the shell.
package readcache

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hdck007/yeet/internal/analytics"
	"github.com/hdck007/yeet/internal/session"
	"github.com/hdck007/yeet/internal/token"
)

const retention = 7 * 24 * time.Hour

// SessionID reports the conversation this read belongs to. Empty disables dedup.
func SessionID() string {
	return session.ID()
}

// Enabled reports whether suppression may run.
func Enabled() bool {
	if SessionID() == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("YEET_NO_READ_CACHE"))) {
	case "1", "true", "yes":
		return false
	}
	return true
}

// ViewKey identifies which rendering of a file was shown. Different views are
// different answers and must not suppress each other.
func ViewKey(level, lines string, maxLines, tail int, lineNums bool) string {
	return fmt.Sprintf("l=%s;lines=%s;max=%d;tail=%d;n=%t", level, lines, maxLines, tail, lineNums)
}

func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type record struct {
	contentHash string
	renderChars int
	renderLines int
}

// Lookup reports whether this session was already shown an identical render of
// identical bytes, returning the notice to print instead. Any failure returns a
// miss so the caller prints the file.
func Lookup(path, viewKey string, content []byte) (string, bool) {
	if !Enabled() {
		return "", false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	db, err := analytics.Open()
	if err != nil {
		return "", false
	}
	defer db.Close()

	var rec record
	row := db.Conn().QueryRow(
		`SELECT content_hash, render_chars, render_lines
		   FROM read_cache
		  WHERE session_id = ? AND path = ? AND view_key = ?`,
		SessionID(), abs, viewKey,
	)
	if err := row.Scan(&rec.contentHash, &rec.renderChars, &rec.renderLines); err != nil {
		return "", false
	}
	if rec.contentHash != hash(content) {
		return "", false
	}
	return notice(abs, rec), true
}

func notice(abs string, rec record) string {
	saved := token.EstimateTokens(rec.renderChars)
	return fmt.Sprintf(
		"yeet: %s unchanged since your last read in this session (%d lines). "+
			"Refer to that earlier output — re-reading was skipped, saving ~%d tokens. "+
			"Pass --no-cache to force a re-read.\n",
		abs, rec.renderLines, saved,
	)
}

// Record stores what was printed so the next identical read can be suppressed.
func Record(path, viewKey string, content []byte, rendered string) {
	if !Enabled() {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	db, err := analytics.Open()
	if err != nil {
		return
	}
	defer db.Close()

	var mtimeNs, size int64
	if info, err := os.Stat(abs); err == nil {
		mtimeNs = info.ModTime().UnixNano()
		size = info.Size()
	}

	renderLines := strings.Count(rendered, "\n")
	if renderLines == 0 && rendered != "" {
		renderLines = 1
	}

	_, _ = db.Conn().Exec(
		`INSERT INTO read_cache
		   (session_id, path, view_key, content_hash, mtime_ns, size, render_chars, render_lines, seen_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		 ON CONFLICT(session_id, path, view_key) DO UPDATE SET
		   content_hash = excluded.content_hash,
		   mtime_ns     = excluded.mtime_ns,
		   size         = excluded.size,
		   render_chars = excluded.render_chars,
		   render_lines = excluded.render_lines,
		   seen_at      = excluded.seen_at`,
		SessionID(), abs, viewKey, hash(content), mtimeNs, size,
		len(rendered), renderLines,
	)

	prune(db.Conn())
}

func prune(conn *sql.DB) {
	cutoff := time.Now().Add(-retention).UTC().Format("2006-01-02T15:04:05.000Z")
	_, _ = conn.Exec(`DELETE FROM read_cache WHERE seen_at < ?`, cutoff)
}
