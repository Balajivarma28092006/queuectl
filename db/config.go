package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Used when the config table has no value
// or contains an invalid value.
const (
	defaultMaxRetries   = 3
	defaultBackoffBase  = 2.0
	defaultLeaseSeconds = 15

	PollInterval         = 2 * time.Second
	RecoverySeconds      = 60
	recoverySafetyMargin = 5
)

// no workers lease time will be more than this value which consists of both poll time before it checks for a new job
// and a small boundary of 5 seconds before
func MaxLeaseSeconds() int {
	return RecoverySeconds - int(PollInterval.Seconds()) - recoverySafetyMargin
}

// if the value exists get it or return a default value
func GetConfigInt(database *sql.DB, key string, fallback int) int {
	var v string
	err := database.QueryRow(`
	SELECT value FROM config WHERE key = ?
	`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback
	}
	if err != nil {
		panic(err)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func GetConfigFloat(database *sql.DB, key string, fallback float64) float64 {
	var v string
	err := database.QueryRow(`
	SELECT value FROM config WHERE key = ?
	`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback
	}
	if err != nil {
		panic(err)
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func SetConfig(database *sql.DB, key, value string) error {
	_, err := database.Exec(`
	INSERT INTO config (key, value) VALUES (?, ?)
	ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

func SetLeaseSeconds(database *sql.DB, seconds int) error {
	if seconds <= 0 {
		return fmt.Errorf("lease-seconds must be > 0")
	}
	if max := MaxLeaseSeconds(); seconds > max {
		return fmt.Errorf(
			"lease-seconds must be <= %d: lease_seconds + poll_interval (%ds) must stay under the %ds but got %ds",
			max, int(PollInterval.Seconds()), RecoverySeconds, seconds)
	}
	return SetConfig(database, "lease_seconds", strconv.Itoa(seconds))
}

func EffectiveMaxRetries(database *sql.DB) int {
	return GetConfigInt(database, "max_retries", defaultMaxRetries)
}
func EffectiveBackoffBase(database *sql.DB) float64 {
	return GetConfigFloat(database, "backoff_base", defaultBackoffBase)
}

func EffectiveLeaseSeconds(database *sql.DB) int {
	v := GetConfigInt(database, "lease_seconds", defaultLeaseSeconds)
	if v <= 0 {
		return defaultLeaseSeconds
	}
	if max := MaxLeaseSeconds(); v > max {
		return max
	}
	return v
}
