package db

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

func Open() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "queue.db")
	if err != nil {
		return nil, err
	}

	createTable := `
	CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		command TEXT NOT NULL,
		state TEXT NOT NULL,
		attempts INTEGER NOT NULL,
		max_retries INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`

	_, err = db.Exec(createTable)
	if err != nil {
		return nil, err
	}
	return db, nil
}
