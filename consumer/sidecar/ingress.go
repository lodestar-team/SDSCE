package sidecar

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sync"
	"time"

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

type ingressControlResultKind uint8

const (
	ingressControlResultNone ingressControlResultKind = iota
	ingressControlResultSemanticStop
	ingressControlResultControlFailure
	ingressControlResultCanceled
)

type ingressControlResult struct {
	kind ingressControlResultKind
	err  error
}

type ingressTerminationCoordinator struct {
	cancel context.CancelFunc

	mu                    sync.Mutex
	result                ingressControlResult
	paymentControlPending bool
	changed               chan struct{}
}

type ingressStreamProgress struct {
	sawBlock     bool
	highestBlock uint64
}

type ingressSessionStatusGetter func(context.Context, string) (*providerv1.GetSessionStatusResponse, error)

const ambiguousIngressStatusPollInterval = 50 * time.Millisecond

func newIngressTerminationCoordinator(cancel context.CancelFunc) *ingressTerminationCoordinator {
	return &ingressTerminationCoordinator{
		cancel:  cancel,
		changed: make(chan struct{}),
	}
}

func (c *ingressTerminationCoordinator) setResult(kind ingressControlResultKind, err error, cancelRuntime bool) {
	if err == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.result.kind != ingressControlResultNone {
		return
	}

	c.result = ingressControlResult{
		kind: kind,
		err:  err,
	}
	if cancelRuntime {
		c.cancel()
	}
}

func (c *ingressTerminationCoordinator) setSemanticStop(err error) {
	c.setResult(ingressControlResultSemanticStop, err, true)
}

func (c *ingressTerminationCoordinator) setControlFailure(err error) {
	c.setResult(ingressControlResultControlFailure, err, true)
}

func (c *ingressTerminationCoordinator) setCanceled() {
	c.setResult(ingressControlResultCanceled, context.Canceled, false)
}

func (c *ingressTerminationCoordinator) setPaymentControlPending(pending bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.paymentControlPending == pending {
		return
	}

	c.paymentControlPending = pending
	close(c.changed)
	c.changed = make(chan struct{})
}

func (c *ingressTerminationCoordinator) paymentControlState() (bool, <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paymentControlPending, c.changed
}

func (c *ingressTerminationCoordinator) currentResult() ingressControlResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.result
}

func (c *ingressTerminationCoordinator) currentError() error {
	return c.currentResult().err
}

func (p *ingressStreamProgress) noteBlock(blockNum uint64) {
	if !p.sawBlock || blockNum > p.highestBlock {
		p.highestBlock = blockNum
	}
	p.sawBlock = true
}

func (p *ingressStreamProgress) observeV2(resp *pbsubstreamsrpcv2.Response) {
	if resp == nil {
		return
	}

	if data := resp.GetBlockScopedData(); data != nil && data.GetClock() != nil {
		p.noteBlock(data.GetClock().GetNumber())
	}

	if undo := resp.GetBlockUndoSignal(); undo != nil && undo.GetLastValidBlock() != nil {
		p.noteBlock(undo.GetLastValidBlock().GetNumber())
	}
}

func (p *ingressStreamProgress) observeV4(resp *pbsubstreamsrpcv4.Response) {
	if resp == nil {
		return
	}

	if batch := resp.GetBlockScopedDatas(); batch != nil {
		for _, item := range batch.GetItems() {
			if item != nil && item.GetClock() != nil {
				p.noteBlock(item.GetClock().GetNumber())
			}
		}
	}

	if undo := resp.GetBlockUndoSignal(); undo != nil && undo.GetLastValidBlock() != nil {
		p.noteBlock(undo.GetLastValidBlock().GetNumber())
	}
}

