package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BalajiVarma28092006/queuectl/db"
)

func HandleEnqueue() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: queuectl enqueue <json_payload>")
		os.Exit(1)
	}

	// first store the json job in a payload variable then unmarshall to store it in a job
	jsonPayload := strings.Join(os.Args[2:], " ")

	var job db.Job
	if err := json.Unmarshal([]byte(jsonPayload), &job); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid JSON: %v\n", err)
		os.Exit(1)
	}
	validateInputs(job)
	now := time.Now().Round(time.Second)
	job.State = "pending"
	job.Attempts = 0

	if job.MaxRetries == 0 {
		job.MaxRetries = 3
	}

	job.CreatedAt = now
	job.UpdatedAt = now

	// the jobs are added in to the database as first come basis
	database, err := db.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	err = db.InsertJobToDB(database, db.Job{
		ID:         job.ID,
		Command:    job.Command,
		State:      job.State,
		Attempts:   job.Attempts,
		MaxRetries: job.MaxRetries,
		CreatedAt:  job.CreatedAt,
		UpdatedAt:  job.UpdatedAt,
	})

	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			fmt.Fprintln(os.Stderr, "Error: job with this ID already exists")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println("Job Enqueued Successfully ")
}

func validateInputs(job db.Job) {
	if strings.TrimSpace(job.ID) == "" {
		fmt.Fprintln(os.Stderr, "Error: job id is required")
		os.Exit(1)
	}

	if strings.TrimSpace(job.Command) == "" {
		fmt.Fprintln(os.Stderr, "Error: command is required")
		os.Exit(1)
	}
}
