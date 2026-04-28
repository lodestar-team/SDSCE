package auth_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/big"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	authv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1"
	"github.com/graphprotocol/substreams-data-service/provider/auth"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/dauth"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// testDomain is a fixed EIP-712 domain used across tests.
var testDomain = horizon.NewDomain(1337, eth.MustNewAddress("0x1234567890123456789012345678901234567890"))

// testServiceProvider is the address this sidecar "owns".
var testServiceProvider = eth.MustNewAddress("0xaaaabbbbccccddddeeeeffffaaaabbbbccccdddd")

// newTestKey generates a deterministic test private key from a single byte.
func newTestKey(seed byte) *eth.PrivateKey {
	// Build a deterministic 32-byte private key from a single seed byte.
	var rawKey [32]byte
	rawKey[31] = seed
	hexStr := fmt.Sprintf("%064x", rawKey)
	key, err := eth.NewPrivateKey(hexStr)
	if err != nil {
		panic(err)
	}
	return key
}

// buildSignedRAV creates a ValidateAuthRequest with a signed RAV in headers.
func buildSignedRAV(t *testing.T, payerKey *eth.PrivateKey, signerKey *eth.PrivateKey, serviceProvider eth.Address, sessionID string) *authv1.ValidateAuthRequest {
	t.Helper()

	payerAddr := payerKey.PublicKey().Address()
	var collectionID horizon.CollectionID
	collectionID[0] = 0xCA

	rav := &horizon.RAV{
		CollectionID:    collectionID,
		Payer:           payerAddr,
		DataService:     eth.MustNewAddress("0x1111111111111111111111111111111111111111"),
		ServiceProvider: serviceProvider,
		TimestampNs:     1_000_000,
		ValueAggregate:  big.NewInt(100),
		Metadata:        nil,
	}

	signedRAV, err := horizon.Sign(testDomain, rav, signerKey)
	require.NoError(t, err)

	protoRAV := sidecar.HorizonSignedRAVToProto(signedRAV)

	// Encode RAV as base64
	protoBytes, err := proto.Marshal(protoRAV)
	require.NoError(t, err)
	ravHeader := base64.StdEncoding.EncodeToString(protoBytes)

	// Build untrusted headers map
	untrustedHeaders := map[string]*authv1.HeaderValues{
		"x-sds-rav": {Values: []string{ravHeader}},
	}
	if sessionID != "" {
		untrustedHeaders["x-sds-session-id"] = &authv1.HeaderValues{Values: []string{sessionID}}
	}

	return &authv1.ValidateAuthRequest{
		UntrustedHeaders: untrustedHeaders,
		IpAddress:        "127.0.0.1",
		Path:             "/sf.substreams.rpc.v2/Blocks",
	}
}

func mustCreateAuthSession(t *testing.T, repo *repository.InMemoryRepository, sessionID string, payer eth.Address) {
	t.Helper()

	sess := repository.NewSession(sessionID, payer, testServiceProvider, testServiceProvider, repository.PricingConfig{})
	sess.LastKeepAlive = time.Now()
	require.NoError(t, repo.SessionCreate(context.Background(), sess))
}

// --- Tests ---

func TestAuthService_ValidateAuth_SelfSigned(t *testing.T) {
	// When payer == signer no on-chain check is needed.
	payerKey := newTestKey(0x01)
	payerAddr := payerKey.PublicKey().Address()
	repo := repository.NewInMemoryRepository()
	mustCreateAuthSession(t, repo, "session-self-signed", payerAddr)

	svc := auth.NewAuthService(testServiceProvider, testDomain, nil, repo)

	req := buildSignedRAV(t, payerKey, payerKey, testServiceProvider, "session-self-signed")
	resp, err := svc.ValidateAuth(context.Background(), connect.NewRequest(req))

	require.NoError(t, err)
	assert.Equal(t, payerAddr.Pretty(), resp.Msg.TrustedHeaders[dauth.HeaderOrganizationID])
	assert.NotEmpty(t, resp.Msg.TrustedHeaders["x-sds-session-id"])
}

