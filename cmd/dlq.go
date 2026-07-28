package cmd

import (
	"fmt"
	"os"

	"github.com/BalajiVarma28092006/queuectl/db"
)

func HandleDLQ() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: queuectl dlq <list|retry <id>>")
		os.Exit(1)
	}

	database, err := db.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer database.Close()

	switch os.Args[2] {
	case "list":
		jobs, err := db.GetJobsByState(database, string(Dead))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		if len(jobs) == 0 {
			fmt.Println("DLQ is Empty")
			return
		}
		for _, j := range jobs {
			fmt.Printf("%-20s attempts = %d ", j.ID, j.Attempts)
		}
	case "retry":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: queuectl dlq retry <id>")
			os.Exit(1)
		}

		if err := db.RetryDeadJobs(database, os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("re-enqueued %s\n", os.Args[3])
	default:
		fmt.Fprintln(os.Stderr, "Usage: queuectl dlq <list|retry <id>>")
		os.Exit(1)
	}
}
