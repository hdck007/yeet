package analytics

import (
	"github.com/hdck007/yeet/internal/token"
)

type Usage struct {
	Command       string
	ArgsSummary   string
	CharsRaw      int
	CharsRendered int
	ExitCode      int
	DurationMs    int64
}

func (d *DB) RecordUsage(u Usage) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	charsDelta := u.CharsRaw - u.CharsRendered
	tokensSaved := token.EstimateTokens(charsDelta)

	// Upsert command_parent
	_, err = tx.Exec(`
		INSERT INTO command_parents (command_name, total_runs, total_chars_raw, total_chars_rendered, total_chars_saved, total_tokens_saved)
		VALUES (?, 1, ?, ?, ?, ?)
		ON CONFLICT(command_name) DO UPDATE SET
			total_runs           = total_runs + 1,
			total_chars_raw      = total_chars_raw + excluded.total_chars_raw,
			total_chars_rendered = total_chars_rendered + excluded.total_chars_rendered,
			total_chars_saved    = total_chars_saved + excluded.total_chars_saved,
			total_tokens_saved   = total_tokens_saved + excluded.total_tokens_saved,
			updated_at           = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
	`, u.Command, u.CharsRaw, u.CharsRendered, charsDelta, tokensSaved)
	if err != nil {
		return err
	}

	// Get parent ID
	var parentID int64
	err = tx.QueryRow("SELECT id FROM command_parents WHERE command_name = ?", u.Command).Scan(&parentID)
	if err != nil {
		return err
	}

	// Insert usage row
	_, err = tx.Exec(`
		INSERT INTO command_usages (command_parent_id, args_summary, chars_raw, chars_rendered, chars_delta,
			tokens_estimated_raw, tokens_estimated_rendered, exit_code, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, parentID, u.ArgsSummary, u.CharsRaw, u.CharsRendered, charsDelta,
		token.EstimateTokens(u.CharsRaw), token.EstimateTokens(u.CharsRendered),
		u.ExitCode, u.DurationMs)
	if err != nil {
		return err
	}

	return tx.Commit()
}
