package db

import (
	"database/sql"
	"errors"
	"strconv"
)

// Used when the config table has no value
// or contains an invalid value.
const (
	defaultMaxRetries = 3
	defaultBackoffBase = 2.0
)

// if the value exists get it or return a default value
func GetConfigInt(database *sql.DB, key string, fallback int) int{
	var v string
	err := database.QueryRow(`
	SELECT value FROM config WHERE key = ?
	`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows){
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

func GetConfigFloat(database *sql.DB, key string, fallback float64) float64{
	var v string
	err := database.QueryRow(`
	SELECT value FROM config WHERE key = ?
	`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows){
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

func SetConfig(database *sql.DB, key, value string) error{
	_, err := database.Exec(`
	INSERT INTO config (key, value) VALUES (?, ?)
	ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

func EffectiveMaxRetries(database *sql.DB) int{
	return GetConfigInt(database, "max_retries", defaultMaxRetries)
}
func EffectiveBackoffBase(database *sql.DB) float64{
	return GetConfigFloat(database, "backoff_base", defaultBackoffBase)
}
