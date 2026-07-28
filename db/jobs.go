package db

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

type Job struct {
	ID             string     `json:"id"`
	Command        string     `json:"command"`
	State          string     `json:"state"`
	Attempts       int        `json:"attempts"`
	MaxRetries     int        `json:"max_retries"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	NextRunAt      *time.Time `json:"next_run_at,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	WorkerID       string     `json:"worker_id,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
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

const jobColumns = `id, command, state, attempts, max_retries, created_at, updated_at, next_run_at, lease_expires_at, worker_id, last_error`

func scanJob(row interface {
	Scan(dest ...any) error
}) (*Job, error) {
	var job Job
	var createdAt, updatedAt int64
	var nextRunAt, leaseExpiresAt sql.NullInt64
	var workerID, lastError sql.NullString

	err := row.Scan(
		&job.ID, &job.Command, &job.State, &job.Attempts, &job.MaxRetries,
		&createdAt, &updatedAt, &nextRunAt, &leaseExpiresAt, &workerID, &lastError,
	)
	if err != nil {
		return nil, err
	}

	job.CreatedAt = fromMillis(createdAt)
	job.UpdatedAt = fromMillis(updatedAt)
	job.NextRunAt = fromMillisPtr(nextRunAt)
	job.LeaseExpiresAt = fromMillisPtr(leaseExpiresAt)
	job.LastError = lastError.String
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

var ErrLostOwnership = errors.New("lost ownership of job: lease expired and job was reclaimed")

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

func ReapExpiredLeases(database *sql.DB) (int64, error) {
	now := toMillis(time.Now().UTC())
	res, err := database.Exec(`
		UPDATE jobs
		SET state = CASE
        	WHEN attempts + 1 >= max_retries THEN 'dead'
        	ELSE 'pending'
    	END,
    	attempts = attempts + 1,
    	worker_id = NULL,
    	lease_expires_at = NULL,
    	updated_at = ?
	WHERE state = 'processing'
  	AND lease_expires_at IS NOT NULL
  	AND lease_expires_at < ?`, now, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func MarkJobSuccess(database *sql.DB, id, workerID string) error {
	res, err := database.Exec(`
		UPDATE jobs
		SET state = 'completed', attempts = attempts + 1, updated_at = ?,
			lease_expires_at = NULL, last_error = NULL
		WHERE id = ? AND worker_id = ? AND state = 'processing'
		`, toMillis(time.Now().UTC()), id, workerID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLostOwnership
	}
	return nil
}

// next run will be after a failure is after backoff-base ^ attempts
func MarkJobFailure(database *sql.DB, id string, workerID, errorMsg string, backoffBase float64) error {
	var attempts, maxRetries int
	err := database.QueryRow(
		`SELECT attempts, max_retries FROM jobs WHERE id = ? AND worker_id = ? AND state = 'processing'`,
		id, workerID,
	).Scan(&attempts, &maxRetries)
	if err == sql.ErrNoRows {
		return ErrLostOwnership
	}
	if err != nil {
		return err
	}
	attempts++
	now := time.Now().UTC()

	if attempts >= maxRetries {
		res, err := database.Exec(`
		UPDATE jobs
		SET state = 'dead', attempts = ?, updated_at = ?,
			lease_expires_at = NULL, last_error = ?
		WHERE id = ? AND worker_id = ? AND state = 'processing'`,
			attempts, toMillis(now), errorMsg, id, workerID)
		if err != nil {
			return err
		}
		return checkOwned(res)
	}

	delaySeconds := math.Pow(backoffBase, float64(attempts))
	nextRun := now.Add(time.Duration(delaySeconds * float64(time.Second)))
	res, err := database.Exec(`
	UPDATE jobs
	SET state = 'failed', attempts = ?, updated_at = ?, next_run_at = ?,
		lease_expires_at = NULL, last_error = ?
	WHERE id = ? AND worker_id = ? AND state = 'processing'`,
		attempts, toMillis(now), toMillis(nextRun), errorMsg, id, workerID)
	if err != nil {
		return nil
	}
	return checkOwned(res)
}

func checkOwned(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLostOwnership
	}
	return nil
}

// renewLease extends a jobs lease_expires_at as long as the worker still owns it
func RenewLease(database *sql.DB, id, workerID string, leaseSeconds int) (bool, error) {
	newLease := toMillis(time.Now().UTC().Add(time.Duration(leaseSeconds) * time.Second))
	res, err := database.Exec(`
		UPDATE jobs
		SET lease_expires_at = ?
		WHERE id = ? AND worker_id = ? AND state = 'processing'`,
		newLease, id, workerID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func ClaimNextJob(database *sql.DB, workerID string, leaseSeconds int) (*Job, error) {
	now := time.Now().UTC()
	nowMs := toMillis(now)
	leaseMs := toMillis(now.Add(time.Duration(leaseSeconds) * time.Second))

	row := database.QueryRow(`
		UPDATE jobs
		SET state = 'processing', worker_id = ?, lease_expires_at = ?, updated_at = ?
		WHERE id = (
			SELECT id FROM jobs
			WHERE state = 'pending'
				OR (state = 'failed' AND (next_run_at IS NULL OR next_run_at <= ?))
			ORDER BY created_at ASC
			LIMIT 1
		)
		RETURNING
	`+jobColumns, workerID, leaseMs, nowMs, nowMs)

	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return job, nil
}
