package sidecar

import (
	"context"
	"fmt"
	"sync"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
)

type paymentSessionStream interface {
	Send(*providerv1.PaymentSessionRequest) error
	Receive() (*providerv1.PaymentSessionResponse, error)
	CloseRequest() error
	CloseResponse() error
}

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
	// sendMu serializes writes only within the current stream generation.
	sendMu *sync.Mutex

	providerEndpoint string
	gatewayClient    providerv1connect.PaymentGatewayServiceClient
	newStream        func(context.Context) paymentSessionStream

	ctx    context.Context
	cancel context.CancelFunc

	stream           paymentSessionStream
	streamCancel     context.CancelFunc
	receiveCh        chan paymentSessionReceiveResult
	boundSessionID   string
	bindingSessionID string
	bindingDone      chan struct{}
	streamGeneration uint64
}

type paymentSessionReceiveResult struct {
	msg *providerv1.PaymentSessionResponse
	err error
}

func newPaymentSessionClient(providerEndpoint string) *paymentSessionClient {
	ctx, cancel := context.WithCancel(context.Background())
	client := &paymentSessionClient{
		providerEndpoint: providerEndpoint,
		gatewayClient:    newPaymentGatewayClient(providerEndpoint),
		ctx:              ctx,
		cancel:           cancel,
	}
	client.newStream = func(ctx context.Context) paymentSessionStream {
		return client.gatewayClient.PaymentSession(ctx)
	}
	return client
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

func (c *paymentSessionClient) BindSession(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}

	for {
		c.mu.Lock()
		switch {
		case c.boundSessionID == sessionID && c.stream != nil:
			c.mu.Unlock()
			return nil
		case c.bindingSessionID == sessionID:
			waitCh := c.bindingDone
			c.mu.Unlock()
			if waitCh != nil {
				<-waitCh
			}
			continue
		case c.boundSessionID != "" && c.boundSessionID != sessionID:
			err := fmt.Errorf("payment session client already bound to %q", c.boundSessionID)
			c.mu.Unlock()
			return err
		case c.bindingSessionID != "" && c.bindingSessionID != sessionID:
			err := fmt.Errorf("payment session client is currently binding %q", c.bindingSessionID)
			c.mu.Unlock()
			return err
		}

		stream := c.ensureStreamLocked()
		sendMu := c.sendMu
		if c.bindingDone == nil {
			c.bindingDone = make(chan struct{})
		}
		bindingDone := c.bindingDone
		streamGeneration := c.streamGeneration
		c.bindingSessionID = sessionID
		c.mu.Unlock()

		err := c.sendRequest(sendMu, stream, &providerv1.PaymentSessionRequest{SessionId: sessionID})

		c.mu.Lock()
		sameStream := c.stream == stream && c.streamGeneration == streamGeneration
		attemptActive := c.bindingDone == bindingDone && c.bindingSessionID == sessionID
		if err == nil {
			if sameStream && attemptActive {
				c.boundSessionID = sessionID
				c.bindingSessionID = ""
				c.finishBindingLocked()
				c.mu.Unlock()
				return nil
			}

			if attemptActive {
				c.bindingSessionID = ""
				c.finishBindingLocked()
			}
			c.mu.Unlock()
			return fmt.Errorf("bind payment session %q: stream changed while binding", sessionID)
		}

		if sameStream && attemptActive {
			c.closeLocked()
		} else if attemptActive {
			c.bindingSessionID = ""
			c.finishBindingLocked()
		}
		c.mu.Unlock()
		return fmt.Errorf("bind payment session %q: %w", sessionID, err)
	}
}

