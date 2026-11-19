package db

import (
	"database/sql"
	"testing"
)

func TestMaxOpenConns(t *testing.T) {
	for env, want := range map[string]int{"": DefaultMaxOpenConns, "40": 40, " 8 ": 8, "0": DefaultMaxOpenConns, "-3": DefaultMaxOpenConns, "lots": DefaultMaxOpenConns} {
		t.Setenv("DB_MAX_OPEN_CONNS", env)
		if got := maxOpenConns(); got != want {
			t.Errorf("DB_MAX_OPEN_CONNS=%q: got %d, want %d", env, got, want)
		}
	}
}

func TestLimitPool(t *testing.T) {
	database, err := sql.Open("postgres", "host=127.0.0.1 port=1 sslmode=disable") // never connects
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	LimitPool(database, 7)
	if got := database.Stats().MaxOpenConnections; got != 7 {
		t.Fatalf("MaxOpenConnections = %d, want 7", got)
	}
}
