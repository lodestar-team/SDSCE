package sidecar

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sync"

	"connectrpc.com/connect"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	"golang.org/x/net/http2"
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

	// Endpoint should never change for a session; if it does, reset stream.
	if client.providerEndpoint != providerEndpoint {
		client.closeLocked()
		client.providerEndpoint = providerEndpoint
		client.gatewayClient = newPaymentGatewayClient(providerEndpoint)
	}
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

	stream *connect.BidiStreamForClient[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse]
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

func (c *paymentSessionClient) ensureStreamLocked() *connect.BidiStreamForClient[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse] {
	if c.stream != nil {
		return c.stream
	}

	c.stream = c.gatewayClient.PaymentSession(c.ctx)
	return c.stream
}

func (c *paymentSessionClient) closeLocked() {
	if c.stream != nil {
		_ = c.stream.CloseRequest()
		_ = c.stream.CloseResponse()
		c.stream = nil
	}
}

func (c *paymentSessionClient) Close() {
	c.mu.Lock()
	c.closeLocked()
	c.cancel()
	c.mu.Unlock()
}

func newPaymentGatewayClient(providerEndpoint string) providerv1connect.PaymentGatewayServiceClient {
	return providerv1connect.NewPaymentGatewayServiceClient(newH2CClient(), providerEndpoint, connect.WithGRPC())
}

func newH2CClient() *http.Client {
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
}
