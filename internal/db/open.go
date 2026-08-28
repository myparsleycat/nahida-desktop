package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const sqliteDriver = "sqlite"

// Open opens a file-backed SQLite database with the Electron client pragmas:
// WAL journal, foreign_keys=ON, busy_timeout=5000.
func Open(path string) (*sql.DB, error) {
	dsn, err := fileDSN(path)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(sqliteDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Electron's DatabaseSync is a single connection. Pooling would make
	// connection-scoped PRAGMAs (foreign_keys during reconcile) leak.
	db.SetMaxOpenConns(1)

	if err := applyPragmas(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return db, nil
}

func fileDSN(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve db path: %w", err)
	}

	// modernc.org/sqlite accepts a file URI with repeated _pragma query params.
	u := url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(abs),
	}
	if !strings.HasPrefix(u.Path, "/") {
		// Windows drive paths become /C:/... so the URI parser keeps the drive letter.
		u.Path = "/" + u.Path
	}

	q := u.Query()
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func applyPragmas(db *sql.DB) error {
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		return fmt.Errorf("pragma journal_mode: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("pragma foreign_keys: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return fmt.Errorf("pragma busy_timeout: %w", err)
	}
	return nil
}
