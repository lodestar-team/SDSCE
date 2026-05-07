package gateway

import (
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/internal/operatorauth"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAuthorizeOperator(t *testing.T) {
	gateway, err := New(&Config{
		ListenAddr: ":0",
		OperatorAuthConfig: operatorauth.Config{
			ReadBearerToken:  "read-token",
			AdminBearerToken: "admin-token",
		},
		Repository: repository.NewInMemoryRepository(),
	}, zap.NewNop())
	require.NoError(t, err)

	t.Run("missing token rejects unauthenticated", func(t *testing.T) {
		_, err := gateway.authorizeOperator(http.Header{}, operatorauth.RoleOperatorRead)
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("malformed token rejects unauthenticated", func(t *testing.T) {
		header := http.Header{}
		header.Set("Authorization", "Basic read-token")

		_, err := gateway.authorizeOperator(header, operatorauth.RoleOperatorRead)
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("operator read token works on read requirement", func(t *testing.T) {
		header := http.Header{}
		header.Set("Authorization", "Bearer read-token")

		role, err := gateway.authorizeOperator(header, operatorauth.RoleOperatorRead)
		require.NoError(t, err)
		require.Equal(t, operatorauth.RoleOperatorRead, role)
	})

	t.Run("operator read token fails on mutating requirement", func(t *testing.T) {
		header := http.Header{}
		header.Set("Authorization", "Bearer read-token")

		role, err := gateway.authorizeOperator(header, operatorauth.RoleAdminWrite)
		require.Equal(t, operatorauth.RoleOperatorRead, role)
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})

	t.Run("admin write token works on read and mutating requirements", func(t *testing.T) {
		header := http.Header{}
		header.Set("Authorization", "Bearer admin-token")

		role, err := gateway.authorizeOperator(header, operatorauth.RoleOperatorRead)
		require.NoError(t, err)
		require.Equal(t, operatorauth.RoleAdminWrite, role)

		role, err = gateway.authorizeOperator(header, operatorauth.RoleAdminWrite)
		require.NoError(t, err)
		require.Equal(t, operatorauth.RoleAdminWrite, role)
	})
}