func (p *ingressStreamProgress) expectedEOF(stopBlockNum uint64) bool {
	if stopBlockNum == 0 || !p.sawBlock {
		return false
	}

	return p.highestBlock+1 >= stopBlockNum
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
	runtimeCtx, bootstrap, coordinator, controlDone, cancel, cleanup, err := s.prepareIngressRuntime(stream.Context(), input)
	if err != nil {
		return err
	}
	defer cleanup()
	defer s.finishIngressRuntime(bootstrap.LocalSession.ID, cancel, controlDone)

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

	progress := &ingressStreamProgress{}

	for {
		resp, err := upstream.Recv()
		if err != nil {
			return s.resolveIngressStreamTermination(stream.Context(), runtimeCtx, err, progress.expectedEOF(req.GetStopBlockNum()), bootstrap.LocalSession.ID, coordinator, controlDone)
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
		progress.observeV2(resp)
	}
}

func (s *Sidecar) proxyV3Stream(
	stream grpc.ServerStreamingServer[pbsubstreamsrpcv2.Response],
	req *pbsubstreamsrpcv3.Request,
	input sessionBootstrapInput,
) error {
	runtimeCtx, bootstrap, coordinator, controlDone, cancel, cleanup, err := s.prepareIngressRuntime(stream.Context(), input)
	if err != nil {
		return err
	}
	defer cleanup()
	defer s.finishIngressRuntime(bootstrap.LocalSession.ID, cancel, controlDone)

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

	progress := &ingressStreamProgress{}

	for {
		resp, err := upstream.Recv()
		if err != nil {
			return s.resolveIngressStreamTermination(stream.Context(), runtimeCtx, err, progress.expectedEOF(req.GetStopBlockNum()), bootstrap.LocalSession.ID, coordinator, controlDone)
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
		progress.observeV2(resp)
	}
}

func (s *Sidecar) proxyV4Stream(
	stream grpc.ServerStreamingServer[pbsubstreamsrpcv4.Response],
	req *pbsubstreamsrpcv3.Request,
	input sessionBootstrapInput,
) error {
	runtimeCtx, bootstrap, coordinator, controlDone, cancel, cleanup, err := s.prepareIngressRuntime(stream.Context(), input)
	if err != nil {
		return err
	}
	defer cleanup()
	defer s.finishIngressRuntime(bootstrap.LocalSession.ID, cancel, controlDone)

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

	progress := &ingressStreamProgress{}

	for {
		resp, err := upstream.Recv()
		if err != nil {
			return s.resolveIngressStreamTermination(stream.Context(), runtimeCtx, err, progress.expectedEOF(req.GetStopBlockNum()), bootstrap.LocalSession.ID, coordinator, controlDone)
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
		progress.observeV4(resp)
	}
}

func (s *Sidecar) prepareIngressRuntime(
	ctx context.Context,
	input sessionBootstrapInput,
) (context.Context, *sessionBootstrapResult, *ingressTerminationCoordinator, chan struct{}, context.CancelFunc, func(), error) {
	bootstrap, err := s.bootstrapManagedSession(ctx, input)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	runtimeCtx, cancel := context.WithCancel(ctx)
	coordinator := newIngressTerminationCoordinator(cancel)
	controlDone := make(chan struct{})

	go s.runIngressPaymentControl(runtimeCtx, bootstrap.LocalSession, coordinator, controlDone)

	cleanup := func() {
		s.paymentSessions.Close(bootstrap.LocalSession.ID)
	}

	return runtimeCtx, bootstrap, coordinator, controlDone, cancel, cleanup, nil
}

func (s *Sidecar) runIngressPaymentControl(
	ctx context.Context,
	session *sidecarlib.Session,
	coordinator *ingressTerminationCoordinator,
	done chan struct{},
) {
	defer close(done)

	if session == nil {
		coordinator.setControlFailure(status.Error(codes.Internal, "consumer ingress session is required"))
		return
	}

	client := s.paymentSessions.Get(session.ID)
	if client == nil {
		coordinator.setControlFailure(status.Error(codes.Unavailable, "provider payment session client is not configured"))
		return
	}

	if err := client.BindSession(session.ID); err != nil {
		coordinator.setControlFailure(status.Errorf(codes.Unavailable, "bind provider payment session: %v", err))
		return
	}

	var pendingRAV *horizon.SignedRAV

	for {
		msg, err := client.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				coordinator.setCanceled()
				return
			}
			coordinator.setControlFailure(status.Errorf(codes.Unavailable, "receive provider payment control: %v", err))
			return
		}
		if msg == nil {
			continue
		}

		if ravReq := msg.GetRavRequest(); ravReq != nil {
			coordinator.setPaymentControlPending(true)
			signed, err := s.signRAVForRequest(session, ravReq)
			if err != nil {
				coordinator.setControlFailure(status.Errorf(codes.Internal, "sign RAV for provider request: %v", err))
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
				if statusResp, statusErr := client.GetSessionStatus(ctx, session.ID); statusErr == nil {
					if resolved, resolvedErr := resolveAmbiguousIngressSessionStatus(statusResp); resolved {
						switch {
						case resolvedErr == nil:
							coordinator.setCanceled()
						case status.Code(resolvedErr) == codes.ResourceExhausted:
							coordinator.setSemanticStop(resolvedErr)
						default:
							coordinator.setControlFailure(resolvedErr)
						}
						return
					}
				}
				coordinator.setControlFailure(status.Errorf(codes.Unavailable, "submit RAV on provider payment session: %v", err))
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
			coordinator.setSemanticStop(status.Error(codes.ResourceExhausted, reason))
			return
		}

		if ctrl := msg.GetSessionControl(); ctrl != nil {
			if ctrl.GetAction() == providerv1.SessionControl_ACTION_CONTINUE {
				if pendingRAV != nil {
					session.SetRAV(pendingRAV)
					pendingRAV = nil
				}
				coordinator.setPaymentControlPending(false)
				continue
			}

			reason := ctrl.GetReason()
			if reason == "" {
				reason = "provider ended stream"
			}
			coordinator.setSemanticStop(status.Error(codes.ResourceExhausted, reason))
			return
		}
	}
}

func (s *Sidecar) finishIngressRuntime(
	sessionID string,
	cancel context.CancelFunc,
	controlDone chan struct{},
) {
	cancel()
	<-controlDone
	s.sessions.Delete(sessionID)
}

func (s *Sidecar) resolveIngressStreamTermination(
	clientCtx context.Context,
	runtimeCtx context.Context,
	upstreamErr error,
	expectedEOF bool,
	sessionID string,
	coordinator *ingressTerminationCoordinator,
	controlDone <-chan struct{},
) error {
	if resultErr := coordinator.currentError(); resultErr != nil {
		return resultErr
	}

	if errors.Is(upstreamErr, io.EOF) {
		if expectedEOF {
			return s.awaitFiniteIngressPostStreamControl(clientCtx, sessionID, coordinator, controlDone)
		}

		return s.awaitAmbiguousIngressTerminationResolution(clientCtx, sessionID, coordinator, controlDone)
	}

	if clientCtx.Err() == nil && (runtimeCtx.Err() != nil || isAmbiguousIngressUpstreamError(upstreamErr)) {
		return s.awaitAmbiguousIngressTerminationResolution(clientCtx, sessionID, coordinator, controlDone)
	}

	if resultErr := coordinator.currentError(); resultErr != nil {
		return resultErr
	}

	if !errors.Is(upstreamErr, io.EOF) {
		return upstreamErr
	}

	return nil
}

func (s *Sidecar) awaitFiniteIngressPostStreamControl(
	clientCtx context.Context,
	sessionID string,
	coordinator *ingressTerminationCoordinator,
	controlDone <-chan struct{},
) error {
	if coordinator == nil {
		return nil
	}

	client := s.paymentSessions.Get(sessionID)
	if client == nil {
		return status.Error(codes.Unavailable, "provider payment session client is not configured")
	}

	return awaitFiniteIngressPostStreamControl(
		clientCtx,
		sessionID,
		s.paymentSessionRoundtripTimeout,
		coordinator,
		controlDone,
		client.GetSessionStatus,
	)
}

func awaitFiniteIngressPostStreamControl(
	clientCtx context.Context,
	sessionID string,
	timeout time.Duration,
	coordinator *ingressTerminationCoordinator,
	controlDone <-chan struct{},
	getStatus ingressSessionStatusGetter,
) error {
	deadlineCtx, cancel := context.WithTimeout(clientCtx, timeout)
	defer cancel()

	ticker := time.NewTicker(ambiguousIngressStatusPollInterval)
	defer ticker.Stop()

	var lastStatusErr error

	for {
		if resultErr := coordinator.currentError(); resultErr != nil {
			return resultErr
		}

		localPending, changed := coordinator.paymentControlState()
		statusResp, err := getStatus(deadlineCtx, sessionID)
		if err == nil {
			resolved, resolvedErr := resolveAmbiguousIngressSessionStatus(statusResp)
			if resolved {
				return resolvedErr
			}
			if !localPending && !statusResp.GetPaymentControlPending() {
				return nil
			}
		} else {
			lastStatusErr = err
		}

		select {
		case <-clientCtx.Done():
			if resultErr := coordinator.currentError(); resultErr != nil {
				return resultErr
			}
			return clientCtx.Err()
		case <-deadlineCtx.Done():
			if resultErr := coordinator.currentError(); resultErr != nil {
				return resultErr
			}
			if lastStatusErr != nil {
				return status.Errorf(codes.Unavailable, "finite upstream Substreams stream ended before provider payment control status resolved the session: %v", lastStatusErr)
			}
			return status.Errorf(codes.Unavailable, "finite upstream Substreams stream ended while provider payment control remained pending for %s", timeout)
		case <-changed:
		case <-ticker.C:
		case <-controlDone:
			controlDone = nil
		}
	}
}

func isAmbiguousIngressUpstreamError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) {
		return true
	}

	switch status.Code(err) {
	case codes.Canceled, codes.Unavailable:
		return true
	default:
		return false
	}
}