func (c *paymentSessionClient) SendRAVSubmission(sessionID string, signed *horizon.SignedRAV, usage *commonv1.Usage) error {
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if signed == nil {
		return fmt.Errorf("signed RAV is required")
	}

	for {
		c.mu.Lock()
		switch {
		case c.boundSessionID == sessionID && c.stream != nil:
			stream := c.stream
			sendMu := c.sendMu
			streamGeneration := c.streamGeneration
			c.mu.Unlock()

			if err := c.sendRequest(sendMu, stream, &providerv1.PaymentSessionRequest{
				SessionId: sessionID,
				Message: &providerv1.PaymentSessionRequest_RavSubmission{
					RavSubmission: &providerv1.SignedRAVSubmission{
						SignedRav: sidecarlib.HorizonSignedRAVToProto(signed),
						Usage:     usage,
					},
				},
			}); err != nil {
				c.mu.Lock()
				if c.stream == stream && c.streamGeneration == streamGeneration {
					c.closeLocked()
				}
				c.mu.Unlock()
				return fmt.Errorf("send rav submission for session %q: %w", sessionID, err)
			}

			return nil
		case c.bindingSessionID == sessionID:
			waitCh := c.bindingDone
			c.mu.Unlock()
			if waitCh != nil {
				<-waitCh
			}
			continue
		case c.boundSessionID == "":
			c.mu.Unlock()
			return fmt.Errorf("payment session client is not bound")
		case c.boundSessionID != sessionID:
			err := fmt.Errorf("payment session client is bound to %q, not %q", c.boundSessionID, sessionID)
			c.mu.Unlock()
			return err
		}
	}
}

func (c *paymentSessionClient) Receive(ctx context.Context) (*providerv1.PaymentSessionResponse, error) {
	c.mu.Lock()
	ch := c.receiveCh
	c.mu.Unlock()

	if ch == nil {
		return nil, context.Canceled
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res, ok := <-ch:
		if !ok {
			return nil, context.Canceled
		}
		return res.msg, res.err
	}
}

func (c *paymentSessionClient) GetSessionStatus(ctx context.Context, sessionID string) (*providerv1.GetSessionStatusResponse, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}

	c.mu.Lock()
	client := c.gatewayClient
	c.mu.Unlock()

	resp, err := client.GetSessionStatus(ctx, connect.NewRequest(&providerv1.GetSessionStatusRequest{
		SessionId: sessionID,
	}))
	if err != nil {
		return nil, fmt.Errorf("get session status for %q: %w", sessionID, err)
	}

	return resp.Msg, nil
}

func (c *paymentSessionClient) ensureStreamLocked() paymentSessionStream {
	if c.stream != nil {
		return c.stream
	}

	streamCtx, streamCancel := context.WithCancel(c.ctx)
	c.stream = c.newStream(streamCtx)
	c.sendMu = &sync.Mutex{}
	c.streamCancel = streamCancel
	c.receiveCh = make(chan paymentSessionReceiveResult, 1)
	go c.receiveLoop(streamCtx, c.stream, c.receiveCh)
	return c.stream
}

func (c *paymentSessionClient) receiveLoop(
	ctx context.Context,
	stream paymentSessionStream,
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
	c.streamGeneration++
	if c.streamCancel != nil {
		c.streamCancel()
		c.streamCancel = nil
	}
	if c.stream != nil {
		_ = c.stream.CloseRequest()
		_ = c.stream.CloseResponse()
		c.stream = nil
	}
	c.sendMu = nil
	c.receiveCh = nil
	c.boundSessionID = ""
	c.bindingSessionID = ""
	c.finishBindingLocked()
}

func (c *paymentSessionClient) finishBindingLocked() {
	if c.bindingDone == nil {
		return
	}

	close(c.bindingDone)
	c.bindingDone = nil
}

func (c *paymentSessionClient) sendRequest(sendMu *sync.Mutex, stream paymentSessionStream, req *providerv1.PaymentSessionRequest) error {
	if sendMu == nil {
		return fmt.Errorf("payment session client transport is unavailable")
	}

	sendMu.Lock()
	defer sendMu.Unlock()

	return stream.Send(req)
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
