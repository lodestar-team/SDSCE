package psql

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/graphprotocol/substreams-data-service/provider/repository"
)

//go:embed sql
var statements embed.FS

var templates *template.Template

func initTemplates() {
	templates = template.New("")

	// Read all SQL files and parse them with full path as name
	matches, err := statements.ReadDir("sql")
	if err != nil {
		panic(fmt.Errorf("unable to read sql directory: %w", err))
	}

	for _, entry := range matches {
		if !entry.IsDir() {
			continue
		}

		folderName := entry.Name()
		sqlFiles, err := statements.ReadDir("sql/" + folderName)
		if err != nil {
			panic(fmt.Errorf("unable to read sql/%s directory: %w", folderName, err))
		}

		for _, sqlFile := range sqlFiles {
			if sqlFile.IsDir() || !strings.HasSuffix(sqlFile.Name(), ".sql") {
				continue
			}

			fullPath := "sql/" + folderName + "/" + sqlFile.Name()
			content, err := statements.ReadFile(fullPath)
			if err != nil {
				panic(fmt.Errorf("unable to read %s: %w", fullPath, err))
			}

			// Use "folder/file.sql" as the template name (e.g., "session/create.sql")
			templateName := folderName + "/" + sqlFile.Name()
			_, err = templates.New(templateName).Parse(string(content))
			if err != nil {
				panic(fmt.Errorf("unable to parse %s: %w", fullPath, err))
			}
		}
	}
}

func onDiskStatement(file string) string {
	if templates == nil {
		initTemplates()
	}

	// Template name is "folder/file.sql" (e.g., "session/create.sql")
	buffer := bytes.NewBuffer(make([]byte, 0, 1024))
	if err := templates.ExecuteTemplate(buffer, file, map[string]any{}); err != nil {
		panic(fmt.Errorf("unable to execute embedded sql statements %q: %w", file, err))
	}

	return buffer.String()
}

// getOne retrieves a single record
func getOne[T any](ctx context.Context, db *Database, statement string, args any) (*T, error) {
	stmt := db.mustGetStmt(statement)
	var model T
	err := stmt.GetContext(ctx, &model, args)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("failed %s: %w", strings.ReplaceAll(statement, "_", " "), err)
	}
	return &model, nil
}

// getMany retrieves multiple records
func getMany[T any](ctx context.Context, db *Database, statement string, args any) ([]*T, error) {
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
func execOne[T any](ctx context.Context, db *Database, statement string, args any) (*T, error) {
	stmt := db.mustGetStmt(statement)
	var model T
	err := stmt.GetContext(ctx, &model, args)
	if err != nil {
		return nil, fmt.Errorf("failed %s: %w", strings.ReplaceAll(statement, "_", " "), err)
	}
	return &model, nil
}

// execSimple executes without returning data
func execSimple(ctx context.Context, db *Database, statement string, args any) error {
	stmt := db.mustGetStmt(statement)
	_, err := stmt.ExecContext(ctx, args)
	if err != nil {
		return fmt.Errorf("failed %s: %w", strings.ReplaceAll(statement, "_", " "), err)
	}
	return nil
}
