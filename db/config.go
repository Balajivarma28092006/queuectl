package db

import "database/sql"

const (
	defaultMaxRetries = 3
	defaultBackoffBase = 2.0
)

func GetConfigInt(database *sql.DB, key string, fallback int) int{}
func GetConfigFloat(database *sql.DB, key string, fallback float64) float64{}
func SetConfig(database *sql.DB, key, value string) error{}
func EffectiveMaxRetries(database *sql.DB) int{}
func EffectiveBackoffBase(database *sql.DB) float64{}