func TestAuthService_ValidateAuth_MissingRAV(t *testing.T) {
	svc := auth.NewAuthService(testServiceProvider, testDomain, nil, nil)

	_, err := svc.ValidateAuth(context.Background(), connect.NewRequest(&authv1.ValidateAuthRequest{
		UntrustedHeaders: map[string]*authv1.HeaderValues{},
		IpAddress:        "127.0.0.1",
		Path:             "/test",
	}))

	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

func TestAuthService_ValidateAuth_WrongServiceProvider(t *testing.T) {
	payerKey := newTestKey(0x02)
	differentProvider := eth.MustNewAddress("0x9999999999999999999999999999999999999999")
	repo := repository.NewInMemoryRepository()
	mustCreateAuthSession(t, repo, "session-wrong-provider", payerKey.PublicKey().Address())

	svc := auth.NewAuthService(testServiceProvider, testDomain, nil, repo)

	// Build RAV targeting a *different* service provider.
	req := buildSignedRAV(t, payerKey, payerKey, differentProvider, "session-wrong-provider")
	_, err := svc.ValidateAuth(context.Background(), connect.NewRequest(req))

	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodePermissionDenied, connectErr.Code())
}

func TestAuthService_ValidateAuth_UnauthorizedSigner(t *testing.T) {
	payerKey := newTestKey(0x03)
	signerKey := newTestKey(0x04) // different from payer, not authorized

	// collectorQuerier that always returns false (unauthorized).
	svc := auth.NewAuthService(testServiceProvider, testDomain, &mockAuthorizer{authorized: false}, nil)

	req := buildSignedRAV(t, payerKey, signerKey, testServiceProvider, "")
	_, err := svc.ValidateAuth(context.Background(), connect.NewRequest(req))

	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodePermissionDenied, connectErr.Code())
}

func TestAuthService_ValidateAuth_AuthorizedDelegateSigner(t *testing.T) {
	payerKey := newTestKey(0x05)
	signerKey := newTestKey(0x06) // different from payer but authorized on-chain

	payerAddr := payerKey.PublicKey().Address()
	repo := repository.NewInMemoryRepository()
	mustCreateAuthSession(t, repo, "session-authorized-delegate", payerAddr)

	// collectorQuerier that always returns true (authorized).
	svc := auth.NewAuthService(testServiceProvider, testDomain, &mockAuthorizer{authorized: true}, repo)

	req := buildSignedRAV(t, payerKey, signerKey, testServiceProvider, "session-authorized-delegate")
	resp, err := svc.ValidateAuth(context.Background(), connect.NewRequest(req))

	require.NoError(t, err)
	assert.Equal(t, payerAddr.Pretty(), resp.Msg.TrustedHeaders[dauth.HeaderOrganizationID])
	assert.NotEmpty(t, resp.Msg.TrustedHeaders["x-sds-session-id"])
}

func TestAuthService_ValidateAuth_NilCollectorQuerier_UnauthorizedSigner(t *testing.T) {
	// When collectorQuerier is nil, only self-signed RAVs are authorized.
	payerKey := newTestKey(0x07)
	signerKey := newTestKey(0x08)

	svc := auth.NewAuthService(testServiceProvider, testDomain, nil, nil)

	req := buildSignedRAV(t, payerKey, signerKey, testServiceProvider, "")
	_, err := svc.ValidateAuth(context.Background(), connect.NewRequest(req))

	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodePermissionDenied, connectErr.Code())
}

func TestAuthService_ValidateAuth_AuthorizerError(t *testing.T) {
	payerKey := newTestKey(0x09)
	signerKey := newTestKey(0x0a)

	svc := auth.NewAuthService(testServiceProvider, testDomain, &mockAuthorizer{err: assert.AnError}, nil)

	req := buildSignedRAV(t, payerKey, signerKey, testServiceProvider, "")
	_, err := svc.ValidateAuth(context.Background(), connect.NewRequest(req))

	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInternal, connectErr.Code())
}

func TestAuthService_ValidateAuth_MissingSessionID(t *testing.T) {
	payerKey := newTestKey(0x0b)
	svc := auth.NewAuthService(testServiceProvider, testDomain, nil, nil)

	req := buildSignedRAV(t, payerKey, payerKey, testServiceProvider, "")
	_, err := svc.ValidateAuth(context.Background(), connect.NewRequest(req))

	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "x-sds-session-id")
}

// mockAuthorizer implements auth.CollectorAuthorizer for testing.
type mockAuthorizer struct {
	authorized bool
	err        error
}

func (m *mockAuthorizer) IsAuthorized(_ context.Context, _, _ eth.Address) (bool, error) {
	return m.authorized, m.err
}
