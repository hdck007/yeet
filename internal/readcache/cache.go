// Package readcache gives `yeet read` the same re-read suppression that Claude
// Code's native Read tool has, which routing reads through Bash otherwise loses.
//
// Native Read keeps a per-conversation readFileState map. A second Read of an
// unchanged file returns a ~45-token system reminder instead of the content.
// Output piped through Bash gets no such treatment, so every `yeet read` of the
// same file pays full price. On a large unfiltered file that is the difference
// between ~45 and several thousand tokens.
//
// The scope rule is the whole design. Dedup is only ever correct *within one
// conversation*: a fresh session has never seen the file, so telling it "refer
// to your earlier output" would point at nothing. Entries are therefore keyed by
// internal/session, which derives the conversation from process ancestry rather
// than from any vendor environment variable, and a new session simply misses.
//
// Every failure path falls open — printing the file is always safe, suppressing
// it wrongly is not.
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

// retention bounds the table. Sessions are abandoned, not closed, so rows would
// otherwise accumulate forever.
const retention = 7 * 24 * time.Hour

// SessionID reports the conversation this read belongs to. Empty means one
// could not be established, and dedup must not apply.
func SessionID() string {
	return session.ID()
}

// Enabled reports whether suppression may run at all.
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

// ViewKey identifies which *rendering* of a file was shown. Two reads of the
// same unchanged file are only interchangeable if they asked for the same view;
// `-l aggressive` and `--lines 10-20` are different answers to different
// questions and must not suppress each other.
func ViewKey(level, lines string, maxLines, tail int, lineNums bool) string {
	return fmt.Sprintf("l=%s;lines=%s;max=%d;tail=%d;n=%t", level, lines, maxLines, tail, lineNums)
}

// hash is the identity of the bytes actually on disk. mtime and size are kept
// alongside it for diagnostics, but the hash is what decides: a file touched by
// a formatter that produced identical bytes has not changed for our purposes,
// which is the same call Claude Code makes.
func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type record struct {
	contentHash string
	renderChars int
	renderLines int
}

// Lookup reports whether this session has already been shown an identical
// render of identical bytes, and returns the notice to print instead.
//
// A miss, a disabled cache, or any error returns ("", false) so the caller
// prints the file.
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
		return "", false // sql.ErrNoRows, or anything else — fall open
	}
	if rec.contentHash != hash(content) {
		return "", false // changed on disk since we showed it
	}
	return notice(abs, rec), true
}

// notice is deliberately terse — it exists to be cheaper than the content it
// replaces, so it states the fact, the saving, and the escape hatch, and stops.
func notice(abs string, rec record) string {
	saved := token.EstimateTokens(rec.renderChars)
	return fmt.Sprintf(
		"yeet: %s unchanged since your last read in this session (%d lines). "+
			"Refer to that earlier output — re-reading was skipped, saving ~%d tokens. "+
			"Pass --no-cache to force a re-read.\n",
		abs, rec.renderLines, saved,
	)
}

// Record stores what was just printed, so the next identical read can be
// suppressed. Errors are ignored: failing to record costs tokens later, but
// never correctness.
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

// prune drops entries past the retention window. It runs on write rather than
// on a timer because yeet is a short-lived process with no background loop.
func prune(conn *sql.DB) {
	cutoff := time.Now().Add(-retention).UTC().Format("2006-01-02T15:04:05.000Z")
	_, _ = conn.Exec(`DELETE FROM read_cache WHERE seen_at < ?`, cutoff)
}
