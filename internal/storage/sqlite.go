package storage

import (
	_ "embed"
	"database/sql"
	"fmt"
	"log/slog"

	_ "modernc.org/sqlite"
)

//go:embed sql/schema.sql
var schemaSQL string

// OpenDB opens (or creates) the furnace SQLite database at path.
// readOnly=true uses mode=ro DSN and skips schema migration.
// readOnly=false uses immediate transaction locking and runs migrateSchema.
func OpenDB(path string, readOnly bool, logger *slog.Logger) (*sql.DB, error) {
	var dsn string
	if readOnly {
		dsn = "file:" + path + "?mode=ro"
	} else {
		dsn = "file:" + path + "?_txlock=immediate"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if readOnly {
		db.SetMaxOpenConns(2)
	} else {
		db.SetMaxOpenConns(1)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite ping: %w", err)
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite pragma (%s): %w", p, err)
		}
	}

	if !readOnly {
		if err := migrateSchema(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite migrate: %w", err)
		}
	}

	logger.Info("sqlite db opened", "path", path, "read_only", readOnly)
	return db, nil
}

func migrateSchema(db *sql.DB) error {
	_, err := db.Exec(schemaSQL)
	return err
}
