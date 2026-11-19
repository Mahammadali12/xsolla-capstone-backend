package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// DefaultMaxOpenConns caps the API's database connections. Without a cap every concurrent request opened its own
// connection, and at about 100 users at once Postgres refused new ones ("too many clients already") and requests failed
// with 500. With the cap, extra requests wait a moment for a free connection instead. DB_MAX_OPEN_CONNS overrides it.
const DefaultMaxOpenConns = 20

// Open connects to Postgres and caps the connection pool (see DefaultMaxOpenConns).
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	LimitPool(db, maxOpenConns())
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

func Migrate(db *sql.DB, migrationsDir string) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY
		)`); err != nil {
		return err
	}

	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return err
	}
	applied := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		version := strings.TrimSuffix(f, ".up.sql")
		if applied[version] {
			continue
		}
		content, err := os.ReadFile(filepath.Join(migrationsDir, f))
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", version, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// LimitPool caps a connection pool at max open connections, keeps up to that many idle ones for reuse, and replaces
// connections now and then so a restarted database or pooler is picked up.
func LimitPool(db *sql.DB, max int) {
	db.SetMaxOpenConns(max)
	db.SetMaxIdleConns(max)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
}

func maxOpenConns() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("DB_MAX_OPEN_CONNS"))); err == nil && v > 0 {
		return v
	}
	return DefaultMaxOpenConns
}
