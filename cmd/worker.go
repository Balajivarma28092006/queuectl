package cmd

import (
	"fmt"
	"os"
)

func HandleWorker() {
	fmt.Println("worker started")
}

func liveWorkerPIDs() []int {
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
