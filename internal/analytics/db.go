package analytics

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	conn *sql.DB
}

func Open() (*DB, error) {
	dbPath, err := dbFilePath()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create analytics dir: %w", err)
	}

	conn, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1")
	if err != nil {
		return nil, fmt.Errorf("open analytics db: %w", err)
	}

	conn.SetMaxOpenConns(1)

	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate analytics db: %w", err)
	}

	return &DB{conn: conn}, nil
}

func (d *DB) Close() error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

func (d *DB) Conn() *sql.DB {
	return d.conn
}

func dbFilePath() (string, error) {
	// YEET_DATA_DIR relocates the store. Tests use it to stay hermetic instead
	// of writing into the user's real analytics history, and it gives anyone
	// running yeet under a sandbox somewhere writable to point at.
	if dir := strings.TrimSpace(os.Getenv("YEET_DATA_DIR")); dir != "" {
		return filepath.Join(dir, "analytics.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "yeet", "analytics.db"), nil
}

func migrate(conn *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS command_parents (
		id                    INTEGER PRIMARY KEY AUTOINCREMENT,
		command_name          TEXT    NOT NULL UNIQUE,
		total_runs            INTEGER NOT NULL DEFAULT 0,
		total_chars_raw       INTEGER NOT NULL DEFAULT 0,
		total_chars_rendered  INTEGER NOT NULL DEFAULT 0,
		total_chars_saved     INTEGER NOT NULL DEFAULT 0,
		total_tokens_saved    INTEGER NOT NULL DEFAULT 0,
		created_at            TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		updated_at            TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	);

	CREATE TABLE IF NOT EXISTS command_usages (
		id                        INTEGER PRIMARY KEY AUTOINCREMENT,
		command_parent_id         INTEGER NOT NULL,
		args_summary              TEXT,
		chars_raw                 INTEGER NOT NULL,
		chars_rendered            INTEGER NOT NULL,
		chars_delta               INTEGER NOT NULL,
		tokens_estimated_raw      INTEGER NOT NULL,
		tokens_estimated_rendered INTEGER NOT NULL,
		exit_code                 INTEGER NOT NULL DEFAULT 0,
		duration_ms               INTEGER NOT NULL DEFAULT 0,
		created_at                TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		FOREIGN KEY (command_parent_id) REFERENCES command_parents(id)
	);

	CREATE INDEX IF NOT EXISTS idx_usages_parent ON command_usages(command_parent_id);
	CREATE INDEX IF NOT EXISTS idx_usages_created ON command_usages(created_at);

	CREATE TABLE IF NOT EXISTS command_failures (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		subcmd     TEXT    NOT NULL,
		full_cmd   TEXT    NOT NULL,
		exit_code  INTEGER NOT NULL,
		stderr     TEXT    NOT NULL DEFAULT '',
		created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	);

	CREATE INDEX IF NOT EXISTS idx_failures_created ON command_failures(created_at);

	-- Re-read suppression for 'yeet read'. Scoped by session_id because dedup is
	-- only correct within one conversation — a new session has never seen the
	-- file, so pointing it at "your earlier output" would point at nothing.
	-- view_key distinguishes renderings (-l aggressive vs --lines 10-20), since
	-- different views are different answers and must not suppress each other.
	CREATE TABLE IF NOT EXISTS read_cache (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id   TEXT    NOT NULL,
		path         TEXT    NOT NULL,
		view_key     TEXT    NOT NULL DEFAULT '',
		content_hash TEXT    NOT NULL,
		mtime_ns     INTEGER NOT NULL DEFAULT 0,
		size         INTEGER NOT NULL DEFAULT 0,
		render_chars INTEGER NOT NULL DEFAULT 0,
		render_lines INTEGER NOT NULL DEFAULT 0,
		seen_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		UNIQUE (session_id, path, view_key)
	);

	CREATE INDEX IF NOT EXISTS idx_read_cache_seen ON read_cache(seen_at);
	`
	if _, err := conn.Exec(schema); err != nil {
		return err
	}

	// Added later: the audit trail. Without these, a row records a saving but
	// not what it was measured against, so the number cannot be checked.
	//   baseline_cmd  — the native command the saving is measured against
	//   yeet_cmd      — the yeet command that actually ran
	//   baseline_kind — how the baseline was obtained (see analytics.Baseline*)
	//   chars_printed — what yeet actually wrote to stdout, which is not
	//                   chars_rendered whenever the raw-output fallback fired
	for _, col := range []struct{ name, decl string }{
		{"baseline_cmd", "TEXT NOT NULL DEFAULT ''"},
		{"yeet_cmd", "TEXT NOT NULL DEFAULT ''"},
		{"baseline_kind", "TEXT NOT NULL DEFAULT ''"},
		{"chars_printed", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := addColumnIfMissing(conn, "command_usages", col.name, col.decl); err != nil {
			return err
		}
	}
	return nil
}

// addColumnIfMissing is an idempotent ALTER TABLE — SQLite has no
// "ADD COLUMN IF NOT EXISTS", and databases from earlier versions are already
// out in the wild.
func addColumnIfMissing(conn *sql.DB, table, column, decl string) error {
	rows, err := conn.Query("SELECT 1 FROM pragma_table_info(?) WHERE name = ?", table, column)
	if err != nil {
		return fmt.Errorf("inspect %s.%s: %w", table, column, err)
	}
	present := rows.Next()
	rows.Close()
	if present {
		return nil
	}
	if _, err := conn.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + decl); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}
