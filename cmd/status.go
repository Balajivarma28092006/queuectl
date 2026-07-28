package cmd

import (
	"fmt"
	"os"

	"github.com/BalajiVarma28092006/queuectl/db"
)

type State string

const (
	Pending    State = "pending"
	Processing State = "processing"
	Failed     State = "Failed"
	Completed  State = "completed"
	Dead       State = "dead"
)

func HandleStatus() {
	database, err := db.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer database.Close()

	counts, err := db.StateCounts(database)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println("Jobs states")
	states := []State{Pending, Processing, Failed, Completed, Dead}
	for _, s := range states {
		count := 0
		if counts != nil {
			if v, ok := counts[string(s)]; ok {
				count = v
			}
		}
		fmt.Printf("%s: %d\n", s, count)
	}

	fmt.Printf("Workers (live): %d\n", len(liveWorkersPIDs()))
	for _, pid := range liveWorkersPIDs() {
		fmt.Printf(" pid %d\n", pid)
	}

	fmt.Printf("Config: max_retries=%d backoff_base=%v",
		db.EffectiveMaxRetries(database), db.EffectiveBackoffBase(database))
}

func liveWorkersPIDs() []int {
	entries, err := os.ReadDir(workersDir())
	if err != nil {
		return nil
	}

	var pids []int
	for _, e := range entries {
		var pid int
		if _, err := fmt.Sscanf(e.Name(), "%d.pid", &pid); err == nil {
			if p, err := os.FindProcess(pid); err == nil && p.Signal(nil) == nil {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}
