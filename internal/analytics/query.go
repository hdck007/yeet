package analytics

type CommandStats struct {
	CommandName    string
	TotalRuns      int
	CharsRaw       int
	CharsRendered  int
	CharsSaved     int
	TokensSaved    int
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
