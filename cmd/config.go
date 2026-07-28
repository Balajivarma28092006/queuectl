package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/BalajiVarma28092006/queuectl/db"
)

func HandleConfig() {
	database, err := db.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer database.Close()

	if len(os.Args) >= 3 && os.Args[2] == "show" {
		fmt.Printf("max_retries=%d\nbackoff_base=%v\nlease_seconds=%d (max allowed: %d, to keep crash recovery under %d seconds)\n",
			db.EffectiveMaxRetries(database), db.EffectiveBackoffBase(database),
			db.EffectiveLeaseSeconds(database), db.MaxLeaseSeconds(), db.RecoverySeconds)
		return
	}

	if len(os.Args) < 5 || os.Args[2] != "set" {
		fmt.Fprintln(os.Stderr, "Usage: queuectl config set <max-retries|backoff-base> <value>")
		os.Exit(1)
	}

	key, val := os.Args[3], os.Args[4]

	switch key {
	case "max_retries":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			fmt.Fprintf(os.Stderr, "invalid max-retries value %q\n", val)
			os.Exit(1)
		}
		_ = db.SetConfig(database, "max_retries", val)
	case "backoff-base":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil || f <= 1 {
			fmt.Fprintf(os.Stderr, "invalid backoff-base value %q (must be > 1) \n", val)
			os.Exit(1)
		}
		_ = db.SetConfig(database, "backoff_base", val)
	case "lease-seconds":
		n, err := strconv.Atoi(val)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid lease-seconds value %q\n", val)
			os.Exit(1)
		}
		// setleaseSeconds itself rejects anything that would push lease_seconds + poll_interval past
		// the max lease seconds it would return an error
		if err := db.SetLeaseSeconds(database, n); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown config key %q\n", key)
		os.Exit(1)
	}
	fmt.Printf("config %s = %s\n", key, val)
}
