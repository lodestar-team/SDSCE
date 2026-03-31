package sidecar

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"time"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
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
	runtimeCtx, bootstrap, tracker, stopState, reporterDone, cancel, cleanup, err := s.prepareIngressRuntime(stream.Context(), input)
	if err != nil {
		return err
	}
	defer cleanup()
	defer s.finishIngressRuntime(bootstrap.LocalSession.ID, tracker, stopState, cancel, reporterDone)

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

		trackV2ResponseUsage(resp, tracker)
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
	runtimeCtx, bootstrap, tracker, stopState, reporterDone, cancel, cleanup, err := s.prepareIngressRuntime(stream.Context(), input)
	if err != nil {
		return err
	}
	defer cleanup()
	defer s.finishIngressRuntime(bootstrap.LocalSession.ID, tracker, stopState, cancel, reporterDone)

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

		trackV2ResponseUsage(resp, tracker)
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
	runtimeCtx, bootstrap, tracker, stopState, reporterDone, cancel, cleanup, err := s.prepareIngressRuntime(stream.Context(), input)
	if err != nil {
		return err
	}
	defer cleanup()
	defer s.finishIngressRuntime(bootstrap.LocalSession.ID, tracker, stopState, cancel, reporterDone)

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

		trackV4ResponseUsage(resp, tracker)
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

func (s *Sidecar) prepareIngressRuntime(
	ctx context.Context,
	input sessionBootstrapInput,
) (context.Context, *sessionBootstrapResult, *sds.UsageTracker, *ingressStopState, chan struct{}, context.CancelFunc, func(), error) {
	bootstrap, err := s.bootstrapManagedSession(ctx, input)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, err
	}

	runtimeCtx, cancel := context.WithCancel(ctx)
	tracker := sds.NewUsageTracker(nil)
	stopState := newIngressStopState(cancel)
	reporterDone := make(chan struct{})

	go s.runIngressUsageReporter(runtimeCtx, bootstrap.LocalSession.ID, tracker, stopState, reporterDone)

	cleanup := func() {
		s.paymentSessions.Close(bootstrap.LocalSession.ID)
	}

	return runtimeCtx, bootstrap, tracker, stopState, reporterDone, cancel, cleanup, nil
}

func (s *Sidecar) runIngressUsageReporter(
	ctx context.Context,
	sessionID string,
	tracker *sds.UsageTracker,
	stopState *ingressStopState,
	done chan struct{},
) {
	defer close(done)

	interval := s.ingressConfig.effectiveReportInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.reportIngressUsage(ctx, sessionID, tracker); err != nil {
				stopState.set(err)
				return
			}
		}
	}
}

func (s *Sidecar) reportIngressUsage(ctx context.Context, sessionID string, tracker *sds.UsageTracker) error {
	_, blocksProcessed, bytes, reqs := tracker.SwapAndGetUsage()
	if blocksProcessed == 0 && bytes == 0 {
		return nil
	}

	resp, err := s.ReportUsage(ctx, connect.NewRequest(&consumerv1.ReportUsageRequest{
		SessionId: sessionID,
		Usage: &commonv1.Usage{
			BlocksProcessed:  blocksProcessed,
			BytesTransferred: bytes,
			Requests:         reqs,
			Cost:             commonv1.GRTFromNative(sds.ZeroGRT()),
		},
	}))
	if err != nil {
		return status.Errorf(codes.Unavailable, "report usage via consumer sidecar: %v", err)
	}

	if !resp.Msg.GetShouldContinue() {
		reason := resp.Msg.GetStopReason()
		if reason == "" {
			reason = "provider ended stream due to payment issue"
		}
		return status.Error(codes.ResourceExhausted, reason)
	}

	return nil
}

func (s *Sidecar) finishIngressRuntime(
	sessionID string,
	tracker *sds.UsageTracker,
	stopState *ingressStopState,
	cancel context.CancelFunc,
	reporterDone chan struct{},
) {
	if stopState.errValue() == nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.reportIngressUsage(flushCtx, sessionID, tracker)
		flushCancel()
	}

	cancel()
	<-reporterDone

	if stopState.errValue() == nil {
		endCtx, endCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = s.EndSession(endCtx, connect.NewRequest(&consumerv1.EndSessionRequest{SessionId: sessionID}))
		endCancel()
	}

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

func trackV2ResponseUsage(resp *pbsubstreamsrpcv2.Response, tracker *sds.UsageTracker) {
	if resp == nil || tracker == nil {
		return
	}

	block := resp.GetBlockScopedData()
	if block == nil {
		return
	}

	tracker.AddBlock(blockOutputBytes(block))
}

func trackV4ResponseUsage(resp *pbsubstreamsrpcv4.Response, tracker *sds.UsageTracker) {
	if resp == nil || tracker == nil {
		return
	}

	blocks := resp.GetBlockScopedDatas()
	if blocks == nil {
		return
	}

	for _, block := range blocks.Items {
		tracker.AddBlock(blockOutputBytes(block))
	}
}

func blockOutputBytes(block *pbsubstreamsrpcv2.BlockScopedData) uint64 {
	if block == nil || block.Output == nil || block.Output.MapOutput == nil {
		return 0
	}

	return uint64(len(block.Output.MapOutput.Value))
}
