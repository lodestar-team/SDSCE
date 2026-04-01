package sidecar

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"sync"

	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	pbfirehose "github.com/streamingfast/pbgo/sf/firehose/v2"
	ssclient "github.com/streamingfast/substreams/client"
	pbsubstreamsrpcv2 "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v2"
	pbsubstreamsrpcv3 "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v3"
	pbsubstreamsrpcv4 "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v4"
	pbsubstreams "github.com/streamingfast/substreams/pb/sf/substreams/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type ingressV2Server struct {
	pbsubstreamsrpcv2.UnimplementedStreamServer
	sidecar *Sidecar
}

type ingressEndpointInfoServer struct {
	pbsubstreamsrpcv2.UnimplementedEndpointInfoServer
	sidecar *Sidecar
}

type ingressV3Server struct {
	pbsubstreamsrpcv3.UnimplementedStreamServer
	sidecar *Sidecar
}

type ingressV4Server struct {
	pbsubstreamsrpcv4.UnimplementedStreamServer
	sidecar *Sidecar
}

type ingressStopState struct {
	cancel context.CancelFunc

	mu   sync.Mutex
	err  error
	done bool
}

func newIngressStopState(cancel context.CancelFunc) *ingressStopState {
	return &ingressStopState{cancel: cancel}
}

func (s *ingressStopState) set(err error) {
	if err == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done {
		return
	}

	s.err = err
	s.done = true
	s.cancel()
}

func (s *ingressStopState) errValue() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *ingressEndpointInfoServer) Info(context.Context, *pbfirehose.InfoRequest) (*pbfirehose.InfoResponse, error) {
	// The ingress resolves provider/network per stream request, so Info cannot
	// faithfully proxy provider-specific chain metadata up front. Return a
	// minimal response instead of failing the endpoint entirely.
	return &pbfirehose.InfoResponse{}, nil
}

func (s *ingressV2Server) Blocks(req *pbsubstreamsrpcv2.Request, stream grpc.ServerStreamingServer[pbsubstreamsrpcv2.Response]) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is required")
	}

	ingressConfig, err := s.sidecar.requireIngressConfig()
	if err != nil {
		return err
	}

	if ingressConfig.ProviderControlPlaneEndpoint == "" {
		return status.Error(codes.FailedPrecondition, "oracle-backed ingress requires a v3/v4 Substreams request containing package/network context")
	}

	return s.sidecar.proxyV2Stream(stream, req, sessionBootstrapInput{
		Payer:                        ingressConfig.Payer,
		Receiver:                     ingressConfig.Receiver,
		DataService:                  ingressConfig.DataService,
		ProviderControlPlaneEndpoint: ingressConfig.ProviderControlPlaneEndpoint,
		SubstreamsPackage:            &pbsubstreams.Package{Modules: req.Modules},
	})
}

func (s *ingressV3Server) Blocks(req *pbsubstreamsrpcv3.Request, stream grpc.ServerStreamingServer[pbsubstreamsrpcv2.Response]) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is required")
	}

	ingressConfig, err := s.sidecar.requireIngressConfig()
	if err != nil {
		return err
	}

	return s.sidecar.proxyV3Stream(stream, req, sessionBootstrapInput{
		Payer:                        ingressConfig.Payer,
		Receiver:                     ingressConfig.Receiver,
		DataService:                  ingressConfig.DataService,
		ProviderControlPlaneEndpoint: ingressConfig.ProviderControlPlaneEndpoint,
		SubstreamsPackage:            req.Package,
		RequestedNetwork:             req.Network,
	})
}

func (s *ingressV4Server) Blocks(req *pbsubstreamsrpcv3.Request, stream grpc.ServerStreamingServer[pbsubstreamsrpcv4.Response]) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is required")
	}

	ingressConfig, err := s.sidecar.requireIngressConfig()
	if err != nil {
		return err
	}

	return s.sidecar.proxyV4Stream(stream, req, sessionBootstrapInput{
		Payer:                        ingressConfig.Payer,
		Receiver:                     ingressConfig.Receiver,
		DataService:                  ingressConfig.DataService,
		ProviderControlPlaneEndpoint: ingressConfig.ProviderControlPlaneEndpoint,
		SubstreamsPackage:            req.Package,
		RequestedNetwork:             req.Network,
	})
}

func (s *Sidecar) requireIngressConfig() (*IngressConfig, error) {
	if s.ingressConfig == nil {
		return nil, status.Error(codes.FailedPrecondition, "consumer ingress runtime is not configured")
	}

	return s.ingressConfig, nil
}