func (s *Sidecar) awaitAmbiguousIngressTerminationResolution(
	clientCtx context.Context,
	sessionID string,
	coordinator *ingressTerminationCoordinator,
	controlDone <-chan struct{},
) error {
	if resultErr := coordinator.currentError(); resultErr != nil {
		return resultErr
	}

	client := s.paymentSessions.Get(sessionID)
	if client == nil {
		return status.Error(codes.Unavailable, "provider payment session client is not configured")
	}

	return awaitAmbiguousIngressTerminationResolution(
		clientCtx,
		sessionID,
		s.paymentSessionRoundtripTimeout,
		coordinator,
		controlDone,
		client.GetSessionStatus,
	)
}

func resolveAmbiguousIngressSessionStatus(resp *providerv1.GetSessionStatusResponse) (bool, error) {
	if resp == nil || resp.GetActive() {
		return false, nil
	}

	switch resp.GetEndReason() {
	case commonv1.EndReason_END_REASON_COMPLETE, commonv1.EndReason_END_REASON_CLIENT_DISCONNECT:
		return true, nil
	case commonv1.EndReason_END_REASON_PAYMENT_ISSUE:
		return true, status.Error(codes.ResourceExhausted, "need more funds")
	case commonv1.EndReason_END_REASON_PROVIDER_STOP:
		return true, status.Error(codes.Unavailable, "provider session terminated with end reason provider stop")
	case commonv1.EndReason_END_REASON_ERROR:
		return true, status.Error(codes.Unavailable, "provider session terminated with end reason error")
	case commonv1.EndReason_END_REASON_UNSPECIFIED:
		fallthrough
	default:
		return true, status.Errorf(codes.Unavailable, "provider session terminated with end reason %s", resp.GetEndReason().String())
	}
}

