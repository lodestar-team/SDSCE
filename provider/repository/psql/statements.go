package psql

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed sql
var statements embed.FS

var templates *template.Template

func initTemplates() {
	var err error
	templates, err = template.ParseFS(statements, "sql/*/*.sql")
	if err != nil {
		panic(fmt.Errorf("unable to parse embedded sql statements: %w", err))
	}
}

func onDiskStatement(file string) string {
	if templates == nil {
		initTemplates()
	}

	_, name, found := strings.Cut(file, "/")
	if !found {
		panic(fmt.Errorf("unable to find 'folder/name' in %q", file))
	}

	buffer := bytes.NewBuffer(make([]byte, 0, 1024))
	if err := templates.ExecuteTemplate(buffer, name, map[string]any{}); err != nil {
		panic(fmt.Errorf("unable to execute embedded sql statements: %w", err))
	}

	return buffer.String()
}

// getOne retrieves a single record
func getOne[T any](ctx context.Context, db *Database, statement string, args map[string]any) (*T, error) {
	stmt := db.mustGetStmt(statement)
	var model T
	err := stmt.GetContext(ctx, &model, args)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed %s: %w", strings.ReplaceAll(statement, "_", " "), err)
	}
	return &model, nil
}

// getMany retrieves multiple records
func getMany[T any](ctx context.Context, db *Database, statement string, args map[string]any) ([]*T, error) {
	stmt := db.mustGetStmt(statement)
	var models []*T
	err := stmt.SelectContext(ctx, &models, args)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed %s: %w", strings.ReplaceAll(statement, "_", " "), err)
	}
	return models, nil
}

// execOne executes INSERT/UPDATE with RETURNING
func execOne[T any](ctx context.Context, db *Database, statement string, args map[string]any) (*T, error) {
	stmt := db.mustGetStmt(statement)
	var model T
	err := stmt.GetContext(ctx, &model, args)
	if err != nil {
		return nil, fmt.Errorf("failed %s: %w", strings.ReplaceAll(statement, "_", " "), err)
	}
	return &model, nil
}

// execSimple executes without returning data
func execSimple(ctx context.Context, db *Database, statement string, args map[string]any) error {
	stmt := db.mustGetStmt(statement)
	_, err := stmt.ExecContext(ctx, args)
	if err != nil {
		return fmt.Errorf("failed %s: %w", strings.ReplaceAll(statement, "_", " "), err)
	}
	return nil
}
