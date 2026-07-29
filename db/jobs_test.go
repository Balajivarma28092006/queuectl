package db

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func TestClaimedNextJob_ExactlyOnce(t *testing.T) {
	dbPath := "test_claim.db"
	cleanup := func() {
		os.Remove(dbPath)
		os.Remove(dbPath + "-shm")
		os.Remove(dbPath + "-wal")
	}
	cleanup()
	defer cleanup()

	database, err := OpenAt(dbPath)
	if err != nil {
		t.Fatalf("opne; %v", err)
	}
	defer database.Close()

	const numJobs = 50
	base := time.Now().UTC()
	for i := range numJobs {
		job := Job{
			ID:         fmt.Sprintf("job-%03d", i),
			Command:    "true",
			State:      "pending",
			MaxRetries: 3,
			CreatedAt:  base.Add(time.Duration(i) * time.Millisecond),
			UpdatedAt:  base.Add(time.Duration(i) * time.Microsecond),
		}
		if err := InsertJobToDB(database, job); err != nil {
			t.Fatalf("insert %v", err)
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	claimedBy := map[string]string{} // jobID --> workerID
	numWorker := 10

	for w := range numWorker {
		wg.Add(1)
		workerID := fmt.Sprintf("worker-%d", w)
		go func(workerID string) {
			defer wg.Done()
			for {
				job, err := ClaimNextJob(database, workerID, 30)
				if err != nil {
					t.Errorf("claim: %v", err)
					return
				}
				if job == nil {
					return // nothing left to claim
				}
				mu.Lock()
				if prev, exists := claimedBy[job.ID]; exists {
					t.Errorf("job %s claimed twice: by %s and %s", job.ID, prev, workerID)
				}
				claimedBy[job.ID] = workerID
				mu.Unlock()
				if err := MarkJobSuccess(database, job.ID, workerID); err != nil {
					t.Errorf("mark success: %v", err)
				}
			}
		}(workerID)
	}
	wg.Wait()
	if len(claimedBy) != numJobs {
		t.Fatalf("expected %d jobs claimed exactly once, got %d", numJobs, len(claimedBy))
	}
}
