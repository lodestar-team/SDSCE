package psql

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
)

// MigrationDir resolves the repository-local migrations directory for the PostgreSQL repository.
func MigrationDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolving migrations directory: runtime caller unavailable")
	}

	dir := filepath.Join(filepath.Dir(filename), "migrations")
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving absolute migrations directory: %w", err)
	}

	return absDir, nil
}

// MigrationSourceURL returns the file:// source URI expected by golang-migrate.
func MigrationSourceURL() (string, error) {
	dir, err := MigrationDir()
	if err != nil {
		return "", err
	}

	return (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(dir),
	}).String(), nil
}