func awaitAmbiguousIngressTerminationResolution(
	clientCtx context.Context,
	sessionID string,
	timeout time.Duration,
	coordinator *ingressTerminationCoordinator,
	controlDone <-chan struct{},
	getStatus ingressSessionStatusGetter,
) error {
	deadlineCtx, cancel := context.WithTimeout(clientCtx, timeout)
	defer cancel()

	ticker := time.NewTicker(ambiguousIngressStatusPollInterval)
	defer ticker.Stop()

	var lastStatusErr error

	for {
		if coordinator != nil {
			if resultErr := coordinator.currentError(); resultErr != nil {
				return resultErr
			}
		}

		statusResp, err := getStatus(deadlineCtx, sessionID)
		if err == nil {
			resolved, resolvedErr := resolveAmbiguousIngressSessionStatus(statusResp)
			if resolved {
				return resolvedErr
			}
		} else {
			lastStatusErr = err
		}

		select {
		case <-clientCtx.Done():
			if coordinator != nil {
				if resultErr := coordinator.currentError(); resultErr != nil {
					return resultErr
				}
			}
			return clientCtx.Err()
		case <-deadlineCtx.Done():
			if coordinator != nil {
				if resultErr := coordinator.currentError(); resultErr != nil {
					return resultErr
				}
			}
			if lastStatusErr != nil {
				return status.Errorf(codes.Unavailable, "upstream Substreams stream terminated before provider session status resolved the session: %v", lastStatusErr)
			}
			return status.Errorf(codes.Unavailable, "upstream Substreams stream terminated before provider session status resolved the session within %s", timeout)
		case <-ticker.C:
		case <-controlDone:
			controlDone = nil
		}
	}
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
