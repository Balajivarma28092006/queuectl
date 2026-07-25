/*
	The main entry point for the whole application to enter, basically
	A global commmand router
*/

package cmd

import (
	"fmt"
	"os"
)

func Execute() {
	if len((os.Args)) < 2 {
		PrintGlobalUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "enqueue":
		HandleEnqueue()
	case "worker":
		HandleWorker()
	case "status":
		HandleStatus()
	case "list":
		HandleList()
	case "dlq":
		HandleDLQ()
	case "config":
		HandleConfig()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		PrintGlobalUsage()
		os.Exit(1)
	}
}

func PrintGlobalUsage() {
	fmt.Println("QueueCTL CLI Platform. Available commands:")
	fmt.Println("  enqueue <json_payload>")
	fmt.Println("  worker [start|stop]")
	fmt.Println("  status")
	fmt.Println("  list --state <state> [--json]")
	fmt.Println("  dlq [list|retry]")
	fmt.Println("  config set <key> <value>")
}
