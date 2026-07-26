package cmd

import (
	"fmt"
	"os"

	"github.com/BalajiVarma28092006/queuectl/db"
)

func HandleConfig() {
	database, err := db.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, err)
		os.Exit(1)
	}
	defer database.Close()

	if len(os.Args) >= 3 && os.Args[2] == "show" {
		fmt.Printf("max_retries=%d\nbackoff_base=%v\n",
		db.EffectiveMaxRetries(database), db.EffectiveBackoffBase(database))
		return
	}

	if len(os.Args) < 5 || os.Args[2] != set {
		fmt.Fprintln(os.Stderr, "Usage: queuectl config set <max-retries|backoff-base> <value>")
		os.Exit(1)
	}
	key, val := os.Args[3], os.Args[4]

	switch key{
	case "max_retries":
	case "backoff-base":
	default:
		fmt.Fprintf(os.Stderr, "unknown config key %q\n", key)
		os.Exit(1)
	}
	fmt.Printf("config %s = %s\n", key, val)
}
