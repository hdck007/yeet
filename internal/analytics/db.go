package analytics

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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
	`
	_, err := conn.Exec(schema)
	return err
}
