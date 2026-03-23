package psql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

var preparedStmts = map[string]string{}

func registerFiles(files []string) {
	for _, file := range files {
		stmt := onDiskStatement(file)
		if _, found := preparedStmts[file]; found {
			panic(fmt.Errorf("statement %q already registered", file))
		}
		preparedStmts[file] = stmt
	}
}

type Database struct {
	*sqlx.DB
	stmts  map[string]*sqlx.NamedStmt
	logger *zap.Logger
}

func NewRepository(dbConn *sqlx.DB, logger *zap.Logger) *Database {
	return &Database{
		DB:     dbConn,
		stmts:  make(map[string]*sqlx.NamedStmt),
		logger: logger,
	}
}

func (r *Database) Setup() error {
	if err := r.setupPreparedStmt(preparedStmts); err != nil {
		return fmt.Errorf("failed to register prepared stmt: %w", err)
	}
	return nil
}

func (r *Database) setupPreparedStmt(stmts map[string]string) error {
	for k, s := range stmts {
		if _, found := r.stmts[k]; found {
			return fmt.Errorf("statement key %q already in use", k)
		}

		ps, err := r.PrepareNamed(s)
		if err != nil {
			return fmt.Errorf("failed to register prepared statement with key %q: %w", k, err)
		}

		r.stmts[k] = ps
	}
	return nil
}

func (r *Database) mustGetStmt(key string) *sqlx.NamedStmt {
	v, ok := r.stmts[key]
	if !ok {
		panic(fmt.Errorf("unable to find prepared stmt %q", key))
	}
	return v
}

// Ping checks database connectivity
func (r *Database) Ping(ctx context.Context) error {
	return r.DB.PingContext(ctx)
}

// Close closes the database connection
func (r *Database) Close() error {
	return r.DB.Close()
}

// GetConnectionFromDSN creates a database connection
func GetConnectionFromDSN(ctx context.Context, dsn string) (*sqlx.DB, error) {
	db, err := sqlx.ConnectContext(ctx, "postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	return db, nil
}
