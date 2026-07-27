package db

import (
	"database/sql"
	"testing"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil{
		t.Fatal(err)
	}

	_, err = database.Exec(`
	CREATE TABLE config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`)
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func TestGetConfigIntReturnsFallbackWhenMissing(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	got := GetConfigInt(database, "max_retries", 3)
	if got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
}

func TestGetConfigIntReturnsStoredValue(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	if err := SetConfig(database,  "max_retries", "5"); err != nil {
		t.Fatal(err)
	}
	got := GetConfigInt(database, "max_retries", 3)
	if got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
}

func TestGetConfigIntReturnsFallbackForInvalidValue(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	if err := SetConfig(database, "max_retries", "hello"); err != nil {
		t.Fatal(err)	
	}

	got := GetConfigInt(database, "max_retries", 3)
	if got != 3 {
		t.Fatalf("expected fallback as 3, got %d", got)
	}
}

func TestGetConfigFloatReturnedStoresValue(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	if err := SetConfig(database, "backoff_base", "2.5"); err != nil {
		t.Fatal(err)
	}
	got := GetConfigFloat(database, "backoff_base", 2.0)
	if got != 2.5 {
		t.Fatalf("expected 2.5, got %f", got)
	}
}

func TestSetConfigUpdatesExistingValue(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	if err := SetConfig(database, "max_retries", "3"); err != nil {
		t.Fatal(err)
	}

	if err := SetConfig(database, "max_retries", "7"); err != nil {
		t.Fatal(err)
	}

	got := GetConfigInt(database, "max_retries", 0)

	if got != 7 {
		t.Fatalf("expected 7, got %d", got)
	}
}

func TestEffectiveMaxRetriesUsesDefault(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	got := EffectiveMaxRetries(database)

	if got != defaultMaxRetries {
		t.Fatalf("expected %d, got %d", defaultMaxRetries, got)
	}
}

func TestEffectiveBackoffBaseUsesDefault(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	got := EffectiveBackoffBase(database)

	if got != defaultBackoffBase {
		t.Fatalf("expected %f, got %f", defaultBackoffBase, got)
	}
}

func TestEffectiveConfigUsesStoredValues(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	if err := SetConfig(database, "max_retries", "8"); err != nil {
		t.Fatal(err)
	}

	if err := SetConfig(database, "backoff_base", "4"); err != nil {
		t.Fatal(err)
	}

	if got := EffectiveMaxRetries(database); got != 8 {
		t.Fatalf("expected 8, got %d", got)
	}

	if got := EffectiveBackoffBase(database); got != 4 {
		t.Fatalf("expected 4, got %f", got)
	}
}
