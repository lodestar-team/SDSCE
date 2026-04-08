package gateway

import (
	"context"
	"testing"

	"github.com/streamingfast/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var zlogTest, _ = logging.PackageLogger("repository_test", "github.com/graphprotocol/substreams-data-service/provider/gateway@repository_test")

func init() {
	logging.InstantiateLoggers()
}

func TestNewRepositoryFromDSN_InMemory(t *testing.T) {
	ctx := context.Background()

	repo, err := NewRepositoryFromDSN(ctx, "inmemory://", zlogTest)
	require.NoError(t, err)
	require.NotNil(t, repo)

	// Verify it works by pinging
	err = repo.Ping(ctx)
	require.NoError(t, err)

	// Cleanup
	err = repo.Close()
	require.NoError(t, err)
}

func TestNewRepositoryFromDSN_EmptyDSN(t *testing.T) {
	ctx := context.Background()

	repo, err := NewRepositoryFromDSN(ctx, "", zlogTest)
	require.Error(t, err)
	require.Nil(t, repo)
	assert.Contains(t, err.Error(), "DSN must not be empty")
}

func TestNewRepositoryFromDSN_InvalidFormat(t *testing.T) {
	ctx := context.Background()

	repo, err := NewRepositoryFromDSN(ctx, "invalid-dsn-no-scheme", zlogTest)
	require.Error(t, err)
	require.Nil(t, repo)
	assert.Contains(t, err.Error(), "missing '://' separator")
}

func TestNewRepositoryFromDSN_UnsupportedScheme(t *testing.T) {
	ctx := context.Background()

	repo, err := NewRepositoryFromDSN(ctx, "redis://localhost:6379", zlogTest)
	require.Error(t, err)
	require.Nil(t, repo)
	assert.Contains(t, err.Error(), "unsupported DSN scheme")
}

func TestNewRequiresRepository(t *testing.T) {
	repo, err := New(&Config{}, zlogTest)
	require.Error(t, err)
	require.Nil(t, repo)
	assert.Contains(t, err.Error(), "repository must be provided explicitly")
}

func TestSanitizeDSN(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "postgres with password",
			input:    "postgres://user:secret@localhost:5432/dbname",
			expected: "postgres://user:***@localhost:5432/dbname",
		},
		{
			name:     "no password",
			input:    "postgres://user@localhost:5432/dbname",
			expected: "postgres://user@localhost:5432/dbname",
		},
		{
			name:     "no credentials",
			input:    "postgres://localhost:5432/dbname",
			expected: "postgres://localhost:5432/dbname",
		},
		{
			name:     "inmemory",
			input:    "inmemory://",
			expected: "inmemory://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeDSN(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
