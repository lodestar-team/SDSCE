package operatorauth

import (
	"fmt"
	"net/http"
	"strings"

	"connectrpc.com/connect"
)

const authorizationHeader = "Authorization"

type Role string

const (
	RoleOperatorRead Role = "operator.read"
	RoleAdminWrite   Role = "admin.write"
)

type Config struct {
	ReadBearerToken  string
	AdminBearerToken string
}

func (r Role) Allows(required Role) bool {
	switch required {
	case RoleOperatorRead:
		return r == RoleOperatorRead || r == RoleAdminWrite
	case RoleAdminWrite:
		return r == RoleAdminWrite
	default:
		return false
	}
}

func AuthorizeHeader(header http.Header, config Config, required Role) (Role, error) {
	rawHeader := strings.TrimSpace(header.Get(authorizationHeader))
	if rawHeader == "" {
		return "", connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("missing %s header", authorizationHeader))
	}

	token, err := parseBearerToken(rawHeader)
	if err != nil {
		return "", connect.NewError(connect.CodeUnauthenticated, err)
	}

	role, ok := config.roleForToken(token)
	if !ok {
		return "", connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("invalid bearer token"))
	}

	if !role.Allows(required) {
		return role, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("role %q does not satisfy %q", role, required))
	}

	return role, nil
}

func parseBearerToken(headerValue string) (string, error) {
	scheme, token, found := strings.Cut(headerValue, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", fmt.Errorf("malformed %s header: expected Bearer token", authorizationHeader)
	}

	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, " ") {
		return "", fmt.Errorf("malformed %s header: expected Bearer token", authorizationHeader)
	}

	return token, nil
}

func (c Config) roleForToken(token string) (Role, bool) {
	if token == "" {
		return "", false
	}

	// Check admin first so a deployer can intentionally reuse the same token for both roles.
	if token == c.AdminBearerToken && c.AdminBearerToken != "" {
		return RoleAdminWrite, true
	}
	if token == c.ReadBearerToken && c.ReadBearerToken != "" {
		return RoleOperatorRead, true
	}

	return "", false
}
