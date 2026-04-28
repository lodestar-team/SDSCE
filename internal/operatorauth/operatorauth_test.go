package operatorauth

import (
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthorizeHeader(t *testing.T) {
	config := Config{
		ReadBearerToken:  "read-token",
		AdminBearerToken: "admin-token",
	}

	t.Run("valid read token on read endpoint", func(t *testing.T) {
		header := http.Header{}
		header.Set("Authorization", "Bearer read-token")

		role, err := AuthorizeHeader(header, config, RoleOperatorRead)
		require.NoError(t, err)
		assert.Equal(t, RoleOperatorRead, role)
	})

	t.Run("valid admin token on read endpoint", func(t *testing.T) {
		header := http.Header{}
		header.Set("Authorization", "Bearer admin-token")

		role, err := AuthorizeHeader(header, config, RoleOperatorRead)
		require.NoError(t, err)
		assert.Equal(t, RoleAdminWrite, role)
	})

	t.Run("read token rejected for write endpoint", func(t *testing.T) {
		header := http.Header{}
		header.Set("Authorization", "Bearer read-token")

		role, err := AuthorizeHeader(header, config, RoleAdminWrite)
		require.Error(t, err)
		assert.Equal(t, RoleOperatorRead, role)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})

	t.Run("missing bearer token rejected", func(t *testing.T) {
		role, err := AuthorizeHeader(http.Header{}, config, RoleOperatorRead)
		require.Error(t, err)
		assert.Equal(t, Role(""), role)
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("malformed authorization header rejected", func(t *testing.T) {
		header := http.Header{}
		header.Set("Authorization", "Token read-token")

		role, err := AuthorizeHeader(header, config, RoleOperatorRead)
		require.Error(t, err)
		assert.Equal(t, Role(""), role)
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("unknown token rejected", func(t *testing.T) {
		header := http.Header{}
		header.Set("Authorization", "Bearer unknown-token")

		role, err := AuthorizeHeader(header, config, RoleOperatorRead)
		require.Error(t, err)
		assert.Equal(t, Role(""), role)
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})
}
