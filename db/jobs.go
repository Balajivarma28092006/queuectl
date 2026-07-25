package db

import (
	"database/sql"
	"time"
)

type Job struct {
	ID         string    `json:"id"`
	Command    string    `json:"command"`
	State      string    `json:"state"`
	Attempts   int       `json:"attempts"`
	MaxRetries int       `json:"max_retries"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func InsertJobToDB(database *sql.DB, job Job) error {
	query := `
	INSERT INTO jobs
	(id, command, state, attempts, max_retries, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := database.Exec(
		query,
		job.ID,
		job.Command,
		job.State,
		job.Attempts,
		job.MaxRetries,
		job.CreatedAt,
		job.UpdatedAt,
	)

	return err
}

func GetJobsByState(database *sql.DB, state string) ([]Job, error) {
	rows, err := database.Query(`
		SELECT id, command, state, attempts, max_retries, created_at, updated_at
		FROM jobs
		WHERE state = ?`, state)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job

	// as sqlite cannot parse time stamps, we read them to strings and then write to times
	var createAt, updatedAt string

	for rows.Next() {
		var job Job

		err := rows.Scan(
			&job.ID,
			&job.Command,
			&job.State,
			&job.Attempts,
			&job.MaxRetries,
			&createAt,
			&updatedAt,
		)

		if err != nil {
			return nil, err
		}
		const layout = "2006-01-02 15:04:05.999999999-07:00"

		job.CreatedAt, _ = time.Parse(layout, createAt)
		job.UpdatedAt, _ = time.Parse(layout, updatedAt)

		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}
