package sidecar

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/require"
)

type fakePaymentSessionStream struct {
	sendStarted chan *providerv1.PaymentSessionRequest
	sendRelease chan error
	closed      chan struct{}
	closeOnce   sync.Once
	sendCount   atomic.Int32
	ignoreClose bool
}

func newFakePaymentSessionStream() *fakePaymentSessionStream {
	return &fakePaymentSessionStream{
		sendStarted: make(chan *providerv1.PaymentSessionRequest, 8),
		sendRelease: make(chan error, 8),
		closed:      make(chan struct{}),
	}
}

func (s *fakePaymentSessionStream) Send(req *providerv1.PaymentSessionRequest) error {
	s.sendCount.Add(1)

	select {
	case s.sendStarted <- req:
	default:
	}

	if s.ignoreClose {
		return <-s.sendRelease
	}

	select {
	case err := <-s.sendRelease:
		return err
	case <-s.closed:
		return fmt.Errorf("stream closed")
	}
}

func (s *fakePaymentSessionStream) Receive() (*providerv1.PaymentSessionResponse, error) {
	<-s.closed
	return nil, context.Canceled
}

func (s *fakePaymentSessionStream) CloseRequest() error {
	s.closeOnce.Do(func() {
		close(s.closed)
	})
	return nil
}

func (s *fakePaymentSessionStream) CloseResponse() error {
	s.closeOnce.Do(func() {
		close(s.closed)
	})
	return nil
}

func newTestPaymentSessionClient(streamFactory func(context.Context) paymentSessionStream) *paymentSessionClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &paymentSessionClient{
		providerEndpoint: "endpoint-1",
		ctx:              ctx,
		cancel:           cancel,
		newStream:        streamFactory,
	}
}

func waitForSend(t *testing.T, ch <-chan *providerv1.PaymentSessionRequest) *providerv1.PaymentSessionRequest {
	t.Helper()

	select {
	case req := <-ch:
		return req
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for outbound payment-session send")
		return nil
	}
}

func mustSignedRAV(t *testing.T) *horizon.SignedRAV {
	t.Helper()

	key, err := eth.NewPrivateKey("0x0000000000000000000000000000000000000000000000000000000000000042")
	require.NoError(t, err)

	var collectionID horizon.CollectionID
	rav := &horizon.RAV{
		CollectionID:    collectionID,
		Payer:           key.PublicKey().Address(),
		ServiceProvider: eth.MustNewAddress("0x1111111111111111111111111111111111111111"),
		DataService:     eth.MustNewAddress("0x2222222222222222222222222222222222222222"),
		TimestampNs:     1,
		ValueAggregate:  big.NewInt(1),
		Metadata:        nil,
	}

	signed, err := horizon.Sign(horizon.NewDomain(1337, eth.MustNewAddress("0x3333333333333333333333333333333333333333")), rav, key)
	require.NoError(t, err)
	return signed
}

func TestPaymentSessionClient_CloseDoesNotWaitForBindSend(t *testing.T) {
	stream := newFakePaymentSessionStream()
	client := newTestPaymentSessionClient(func(context.Context) paymentSessionStream {
		return stream
	})

	bindErrCh := make(chan error, 1)
	go func() {
		bindErrCh <- client.BindSession("session-close")
	}()

	waitForSend(t, stream.sendStarted)

	closeDone := make(chan struct{})
	go func() {
		client.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close blocked on bind send")
	}

	select {
	case err := <-bindErrCh:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("BindSession did not finish after Close")
	}

	require.Empty(t, client.boundSessionID)
	require.Empty(t, client.bindingSessionID)
}

func TestPaymentSessionClient_BindSessionWaitsForInFlightBind(t *testing.T) {
	stream := newFakePaymentSessionStream()
	client := newTestPaymentSessionClient(func(context.Context) paymentSessionStream {
		return stream
	})

	firstErrCh := make(chan error, 1)
	secondErrCh := make(chan error, 1)

	go func() {
		firstErrCh <- client.BindSession("session-bind")
	}()

	waitForSend(t, stream.sendStarted)

	go func() {
		secondErrCh <- client.BindSession("session-bind")
	}()

	require.Eventually(t, func() bool {
		return stream.sendCount.Load() == 1
	}, time.Second, 10*time.Millisecond)

	stream.sendRelease <- nil

	require.NoError(t, <-firstErrCh)
	require.NoError(t, <-secondErrCh)
	require.Equal(t, int32(1), stream.sendCount.Load())
	require.Equal(t, "session-bind", client.boundSessionID)

	client.Close()
}

