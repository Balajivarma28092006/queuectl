package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func Open() (*sql.DB, error) {
	return OpenAt("queue.db")
}

func OpenAt(path string) (*sql.DB, error) {
	dsn := path + "?_busy_timeout=5000&_journal_mode=WAL"
	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	if err := initSchema(database); err != nil {
		return nil, err
	}
	return database, err
}

func initSchema(database *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		command TEXT NOT NULL,
		state TEXT NOT NULL,
		attempts INTEGER NOT NULL DEFAULT 0,
		max_retries INTEGER NOT NULL DEFAULT 3,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		next_run_at INTEGER,
		worker_id TEXT
	);

	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`
	if _, err := database.Exec(schema); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}

	// can implement migrations but it can wait for now
	return nil
}

// Retry Dead jobs re-enqueus a DLQ job with a reset retry budget so thats why attempts are reset to 0 rather continuing it
func RetryDeadJobs(database *sql.DB, id string) error {
	res, err := database.Exec(`
		UPDATE jobs
		SET state = 'pending', attempts = 0, next_run_at = NULL,
		worker_id = NULL, updated_at = ?
		WHERE id = ? AND state = 'dead'
	`, toMillis(time.Now().UTC()), id)
	if err != nil {
		return err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("job %q not found in DLQ (must be state=dead)", id)
	}
	return nil
}
