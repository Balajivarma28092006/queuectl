package cmd

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/BalajiVarma28092006/queuectl/db"
)

const pollInterval = 2 * time.Second

func workersDir() string {
	return filepath.Join(".queuectl", "workers")
}

func HandleWorker() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: queuectl worker <start|stop>")
		os.Exit(1)
	}

	switch os.Args[2] {
	case "start":
		handleWorkerStart(os.Args[3:])
	case "stop":
		handleWorkerStop()
	default:
		fmt.Fprintln(os.Stderr, "Usage: queuectl worker <stop|start --count>")
		os.Exit(1)
	}
}

func handleWorkerStart(args []string) {
	fs := flag.NewFlagSet("worker start", flag.ExitOnError)
	count := fs.Int("count", 1, "number of concurrent workers goroutines to run this process")
	_ = fs.Parse(args)

	if err := os.MkdirAll(workersDir(), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create workers dir: %v\n", err)
		os.Exit(1)
	}

	database, err := db.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	pid := os.Getpid()
	pidPath := filepath.Join(workersDir(), fmt.Sprintf("%d.pid", pid))
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write pid file: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(pidPath)

	ctx, cancel := context.WithCancel(context.Background())

	// SIGTERM from worker stop run in another terminal and SIGINT (CTRL+C) both trigger the same cancel path
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\nreceived %s, finishing happening job or jobs then exiting...\n", sig)
		cancel()
	}()

	fmt.Printf("starting %d worker goroutine(s) in process pid=%d\n", *count, pid)
	fmt.Println("(Ctrl+C here, or `queuectl worker stop` from another terminal, for graceful shutdown)")

	var wg sync.WaitGroup
	// Without the WaitGroup, after cancel() the main goroutine would continue immediately,
	// and it would differed the database.close and os.Remove pid and exits the process. so the
	// workers who got the chance to do a job might not be able to finish the job and they will be
	// killed abruptly. This will be stopped by using wait groups they wait until the number of workers remaining goes to zero
	// before calling the cancel() which would finally end the process.
	for i := 0; i < *count; i++ {
		wg.Add(1)
		workerID := fmt.Sprintf("%d-%d", pid, i+1)
		go func() {
			defer wg.Done()
			workerLoop(ctx, database, workerID)
		}()
	}
	wg.Wait()
	fmt.Println("all workers stopped")
}

func workerLoop(ctx context.Context, database *sql.DB, workerID string) {
	for {
		// select whichever data is recieved first
		select {
		case <-ctx.Done():
			return
		default:
		}
		if n, err := db.ReapExpiredLeases(database); err == nil && n > 0 {
			fmt.Fprintf(os.Stderr, "[%s] reaped %d job(s) stuck past their lease\n", workerID, n)
		}
		leaseSeconds := db.EffectiveLeaseSeconds(database)
		job, err := db.ClaimNextJob(database, workerID, leaseSeconds)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] claim error: %v\n", workerID, err)
			job = nil
		}

		if job == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollInterval):
			}
			continue
		}
	}
}

func handleWorkerStop() {

}