func TestPaymentSessionClient_CloseDoesNotWaitForRAVSend(t *testing.T) {
	stream := newFakePaymentSessionStream()
	client := newTestPaymentSessionClient(func(context.Context) paymentSessionStream {
		return stream
	})

	bindErrCh := make(chan error, 1)
	go func() {
		bindErrCh <- client.BindSession("session-rav")
	}()

	waitForSend(t, stream.sendStarted)
	stream.sendRelease <- nil
	require.NoError(t, <-bindErrCh)

	ravErrCh := make(chan error, 1)
	go func() {
		ravErrCh <- client.SendRAVSubmission("session-rav", mustSignedRAV(t), &commonv1.Usage{})
	}()

	waitForSend(t, stream.sendStarted)

	closeDone := make(chan struct{})
	go func() {
		client.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close blocked on RAV send")
	}

	select {
	case err := <-ravErrCh:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("SendRAVSubmission did not finish after Close")
	}
}

func TestPaymentSessionClient_SetEndpointDoesNotResurrectStaleBindingState(t *testing.T) {
	stream1 := newFakePaymentSessionStream()
	stream2 := newFakePaymentSessionStream()

	var client *paymentSessionClient
	client = newTestPaymentSessionClient(func(context.Context) paymentSessionStream {
		if client.providerEndpoint == "endpoint-2" {
			return stream2
		}
		return stream1
	})

	firstErrCh := make(chan error, 1)
	go func() {
		firstErrCh <- client.BindSession("session-endpoint")
	}()

	waitForSend(t, stream1.sendStarted)

	setDone := make(chan struct{})
	go func() {
		client.SetEndpoint("endpoint-2")
		close(setDone)
	}()

	select {
	case <-setDone:
	case <-time.After(time.Second):
		t.Fatal("SetEndpoint blocked on in-flight bind send")
	}

	select {
	case err := <-firstErrCh:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("first bind did not finish after endpoint change")
	}

	secondErrCh := make(chan error, 1)
	go func() {
		secondErrCh <- client.BindSession("session-endpoint")
	}()

	waitForSend(t, stream2.sendStarted)
	stream2.sendRelease <- nil

	require.NoError(t, <-secondErrCh)
	require.Equal(t, "session-endpoint", client.boundSessionID)

	client.Close()
}

func TestPaymentSessionClient_SetEndpointAllowsNewGenerationWhileOldSendIsWedged(t *testing.T) {
	oldStream := newFakePaymentSessionStream()
	oldStream.ignoreClose = true
	newStream := newFakePaymentSessionStream()

	var client *paymentSessionClient
	client = newTestPaymentSessionClient(func(context.Context) paymentSessionStream {
		if client.providerEndpoint == "endpoint-2" {
			return newStream
		}
		return oldStream
	})

	firstErrCh := make(chan error, 1)
	go func() {
		firstErrCh <- client.BindSession("session-wedged")
	}()

	waitForSend(t, oldStream.sendStarted)

	client.mu.Lock()
	oldSendMu := client.sendMu
	client.mu.Unlock()

	setDone := make(chan struct{})
	go func() {
		client.SetEndpoint("endpoint-2")
		close(setDone)
	}()

	select {
	case <-setDone:
	case <-time.After(time.Second):
		t.Fatal("SetEndpoint blocked on a wedged old send")
	}

	secondErrCh := make(chan error, 1)
	go func() {
		secondErrCh <- client.BindSession("session-wedged")
	}()

	waitForSend(t, newStream.sendStarted)
	client.mu.Lock()
	newSendMu := client.sendMu
	client.mu.Unlock()
	require.NotNil(t, oldSendMu)
	require.NotNil(t, newSendMu)
	require.NotSame(t, oldSendMu, newSendMu)
	newStream.sendRelease <- nil

	select {
	case err := <-secondErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("replacement bind did not complete")
	}

	require.Equal(t, "session-wedged", client.boundSessionID)
	require.Empty(t, client.bindingSessionID)

	oldStream.sendRelease <- nil
	select {
	case err := <-firstErrCh:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("wedged old bind did not finish after release")
	}

	client.Close()
}
