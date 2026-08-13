package analytics

import (
	"github.com/hdck007/yeet/internal/token"
)

// How the baseline (the "without yeet" number) was obtained. Every recorded
// saving is measured against a baseline, so the log has to say which kind it
// was — otherwise the number cannot be audited.
const (
	// BaselineAsInvoked: the baseline is the native command equivalent to what
	// the caller actually asked for, measured byte-for-byte. Trustworthy.
	BaselineAsInvoked = "as-invoked"

	// BaselineSynthetic: yeet had to run a *different* form of the command to
	// do its job (extra flags for parseable output, a recursive search for a
	// non-recursive request), so the raw output is larger than what the caller
	// would have seen. Savings from these rows overstate the truth and are
	// reported separately rather than mixed into the totals.
	BaselineSynthetic = "synthetic"

	// BaselineDirect: no native command was involved — yeet read the file or
	// computed the answer itself. The baseline is the full content that a plain
	// read would have produced.
	BaselineDirect = "direct"
)

type Usage struct {
	Command     string
	ArgsSummary string

	// CharsRaw is the size of the baseline output.
	CharsRaw int
	// CharsRendered is the size of the output yeet produced.
	CharsRendered int
	// CharsPrinted is what yeet actually wrote to stdout. It differs from
	// CharsRendered whenever the raw-output fallback fired because the filtered
	// form came out larger. Savings are computed from this, not from
	// CharsRendered, so a fallback cannot be logged as a win.
	CharsPrinted int

	// BaselineCmd and YeetCmd record the exact commands compared, so any row
	// can be re-run by hand and checked.
	BaselineCmd  string
	YeetCmd      string
	BaselineKind string

	ExitCode   int
	DurationMs int64
}

// printed falls back to CharsRendered for callers that have not been updated to
// report what they actually wrote.
func (u Usage) printed() int {
	if u.CharsPrinted > 0 {
		return u.CharsPrinted
	}
	return u.CharsRendered
}

// Saved is the honest saving for this row: baseline minus what was actually
// printed, floored at zero. A command whose output grew saves nothing; it does
// not "save" a negative amount that quietly offsets real wins elsewhere.
func (u Usage) Saved() int {
	d := u.CharsRaw - u.printed()
	if d < 0 {
		return 0
	}
	return d
}

type Failure struct {
	Subcmd   string
	FullCmd  string
	ExitCode int
	Stderr   string
}

func (d *DB) RecordFailure(f Failure) error {
	_, err := d.conn.Exec(`
		INSERT INTO command_failures (subcmd, full_cmd, exit_code, stderr)
		VALUES (?, ?, ?, ?)
	`, f.Subcmd, f.FullCmd, f.ExitCode, f.Stderr)
	return err
}

func (d *DB) RecordUsage(u Usage) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	printed := u.printed()
	charsSaved := u.Saved()
	tokensSaved := token.EstimateTokens(u.CharsRaw) - token.EstimateTokens(printed)
	if tokensSaved < 0 {
		tokensSaved = 0
	}

	kind := u.BaselineKind
	if kind == "" {
		kind = BaselineSynthetic // unlabelled rows are the untrustworthy kind
	}

	// Only baselines we can stand behind contribute to the headline totals.
	// Synthetic rows are still stored in full, just not counted.
	countedRaw, countedPrinted, countedSaved, countedTokens := u.CharsRaw, printed, charsSaved, tokensSaved
	if kind == BaselineSynthetic {
		countedRaw, countedPrinted, countedSaved, countedTokens = 0, 0, 0, 0
	}

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
	`, u.Command, countedRaw, countedPrinted, countedSaved, countedTokens)
	if err != nil {
		return err
	}

	var parentID int64
	err = tx.QueryRow("SELECT id FROM command_parents WHERE command_name = ?", u.Command).Scan(&parentID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO command_usages (command_parent_id, args_summary, chars_raw, chars_rendered, chars_delta,
			tokens_estimated_raw, tokens_estimated_rendered, exit_code, duration_ms,
			baseline_cmd, yeet_cmd, baseline_kind, chars_printed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, parentID, u.ArgsSummary, u.CharsRaw, u.CharsRendered, charsSaved,
		token.EstimateTokens(u.CharsRaw), token.EstimateTokens(printed),
		u.ExitCode, u.DurationMs,
		u.BaselineCmd, u.YeetCmd, kind, printed)
	if err != nil {
		return err
	}

	return tx.Commit()
}
