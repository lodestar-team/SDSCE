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

	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	providergateway "github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/sidecar"
)

func TestPaymentSession_BindsToSessionID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	domain := env.Domain()

	providerConfig := &providergateway.Config{
		ListenAddr:      ":19006",
		ServiceProvider: env.ServiceProvider.Address,
		Domain:          domain,
		CollectorAddr:   env.Collector.Address,
		EscrowAddr:      env.Escrow.Address,
		RPCEndpoint:     env.RPCURL,
		TransportConfig: sidecar.ServerTransportConfig{Plaintext: true},
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
	gatewayClient := providerv1connect.NewPaymentGatewayServiceClient(h2cClient, "http://localhost:19006", connect.WithGRPC())

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

	startResp, err := gatewayClient.StartSession(ctx, connect.NewRequest(&providerv1.StartSessionRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(env.Payer.Address),
			Receiver:    commonv1.AddressFromEth(env.ServiceProvider.Address),
			DataService: commonv1.AddressFromEth(env.DataService.Address),
		},
		InitialRav: sidecar.HorizonSignedRAVToProto(signedRAV),
	}))
	require.NoError(t, err)
	require.True(t, startResp.Msg.Accepted, "expected StartSession accepted: %s", startResp.Msg.RejectionReason)
	require.NotEmpty(t, startResp.Msg.SessionId)

	// Missing session_id should fail with InvalidArgument.
	streamBad := gatewayClient.PaymentSession(ctx)
	require.NoError(t, streamBad.Send(&providerv1.PaymentSessionRequest{
		Message: &providerv1.PaymentSessionRequest_RavSubmission{
			RavSubmission: &providerv1.SignedRAVSubmission{
				SignedRav: sidecar.HorizonSignedRAVToProto(signedRAV),
			},
		},
	}))

	respBad, err := streamBad.Receive()
	require.NoError(t, err)
	require.NotNil(t, respBad.GetSessionControl())
	require.Equal(t, providerv1.SessionControl_ACTION_STOP, respBad.GetSessionControl().GetAction())
	require.Contains(t, respBad.GetSessionControl().GetReason(), "<session_id> is required")

	// Correct session_id should be accepted and return CONTINUE.
	stream := gatewayClient.PaymentSession(ctx)
	require.NoError(t, stream.Send(&providerv1.PaymentSessionRequest{
		SessionId: startResp.Msg.SessionId,
		Message: &providerv1.PaymentSessionRequest_RavSubmission{
			RavSubmission: &providerv1.SignedRAVSubmission{
				SignedRav: sidecar.HorizonSignedRAVToProto(signedRAV),
			},
		},
	}))

	resp, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp.GetSessionControl())
	require.Equal(t, providerv1.SessionControl_ACTION_CONTINUE, resp.GetSessionControl().GetAction())

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}
