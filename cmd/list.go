package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/BalajiVarma28092006/queuectl/db"
)

func HandleList() {
	listCmd := flag.NewFlagSet("list", flag.ExitOnError)
	stateFilter := listCmd.String("state", "", "Filter jobs by state")
	jsonOutput := listCmd.Bool("json", false, "Output the result in JSON")

	_ = listCmd.Parse(os.Args[2:])

	if *stateFilter == "" {
		fmt.Fprintln(os.Stderr, "Usage: queuectl list --state <state> [--json]")
		os.Exit(1)
	}

	database, err := db.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer database.Close()

	jobs, err := db.GetJobsByState(database, *stateFilter)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *jsonOutput {
		out, _ := json.MarshalIndent(jobs, "", "  ")
		fmt.Println(string(out))
		return
	}

	for _, job := range jobs {
		fmt.Printf(
			"ID: %s\nCommand: %s\nState: %s\nAttempts: %d/%d\nCreated: %s\nUpdated: %s\n\n",
			job.ID,
			job.Command,
			job.State,
			job.Attempts,
			job.MaxRetries,
			job.CreatedAt.Format("2006-01-02 15:04:05"),
			job.UpdatedAt.Format("2006-01-02 15:04:05"),
		)
	}
}