func (s *Sidecar) proxyV2Stream(
	stream grpc.ServerStreamingServer[pbsubstreamsrpcv2.Response],
	req *pbsubstreamsrpcv2.Request,
	input sessionBootstrapInput,
) error {
	runtimeCtx, bootstrap, stopState, controlDone, cancel, cleanup, err := s.prepareIngressRuntime(stream.Context(), input)
	if err != nil {
		return err
	}
	defer cleanup()
	defer s.finishIngressRuntime(bootstrap.LocalSession.ID, stopState, cancel, controlDone)

	upstreamConn, closeConn, callOpts, headers, err := newUpstreamStreamConn(bootstrap.DataPlaneEndpoint)
	if err != nil {
		return status.Errorf(codes.Unavailable, "connect upstream Substreams endpoint: %v", err)
	}
	defer closeConn()

	upstreamCtx, err := newUpstreamStreamContext(runtimeCtx, headers, bootstrap.PaymentRAV, bootstrap.LocalSession.ID)
	if err != nil {
		return status.Errorf(codes.Internal, "encode SDS upstream headers: %v", err)
	}

	upstream, err := pbsubstreamsrpcv2.NewStreamClient(upstreamConn).Blocks(upstreamCtx, req, callOpts...)
	if err != nil {
		return status.Errorf(codes.Unavailable, "open upstream Blocks stream: %v", err)
	}

	for {
		resp, err := upstream.Recv()
		if err == io.EOF {
			if stopErr := stopState.errValue(); stopErr != nil {
				return stopErr
			}
			return nil
		}
		if err != nil {
			if stopErr := stopState.errValue(); stopErr != nil {
				return stopErr
			}
			return err
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

func (s *Sidecar) proxyV3Stream(
	stream grpc.ServerStreamingServer[pbsubstreamsrpcv2.Response],
	req *pbsubstreamsrpcv3.Request,
	input sessionBootstrapInput,
) error {
	runtimeCtx, bootstrap, stopState, controlDone, cancel, cleanup, err := s.prepareIngressRuntime(stream.Context(), input)
	if err != nil {
		return err
	}
	defer cleanup()
	defer s.finishIngressRuntime(bootstrap.LocalSession.ID, stopState, cancel, controlDone)

	upstreamConn, closeConn, callOpts, headers, err := newUpstreamStreamConn(bootstrap.DataPlaneEndpoint)
	if err != nil {
		return status.Errorf(codes.Unavailable, "connect upstream Substreams endpoint: %v", err)
	}
	defer closeConn()

	upstreamCtx, err := newUpstreamStreamContext(runtimeCtx, headers, bootstrap.PaymentRAV, bootstrap.LocalSession.ID)
	if err != nil {
		return status.Errorf(codes.Internal, "encode SDS upstream headers: %v", err)
	}

	upstream, err := pbsubstreamsrpcv3.NewStreamClient(upstreamConn).Blocks(upstreamCtx, req, callOpts...)
	if err != nil {
		return status.Errorf(codes.Unavailable, "open upstream Blocks stream: %v", err)
	}

	for {
		resp, err := upstream.Recv()
		if err == io.EOF {
			if stopErr := stopState.errValue(); stopErr != nil {
				return stopErr
			}
			return nil
		}
		if err != nil {
			if stopErr := stopState.errValue(); stopErr != nil {
				return stopErr
			}
			return err
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

func (s *Sidecar) proxyV4Stream(
	stream grpc.ServerStreamingServer[pbsubstreamsrpcv4.Response],
	req *pbsubstreamsrpcv3.Request,
	input sessionBootstrapInput,
) error {
	runtimeCtx, bootstrap, stopState, controlDone, cancel, cleanup, err := s.prepareIngressRuntime(stream.Context(), input)
	if err != nil {
		return err
	}
	defer cleanup()
	defer s.finishIngressRuntime(bootstrap.LocalSession.ID, stopState, cancel, controlDone)

	upstreamConn, closeConn, callOpts, headers, err := newUpstreamStreamConn(bootstrap.DataPlaneEndpoint)
	if err != nil {
		return status.Errorf(codes.Unavailable, "connect upstream Substreams endpoint: %v", err)
	}
	defer closeConn()

	upstreamCtx, err := newUpstreamStreamContext(runtimeCtx, headers, bootstrap.PaymentRAV, bootstrap.LocalSession.ID)
	if err != nil {
		return status.Errorf(codes.Internal, "encode SDS upstream headers: %v", err)
	}

	upstream, err := pbsubstreamsrpcv4.NewStreamClient(upstreamConn).Blocks(upstreamCtx, req, callOpts...)
	if err != nil {
		return status.Errorf(codes.Unavailable, "open upstream Blocks stream: %v", err)
	}

	for {
		resp, err := upstream.Recv()
		if err == io.EOF {
			if stopErr := stopState.errValue(); stopErr != nil {
				return stopErr
			}
			return nil
		}
		if err != nil {
			if stopErr := stopState.errValue(); stopErr != nil {
				return stopErr
			}
			return err
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

func (s *Sidecar) prepareIngressRuntime(
	ctx context.Context,
	input sessionBootstrapInput,
) (context.Context, *sessionBootstrapResult, *ingressStopState, chan struct{}, context.CancelFunc, func(), error) {
	bootstrap, err := s.bootstrapManagedSession(ctx, input)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	runtimeCtx, cancel := context.WithCancel(ctx)
	stopState := newIngressStopState(cancel)
	controlDone := make(chan struct{})

	go s.runIngressPaymentControl(runtimeCtx, bootstrap.LocalSession, stopState, controlDone)

	cleanup := func() {
		s.paymentSessions.Close(bootstrap.LocalSession.ID)
	}

	return runtimeCtx, bootstrap, stopState, controlDone, cancel, cleanup, nil
}

func (s *Sidecar) runIngressPaymentControl(
	ctx context.Context,
	session *sidecarlib.Session,
	stopState *ingressStopState,
	done chan struct{},
) {
	defer close(done)

	if session == nil {
		stopState.set(status.Error(codes.Internal, "consumer ingress session is required"))
		return
	}

	client := s.paymentSessions.Get(session.ID)
	if client == nil {
		stopState.set(status.Error(codes.Unavailable, "provider payment session client is not configured"))
		return
	}

	if err := client.BindSession(session.ID); err != nil {
		stopState.set(status.Errorf(codes.Unavailable, "bind provider payment session: %v", err))
		return
	}

	var pendingRAV *horizon.SignedRAV

	for {
		msg, err := client.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			stopState.set(status.Errorf(codes.Unavailable, "receive provider payment control: %v", err))
			return
		}
		if msg == nil {
			continue
		}

		if ravReq := msg.GetRavRequest(); ravReq != nil {
			signed, err := s.signRAVForRequest(session, ravReq)
			if err != nil {
				stopState.set(status.Errorf(codes.Internal, "sign RAV for provider request: %v", err))
				return
			}

			if usage := ravReq.GetUsage(); usage != nil {
				var usageCost *big.Int
				if usage.Cost != nil {
					usageCost = usage.Cost.ToBigInt()
				}
				session.AddUsage(usage.BlocksProcessed, usage.BytesTransferred, usage.Requests, usageCost)
			}

			if err := client.SendRAVSubmission(session.ID, signed, ravReq.GetUsage()); err != nil {
				stopState.set(status.Errorf(codes.Unavailable, "submit RAV on provider payment session: %v", err))
				return
			}

			pendingRAV = signed
			continue
		}

		if need := msg.GetNeedMoreFunds(); need != nil {
			reason := "need more funds"
			if minimum := need.GetMinimumNeeded(); minimum != nil {
				minimumValue := minimum.ToNative()
				reason = fmt.Sprintf("need more funds (minimum needed %s)", minimumValue.String())
			}
			stopState.set(status.Error(codes.ResourceExhausted, reason))
			return
		}

		if ctrl := msg.GetSessionControl(); ctrl != nil {
			if ctrl.GetAction() == providerv1.SessionControl_ACTION_CONTINUE {
				if pendingRAV != nil {
					session.SetRAV(pendingRAV)
					pendingRAV = nil
				}
				continue
			}

			reason := ctrl.GetReason()
			if reason == "" {
				reason = "provider ended stream"
			}
			stopState.set(status.Error(codes.ResourceExhausted, reason))
			return
		}
	}
}

func (s *Sidecar) finishIngressRuntime(
	sessionID string,
	stopState *ingressStopState,
	cancel context.CancelFunc,
	controlDone chan struct{},
) {
	cancel()
	<-controlDone
	s.sessions.Delete(sessionID)
}

func newUpstreamStreamConn(endpoint string) (*grpc.ClientConn, func() error, []grpc.CallOption, ssclient.Headers, error) {
	parsed := sidecarlib.ParseEndpoint(endpoint)
	config := ssclient.NewSubstreamsClientConfig(ssclient.SubstreamsClientConfigOptions{
		Endpoint:  parsed.URL,
		Insecure:  parsed.Insecure,
		PlainText: parsed.Plaintext,
	})

	return ssclient.NewSubstreamsClientConn(config)
}

func newUpstreamStreamContext(
	ctx context.Context,
	headers ssclient.Headers,
	paymentRAV *commonv1.SignedRAV,
	sessionID string,
) (context.Context, error) {
	ravHeader, err := encodeRAVHeader(paymentRAV)
	if err != nil {
		return nil, err
	}

	if headers == nil {
		headers = make(ssclient.Headers)
	}

	headers = headers.Append(map[string]string{
		sds.HeaderRAV:       ravHeader,
		sds.HeaderSessionID: sessionID,
	})

	return metadata.AppendToOutgoingContext(ctx, headers.ToArray()...), nil
}

func encodeRAVHeader(signedRAV *commonv1.SignedRAV) (string, error) {
	protoBytes, err := proto.Marshal(signedRAV)
	if err != nil {
		return "", fmt.Errorf("marshal proto: %w", err)
	}

	return base64.StdEncoding.EncodeToString(protoBytes), nil
}
