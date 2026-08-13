package analytics

type CommandStats struct {
	CommandName   string
	TotalRuns     int
	CharsRaw      int
	CharsRendered int
	CharsSaved    int
	TokensSaved   int
}

type CommandUsages struct {
	CommandName string
	ArgsSummary string
}

func (d *DB) GetAllStats() ([]CommandStats, error) {
	rows, err := d.conn.Query(`
		SELECT command_name, total_runs, total_chars_raw, total_chars_rendered, total_chars_saved, total_tokens_saved
		FROM command_parents
		ORDER BY total_tokens_saved DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []CommandStats
	for rows.Next() {
		var s CommandStats
		if err := rows.Scan(&s.CommandName, &s.TotalRuns, &s.CharsRaw, &s.CharsRendered, &s.CharsSaved, &s.TokensSaved); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

func (d *DB) GetUsages() ([]CommandUsages, error) {
	rows, err := d.conn.Query(`
		SELECT command_name, args_summary 
		FROM command_parents as cmd_p join command_usages as cmd_u 
		ON cmd_p.id = cmd_u.command_parent_id 
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []CommandUsages
	for rows.Next() {
		var s CommandUsages
		if err := rows.Scan(&s.CommandName, &s.ArgsSummary); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

func (d *DB) ResetStats() error {
	_, err := d.conn.Exec("DELETE FROM command_usages; DELETE FROM command_parents;")
	return err
}

type FailureRow struct {
	ID        int
	Subcmd    string
	FullCmd   string
	ExitCode  int
	Stderr    string
	CreatedAt string
}

func (d *DB) GetFailures(limit int) ([]FailureRow, error) {
	rows, err := d.conn.Query(`
		SELECT id, subcmd, full_cmd, exit_code, stderr, created_at
		FROM command_failures
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var failures []FailureRow
	for rows.Next() {
		var f FailureRow
		if err := rows.Scan(&f.ID, &f.Subcmd, &f.FullCmd, &f.ExitCode, &f.Stderr, &f.CreatedAt); err != nil {
			return nil, err
		}
		failures = append(failures, f)
	}
	return failures, rows.Err()
}

func (d *DB) ClearFailures() error {
	_, err := d.conn.Exec("DELETE FROM command_failures;")
	return err
}

// AuditRow is one recorded invocation with everything needed to re-run the
// comparison by hand. Nothing here is derived or estimated at read time.
type AuditRow struct {
	Command      string
	BaselineCmd  string
	YeetCmd      string
	BaselineKind string
	CharsRaw     int
	CharsPrinted int
	CharsSaved   int
	CreatedAt    string
}

// GetAuditRows returns the most recent invocations, newest first. This is the
// answer to "where does that savings number come from" — every row names the
// exact baseline command it was measured against and how that baseline was
// obtained, so any figure can be checked rather than trusted.
func (d *DB) GetAuditRows(limit int) ([]AuditRow, error) {
	if limit <= 0 {
		limit = 25
	}
	rows, err := d.conn.Query(`
		SELECT p.command_name, u.baseline_cmd, u.yeet_cmd, u.baseline_kind,
		       u.chars_raw, u.chars_printed, u.chars_delta, u.created_at
		FROM command_usages u
		JOIN command_parents p ON p.id = u.command_parent_id
		ORDER BY u.id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditRow
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.Command, &r.BaselineCmd, &r.YeetCmd, &r.BaselineKind,
			&r.CharsRaw, &r.CharsPrinted, &r.CharsSaved, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountByBaselineKind reports how many stored rows fall into each baseline
// kind, so the share of the data that is trustworthy is visible rather than
// implied.
func (d *DB) CountByBaselineKind() (map[string]int, error) {
	rows, err := d.conn.Query(`
		SELECT CASE WHEN baseline_kind = '' THEN 'unlabelled' ELSE baseline_kind END AS k,
		       COUNT(*)
		FROM command_usages GROUP BY k`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}
