package sidecar

import (
	"context"
	"sync"

	"connectrpc.com/connect"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
)

type paymentSessionManager struct {
	mu sync.RWMutex
	m  map[string]*paymentSessionClient
}

func newPaymentSessionManager() *paymentSessionManager {
	return &paymentSessionManager{
		m: make(map[string]*paymentSessionClient),
	}
}

func (m *paymentSessionManager) SetEndpoint(sessionID, providerEndpoint string) {
	if sessionID == "" || providerEndpoint == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	client := m.m[sessionID]
	if client == nil {
		client = newPaymentSessionClient(providerEndpoint)
		m.m[sessionID] = client
		return
	}

	client.SetEndpoint(providerEndpoint)
}

func (m *paymentSessionManager) Get(sessionID string) *paymentSessionClient {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.m[sessionID]
}

func (m *paymentSessionManager) Close(sessionID string) {
	m.mu.Lock()
	client := m.m[sessionID]
	delete(m.m, sessionID)
	m.mu.Unlock()

	if client != nil {
		client.Close()
	}
}

type paymentSessionClient struct {
	mu sync.Mutex

	providerEndpoint string
	gatewayClient    providerv1connect.PaymentGatewayServiceClient

	ctx    context.Context
	cancel context.CancelFunc

	stream       *connect.BidiStreamForClient[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse]
	streamCancel context.CancelFunc
	receiveCh    chan paymentSessionReceiveResult
}

type paymentSessionReceiveResult struct {
	msg *providerv1.PaymentSessionResponse
	err error
}

func newPaymentSessionClient(providerEndpoint string) *paymentSessionClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &paymentSessionClient{
		providerEndpoint: providerEndpoint,
		gatewayClient:    newPaymentGatewayClient(providerEndpoint),
		ctx:              ctx,
		cancel:           cancel,
	}
}

func (c *paymentSessionClient) SetEndpoint(providerEndpoint string) {
	if providerEndpoint == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.providerEndpoint == providerEndpoint {
		return
	}

	c.closeLocked()
	c.providerEndpoint = providerEndpoint
	c.gatewayClient = newPaymentGatewayClient(providerEndpoint)
}

func (c *paymentSessionClient) WithStream(
	fn func(
		stream *connect.BidiStreamForClient[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
		receive func(context.Context) (*providerv1.PaymentSessionResponse, error),
	) error,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	stream := c.ensureStreamLocked()
	if err := fn(stream, c.receiveLocked); err != nil {
		c.closeLocked()
		return err
	}

	return nil
}

func (c *paymentSessionClient) ensureStreamLocked() *connect.BidiStreamForClient[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse] {
	if c.stream != nil {
		return c.stream
	}

	streamCtx, streamCancel := context.WithCancel(c.ctx)
	c.stream = c.gatewayClient.PaymentSession(c.ctx)
	c.streamCancel = streamCancel
	c.receiveCh = make(chan paymentSessionReceiveResult, 1)
	go c.receiveLoop(streamCtx, c.stream, c.receiveCh)
	return c.stream
}

func (c *paymentSessionClient) receiveLocked(ctx context.Context) (*providerv1.PaymentSessionResponse, error) {
	if c.receiveCh == nil {
		return nil, context.Canceled
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res, ok := <-c.receiveCh:
		if !ok {
			return nil, context.Canceled
		}
		return res.msg, res.err
	}
}

func (c *paymentSessionClient) receiveLoop(
	ctx context.Context,
	stream *connect.BidiStreamForClient[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
	ch chan paymentSessionReceiveResult,
) {
	defer close(ch)

	for {
		msg, err := stream.Receive()
		select {
		case ch <- paymentSessionReceiveResult{msg: msg, err: err}:
		case <-ctx.Done():
			return
		}

		if err != nil {
			return
		}
	}
}

func (c *paymentSessionClient) closeLocked() {
	if c.streamCancel != nil {
		c.streamCancel()
		c.streamCancel = nil
	}
	if c.stream != nil {
		_ = c.stream.CloseRequest()
		_ = c.stream.CloseResponse()
		c.stream = nil
	}
	c.receiveCh = nil
}

func (c *paymentSessionClient) Close() {
	c.mu.Lock()
	c.closeLocked()
	c.cancel()
	c.mu.Unlock()
}

func newPaymentGatewayClient(providerEndpoint string) providerv1connect.PaymentGatewayServiceClient {
	parsedEndpoint := sidecarlib.ParseEndpoint(providerEndpoint)
	return providerv1connect.NewPaymentGatewayServiceClient(parsedEndpoint.GRPCClient(), parsedEndpoint.URL, connect.WithGRPC())
}
