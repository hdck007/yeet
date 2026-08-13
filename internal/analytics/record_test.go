package analytics

import (
	"testing"
)

// openTemp gives each test its own database. YEET_DATA_DIR is set explicitly
// rather than relying on HOME, so an inherited YEET_DATA_DIR cannot make these
// tests share a store or write into the real analytics history.
func openTemp(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("YEET_DATA_DIR", dir)
	t.Setenv("HOME", dir)
	db, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ─── Saved() ──────────────────────────────────────────────────────────────────

// The saving is baseline minus what actually reached stdout, floored at zero.
// A negative "saving" would quietly offset real wins recorded elsewhere.
func TestUsage_Saved(t *testing.T) {
	cases := []struct {
		name string
		u    Usage
		want int
	}{
		{"straightforward", Usage{CharsRaw: 1000, CharsPrinted: 200}, 800},
		{"no change", Usage{CharsRaw: 500, CharsPrinted: 500}, 0},
		{"output grew — floored at zero, never negative",
			Usage{CharsRaw: 100, CharsPrinted: 400}, 0},
		{"printed falls back to rendered when unset",
			Usage{CharsRaw: 1000, CharsRendered: 300}, 700},
		{"printed wins over rendered when both are set",
			Usage{CharsRaw: 1000, CharsRendered: 100, CharsPrinted: 900}, 100},
		{"empty", Usage{}, 0},
	}
	for _, c := range cases {
		if got := c.u.Saved(); got != c.want {
			t.Errorf("%s: Saved() = %d, want %d", c.name, got, c.want)
		}
	}
}

// This is the specific bug the honest-logging work fixed: when the filtered form
// came out larger, the raw output was printed but the *filtered* length was
// recorded, booking a saving that never reached the model.
func TestUsage_Saved_FallbackIsNotCountedAsAWin(t *testing.T) {
	u := Usage{CharsRaw: 420, CharsRendered: 100, CharsPrinted: 420}
	if got := u.Saved(); got != 0 {
		t.Errorf("a raw-output fallback saves nothing, got %d", got)
	}
}

// ─── RecordUsage ──────────────────────────────────────────────────────────────

func TestRecordUsage_AsInvokedCountsTowardTotals(t *testing.T) {
	db := openTemp(t)
	err := db.RecordUsage(Usage{
		Command: "ls", CharsRaw: 1000, CharsRendered: 200, CharsPrinted: 200,
		BaselineCmd: "ls .", YeetCmd: "yeet ls .", BaselineKind: BaselineAsInvoked,
	})
	if err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	stats, err := db.GetAllStats()
	if err != nil {
		t.Fatalf("GetAllStats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 command, got %d", len(stats))
	}
	s := stats[0]
	if s.TotalRuns != 1 {
		t.Errorf("TotalRuns = %d, want 1", s.TotalRuns)
	}
	if s.CharsRaw != 1000 || s.CharsSaved != 800 {
		t.Errorf("raw=%d saved=%d, want 1000/800", s.CharsRaw, s.CharsSaved)
	}
}

// A synthetic baseline means yeet had to run a *larger* form of the command, so
// the apparent saving overstates the truth. The row is still stored in full, but
// it must not inflate the headline numbers.
func TestRecordUsage_SyntheticIsStoredButNotCounted(t *testing.T) {
	db := openTemp(t)
	if err := db.RecordUsage(Usage{
		Command: "grep", CharsRaw: 260000, CharsRendered: 36000, CharsPrinted: 36000,
		BaselineCmd: "rg --json ...", BaselineKind: BaselineSynthetic,
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	stats, _ := db.GetAllStats()
	if len(stats) != 1 {
		t.Fatalf("expected the command to exist, got %d", len(stats))
	}
	if stats[0].TotalRuns != 1 {
		t.Errorf("the run should still be counted: TotalRuns = %d", stats[0].TotalRuns)
	}
	if stats[0].CharsSaved != 0 || stats[0].CharsRaw != 0 {
		t.Errorf("a synthetic baseline must contribute nothing to the totals, got raw=%d saved=%d",
			stats[0].CharsRaw, stats[0].CharsSaved)
	}

	// ...but the detail is preserved so it can be audited.
	rows, err := db.GetAuditRows(10)
	if err != nil {
		t.Fatalf("GetAuditRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(rows))
	}
	if rows[0].CharsRaw != 260000 {
		t.Errorf("the audit row should keep the real numbers, got %d", rows[0].CharsRaw)
	}
	if rows[0].BaselineKind != BaselineSynthetic {
		t.Errorf("kind = %q, want %q", rows[0].BaselineKind, BaselineSynthetic)
	}
}

// Rows written before baselines were tracked carry no kind. They are the
// untrustworthy sort by definition, so they must not count either.
func TestRecordUsage_UnlabelledIsTreatedAsUntrustworthy(t *testing.T) {
	db := openTemp(t)
	if err := db.RecordUsage(Usage{
		Command: "ls", CharsRaw: 5000, CharsRendered: 100, CharsPrinted: 100,
		// BaselineKind deliberately empty
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	stats, _ := db.GetAllStats()
	if stats[0].CharsSaved != 0 {
		t.Errorf("an unlabelled baseline must not be counted, got saved=%d", stats[0].CharsSaved)
	}
	kinds, err := db.CountByBaselineKind()
	if err != nil {
		t.Fatalf("CountByBaselineKind: %v", err)
	}
	if kinds[BaselineSynthetic] != 1 {
		t.Errorf("an unlabelled row should be stored as synthetic, got %v", kinds)
	}
}

func TestRecordUsage_NeverRecordsNegativeSavings(t *testing.T) {
	db := openTemp(t)
	// Output grew: 100 bytes in, 400 out.
	if err := db.RecordUsage(Usage{
		Command: "ls", CharsRaw: 100, CharsRendered: 400, CharsPrinted: 400,
		BaselineKind: BaselineAsInvoked,
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	stats, _ := db.GetAllStats()
	if stats[0].CharsSaved < 0 || stats[0].TokensSaved < 0 {
		t.Errorf("negative savings must be floored: saved=%d tokens=%d",
			stats[0].CharsSaved, stats[0].TokensSaved)
	}
	if stats[0].CharsSaved != 0 {
		t.Errorf("a command whose output grew saves nothing, got %d", stats[0].CharsSaved)
	}
}

func TestRecordUsage_AccumulatesAcrossRuns(t *testing.T) {
	db := openTemp(t)
	for i := 0; i < 3; i++ {
		if err := db.RecordUsage(Usage{
			Command: "git", CharsRaw: 1000, CharsPrinted: 100,
			BaselineKind: BaselineAsInvoked,
		}); err != nil {
			t.Fatalf("RecordUsage: %v", err)
		}
	}
	stats, _ := db.GetAllStats()
	if stats[0].TotalRuns != 3 {
		t.Errorf("TotalRuns = %d, want 3", stats[0].TotalRuns)
	}
	if stats[0].CharsSaved != 2700 {
		t.Errorf("CharsSaved = %d, want 2700", stats[0].CharsSaved)
	}
}

// ─── audit trail ──────────────────────────────────────────────────────────────

// The whole point of the audit columns: a stored figure can be traced back to
// the exact command it was measured against.
func TestGetAuditRows_PreservesTheCommandsCompared(t *testing.T) {
	db := openTemp(t)
	if err := db.RecordUsage(Usage{
		Command: "git", ArgsSummary: "diff HEAD~1",
		CharsRaw: 228768, CharsRendered: 866, CharsPrinted: 866,
		BaselineCmd: "git diff HEAD~1", YeetCmd: "yeet git diff HEAD~1",
		BaselineKind: BaselineAsInvoked,
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	rows, err := db.GetAuditRows(10)
	if err != nil {
		t.Fatalf("GetAuditRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.BaselineCmd != "git diff HEAD~1" {
		t.Errorf("BaselineCmd = %q — without it the figure cannot be checked", r.BaselineCmd)
	}
	if r.YeetCmd != "yeet git diff HEAD~1" {
		t.Errorf("YeetCmd = %q", r.YeetCmd)
	}
	if r.CharsRaw != 228768 || r.CharsPrinted != 866 || r.CharsSaved != 227902 {
		t.Errorf("row numbers wrong: raw=%d printed=%d saved=%d", r.CharsRaw, r.CharsPrinted, r.CharsSaved)
	}
	if r.CreatedAt == "" {
		t.Error("CreatedAt should be populated by the schema default")
	}
}

func TestGetAuditRows_NewestFirstAndRespectsLimit(t *testing.T) {
	db := openTemp(t)
	for _, cmd := range []string{"first", "second", "third"} {
		if err := db.RecordUsage(Usage{
			Command: cmd, CharsRaw: 100, CharsPrinted: 10, BaselineKind: BaselineAsInvoked,
		}); err != nil {
			t.Fatalf("RecordUsage: %v", err)
		}
	}
	rows, err := db.GetAuditRows(2)
	if err != nil {
		t.Fatalf("GetAuditRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("limit not respected: got %d rows, want 2", len(rows))
	}
	if rows[0].Command != "third" {
		t.Errorf("rows should be newest first, got %q", rows[0].Command)
	}
}

func TestGetAuditRows_EmptyDatabase(t *testing.T) {
	db := openTemp(t)
	rows, err := db.GetAuditRows(10)
	if err != nil {
		t.Fatalf("GetAuditRows on an empty db: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected no rows, got %d", len(rows))
	}
	kinds, err := db.CountByBaselineKind()
	if err != nil {
		t.Fatalf("CountByBaselineKind on an empty db: %v", err)
	}
	if len(kinds) != 0 {
		t.Errorf("expected no kinds, got %v", kinds)
	}
}

func TestCountByBaselineKind(t *testing.T) {
	db := openTemp(t)
	add := func(kind string, n int) {
		for i := 0; i < n; i++ {
			if err := db.RecordUsage(Usage{
				Command: "x", CharsRaw: 10, CharsPrinted: 1, BaselineKind: kind,
			}); err != nil {
				t.Fatalf("RecordUsage: %v", err)
			}
		}
	}
	add(BaselineAsInvoked, 3)
	add(BaselineSynthetic, 2)
	add(BaselineDirect, 1)

	kinds, err := db.CountByBaselineKind()
	if err != nil {
		t.Fatalf("CountByBaselineKind: %v", err)
	}
	if kinds[BaselineAsInvoked] != 3 || kinds[BaselineSynthetic] != 2 || kinds[BaselineDirect] != 1 {
		t.Errorf("counts wrong: %v", kinds)
	}
}

// ─── schema ───────────────────────────────────────────────────────────────────

// Databases from earlier versions are already on disk, so the migration has to
// be safe to run repeatedly against one that is already current.
func TestMigrate_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YEET_DATA_DIR", dir)
	t.Setenv("HOME", dir)
	for i := 0; i < 3; i++ {
		db, err := Open()
		if err != nil {
			t.Fatalf("Open on pass %d: %v", i+1, err)
		}
		if err := db.RecordUsage(Usage{
			Command: "ls", CharsRaw: 100, CharsPrinted: 10, BaselineKind: BaselineAsInvoked,
		}); err != nil {
			t.Fatalf("RecordUsage on pass %d: %v", i+1, err)
		}
		db.Close()
	}
	db, _ := Open()
	defer db.Close()
	rows, err := db.GetAuditRows(10)
	if err != nil {
		t.Fatalf("GetAuditRows after repeated migration: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows across 3 opens, got %d", len(rows))
	}
}

// A database created before the audit columns existed must gain them without
// losing the rows already in it.
func TestMigrate_UpgradesAnOlderDatabaseInPlace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YEET_DATA_DIR", dir)
	t.Setenv("HOME", dir)
	db, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// A parent row has to exist before a usage row can reference it.
	if err := db.RecordUsage(Usage{
		Command: "ls", CharsRaw: 10, CharsPrinted: 1, BaselineKind: BaselineAsInvoked,
	}); err != nil {
		t.Fatalf("seed a parent: %v", err)
	}

	// Drop the columns added later, which is the shape an older yeet left behind.
	conn := db.Conn()
	for _, col := range []string{"baseline_cmd", "yeet_cmd", "baseline_kind", "chars_printed"} {
		if _, err := conn.Exec("ALTER TABLE command_usages DROP COLUMN " + col); err != nil {
			t.Skipf("this SQLite build cannot drop columns (%v) — migration path covered by TestMigrate_IsIdempotent", err)
		}
	}
	if _, err := conn.Exec(`INSERT INTO command_usages
		(command_parent_id, args_summary, chars_raw, chars_rendered, chars_delta,
		 tokens_estimated_raw, tokens_estimated_rendered, exit_code, duration_ms)
		VALUES (1, 'legacy', 500, 100, 400, 125, 25, 0, 5)`); err != nil {
		t.Fatalf("seed a legacy row: %v", err)
	}
	db.Close()

	// Reopening runs the migration again; the legacy row must survive.
	db2, err := Open()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	rows, err := db2.GetAuditRows(10)
	if err != nil {
		t.Fatalf("GetAuditRows after upgrade: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.CharsRaw == 500 {
			found = true
			if r.BaselineKind != "" {
				t.Errorf("a legacy row should have no baseline kind, got %q", r.BaselineKind)
			}
		}
	}
	if !found {
		t.Error("the pre-existing row was lost by the migration")
	}
}

func TestRecordFailure(t *testing.T) {
	db := openTemp(t)
	if err := db.RecordFailure(Failure{
		Subcmd: "grep", FullCmd: "yeet grep -l x", ExitCode: 1, Stderr: "unknown flag",
	}); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	rows, err := db.GetFailures(10)
	if err != nil {
		t.Fatalf("GetFailures: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(rows))
	}
	if rows[0].Subcmd != "grep" {
		t.Errorf("Subcmd = %q, want grep", rows[0].Subcmd)
	}
}

func TestResetStats(t *testing.T) {
	db := openTemp(t)
	if err := db.RecordUsage(Usage{
		Command: "ls", CharsRaw: 100, CharsPrinted: 10, BaselineKind: BaselineAsInvoked,
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if err := db.ResetStats(); err != nil {
		t.Fatalf("ResetStats: %v", err)
	}
	stats, _ := db.GetAllStats()
	if len(stats) != 0 {
		t.Errorf("stats should be empty after a reset, got %d commands", len(stats))
	}
	rows, _ := db.GetAuditRows(10)
	if len(rows) != 0 {
		t.Errorf("audit rows should be cleared by a reset, got %d", len(rows))
	}
}
