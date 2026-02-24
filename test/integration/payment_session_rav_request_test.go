package integration

import (
	"context"
	"crypto/tls"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"

	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	providergateway "github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/sidecar"
)

func TestPaymentSession_ProviderRequestsRAVOnUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	domain := env.Domain()

	// Make pricing deterministic: 1 base unit per block, 0 per byte.
	pricingConfig := &sidecar.PricingConfig{
		PricePerBlock: sds.NewGRTFromUint64(1),
		PricePerByte:  sds.ZeroGRT(),
	}

	providerConfig := &providergateway.Config{
		ListenAddr:      ":19007",
		ServiceProvider: env.ServiceProvider.Address,
		Domain:          domain,
		CollectorAddr:   env.Collector.Address,
		EscrowAddr:      env.Escrow.Address,
		RPCEndpoint:     env.RPCURL,
		PricingConfig:   pricingConfig,
	}
	providerGateway := providergateway.New(providerConfig, zlog.Named("provider"))
	go providerGateway.Run()
	defer providerGateway.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	h2cClient := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		},
	}

	gatewayClient := providerv1connect.NewPaymentGatewayServiceClient(h2cClient, "http://localhost:19007", connect.WithGRPC())

	rav0 := &horizon.RAV{
		Payer:           env.Payer.Address,
		DataService:     env.DataService.Address,
		ServiceProvider: env.ServiceProvider.Address,
		TimestampNs:     uint64(time.Now().UnixNano()),
		ValueAggregate:  big.NewInt(0),
		Metadata:        nil,
	}
	signedRAV0, err := horizon.Sign(domain, rav0, setup.SignerKey)
	require.NoError(t, err)

	startResp, err := gatewayClient.StartSession(ctx, connect.NewRequest(&providerv1.StartSessionRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(env.Payer.Address),
			Receiver:    commonv1.AddressFromEth(env.ServiceProvider.Address),
			DataService: commonv1.AddressFromEth(env.DataService.Address),
		},
		InitialRav: sidecar.HorizonSignedRAVToProto(signedRAV0),
	}))
	require.NoError(t, err)
	require.True(t, startResp.Msg.Accepted)
	require.NotEmpty(t, startResp.Msg.SessionId)

	stream := gatewayClient.PaymentSession(ctx)

	// Send a usage report; provider should compute cost from pricing config and request a new RAV.
	require.NoError(t, stream.Send(&providerv1.PaymentSessionRequest{
		SessionId: startResp.Msg.SessionId,
		Message: &providerv1.PaymentSessionRequest_UsageReport{
			UsageReport: &providerv1.UsageReport{
				Usage: &commonv1.Usage{
					BlocksProcessed:  1,
					BytesTransferred: 1,
					Requests:         1,
					Cost:             commonv1.GRTFromBigInt(big.NewInt(123)), // intentionally wrong; provider overrides
				},
			},
		},
	}))

	resp, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp.GetRavRequest(), "expected provider to emit a rav_request")
	require.NotNil(t, resp.GetRavRequest().GetCurrentRav())
	require.NotNil(t, resp.GetRavRequest().GetUsage())
	require.Equal(t, uint64(1), resp.GetRavRequest().GetUsage().GetBlocksProcessed())
	require.Equal(t, big.NewInt(1).Bytes(), resp.GetRavRequest().GetUsage().GetCost().GetBytes())

	current := resp.GetRavRequest().GetCurrentRav().GetRav().GetValueAggregate().ToNative()
	nextValue := new(big.Int).Add(current.BigInt(), big.NewInt(1))

	rav1 := &horizon.RAV{
		Payer:           env.Payer.Address,
		DataService:     env.DataService.Address,
		ServiceProvider: env.ServiceProvider.Address,
		TimestampNs:     uint64(time.Now().UnixNano()),
		ValueAggregate:  nextValue,
		Metadata:        nil,
	}
	signedRAV1, err := horizon.Sign(domain, rav1, setup.SignerKey)
	require.NoError(t, err)

	require.NoError(t, stream.Send(&providerv1.PaymentSessionRequest{
		SessionId: startResp.Msg.SessionId,
		Message: &providerv1.PaymentSessionRequest_RavSubmission{
			RavSubmission: &providerv1.SignedRAVSubmission{
				SignedRav: sidecar.HorizonSignedRAVToProto(signedRAV1),
				Usage:     resp.GetRavRequest().GetUsage(),
			},
		},
	}))

	resp2, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp2.GetSessionControl())
	require.Equal(t, providerv1.SessionControl_ACTION_CONTINUE, resp2.GetSessionControl().GetAction())

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}
