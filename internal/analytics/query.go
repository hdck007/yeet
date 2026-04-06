package analytics

type CommandStats struct {
	CommandName    string
	TotalRuns      int
	CharsRaw       int
	CharsRendered  int
	CharsSaved     int
	TokensSaved    int
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

func (d *DB) ResetStats() error {
	_, err := d.conn.Exec("DELETE FROM command_usages; DELETE FROM command_parents;")
	return err
}
