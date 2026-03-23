package psql

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	postgresContainer *postgres.PostgresContainer
	postgresTestDSN   string
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	var err error
	postgresContainer, err = postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("sds_test"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start PostgreSQL container: %v\n", err)
		os.Exit(1)
	}

	postgresTestDSN, err = postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get connection string: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	if err := postgresContainer.Terminate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to terminate container: %v\n", err)
	}

	os.Exit(code)
}
