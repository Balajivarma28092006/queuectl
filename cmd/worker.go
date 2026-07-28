package cmd

import (
	"fmt"
	"path/filepath"
	"time"
)

const pollInterval = 2 * time.Second

func workersDir() string {
	return filepath.Join(".queuectl", "workers")
}

func HandleWorker() {
	fmt.Println("worker started")
}
