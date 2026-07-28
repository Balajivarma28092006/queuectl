package db

import (
	"database/sql"
	"time"
)

type Job struct {
	ID         string     `json:"id"`
	Command    string     `json:"command"`
	State      string     `json:"state"`
	Attempts   int        `json:"attempts"`
	MaxRetries int        `json:"max_retries"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
	WorkerID   string     `json:"worker_id,omitempty"`
}

func toMillis(t time.Time) int64 { return t.UnixMilli() }

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func fromMillisPtr(ms sql.NullInt64) *time.Time {
	if !ms.Valid {
		return nil
	}
	t := fromMillis(ms.Int64)
	return &t
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
		toMillis(job.CreatedAt),
		toMillis(job.UpdatedAt),
	)
	return err
}

const jobColumns = `id, command, state, attempts, max_retries, created_at, updated_at, next_run_at, worker_id`

func scanJob(row interface {
	Scan(dest ...interface{}) error
}) (*Job, error) {
	var job Job
	var createdAt, updatedAt int64
	var nextRunAt sql.NullInt64
	var workerID sql.NullString

	err := row.Scan(
		&job.ID, &job.Command, &job.State, &job.Attempts, &job.MaxRetries,
		&createdAt, &updatedAt, &nextRunAt, &workerID,
	)
	if err != nil {
		return nil, err
	}

	job.CreatedAt = fromMillis(createdAt)
	job.UpdatedAt = fromMillis(updatedAt)
	job.NextRunAt = fromMillisPtr(nextRunAt)
	job.WorkerID = workerID.String
	return &job, nil
}

func GetJobsByState(database *sql.DB, state string) ([]Job, error) {
	rows, err := database.Query(`
		SELECT `+jobColumns+`
		FROM jobs
		WHERE state = ?`, state)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *job)
	}
	return jobs, rows.Err()
}

func StateCounts(database *sql.DB) (map[string]int, error) {
	rows, err := database.Query(`SELECT state, COUNT(*) FROM jobs GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		counts[state] = n
	}
	return counts, rows.Err()
}
