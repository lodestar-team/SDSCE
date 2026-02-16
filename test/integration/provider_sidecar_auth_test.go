package integration

import (
	"context"
	"math/big"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/require"

	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	providersidecar "github.com/graphprotocol/substreams-data-service/provider/sidecar"
	"github.com/graphprotocol/substreams-data-service/sidecar"
)

func TestProviderSidecar_OnChainAuthorization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	domain := env.Domain()

	providerConfig := &providersidecar.Config{
		ListenAddr:      ":19005",
		ServiceProvider: env.ServiceProvider.Address,
		Domain:          domain,
		CollectorAddr:   env.Collector.Address,
		EscrowAddr:      env.Escrow.Address,
		RPCEndpoint:     env.RPCURL,
		AcceptedSigners: nil, // must use on-chain isAuthorized
	}
	providerSidecar := providersidecar.New(providerConfig, zlog.Named("provider"))
	go providerSidecar.Run()
	defer providerSidecar.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	providerClient := providerv1connect.NewProviderSidecarServiceClient(http.DefaultClient, "http://localhost:19005")

	rav := &horizon.RAV{
		Payer:           env.Payer.Address,
		DataService:     env.DataService.Address,
		ServiceProvider: env.ServiceProvider.Address,
		TimestampNs:     uint64(time.Now().UnixNano()),
		ValueAggregate:  big.NewInt(0),
		Metadata:        nil,
	}
	signedRAV, err := horizon.Sign(domain, rav, setup.SignerKey)
	require.NoError(t, err)

	validateResp, err := providerClient.ValidatePayment(ctx, connect.NewRequest(&providerv1.ValidatePaymentRequest{
		PaymentRav: sidecar.HorizonSignedRAVToProto(signedRAV),
	}))
	require.NoError(t, err)
	require.True(t, validateResp.Msg.Valid, "expected authorized signer to be accepted: %s", validateResp.Msg.RejectionReason)

	unauthorizedKey, err := eth.NewRandomPrivateKey()
	require.NoError(t, err)
	unauthorizedSignedRAV, err := horizon.Sign(domain, rav, unauthorizedKey)
	require.NoError(t, err)

	validateResp2, err := providerClient.ValidatePayment(ctx, connect.NewRequest(&providerv1.ValidatePaymentRequest{
		PaymentRav: sidecar.HorizonSignedRAVToProto(unauthorizedSignedRAV),
	}))
	require.NoError(t, err)
	require.False(t, validateResp2.Msg.Valid)
	require.Contains(t, validateResp2.Msg.RejectionReason, "not authorized")

	// Also validate the gateway StartSession path enforces on-chain authorization.
	gatewayClient := providerv1connect.NewPaymentGatewayServiceClient(http.DefaultClient, "http://localhost:19005")
	startResp, err := gatewayClient.StartSession(ctx, connect.NewRequest(&providerv1.StartSessionRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(env.Payer.Address),
			Receiver:    commonv1.AddressFromEth(env.ServiceProvider.Address),
			DataService: commonv1.AddressFromEth(env.DataService.Address),
		},
		InitialRav: sidecar.HorizonSignedRAVToProto(unauthorizedSignedRAV),
	}))
	require.NoError(t, err)
	require.False(t, startResp.Msg.Accepted)
	require.Contains(t, startResp.Msg.RejectionReason, "not authorized")
}
