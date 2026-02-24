package sidecar

import (
	"context"
	"math/big"
	"testing"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
	"github.com/streamingfast/eth-go"
	"go.uber.org/zap"
)

func TestReportUsage_MissingUsage(t *testing.T) {
	key, err := eth.NewRandomPrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	domain := horizon.NewDomain(1337, eth.MustNewAddress("0x1234567890123456789012345678901234567890"))
	s := New(&Config{
		ListenAddr: ":0",
		SignerKey:  key,
		Domain:     domain,
	}, zap.NewNop())

	session := s.sessions.Create(
		eth.MustNewAddress("0x1111111111111111111111111111111111111111"),
		eth.MustNewAddress("0x2222222222222222222222222222222222222222"),
		eth.MustNewAddress("0x3333333333333333333333333333333333333333"),
	)

	_, gotErr := s.ReportUsage(context.Background(), connect.NewRequest(&consumerv1.ReportUsageRequest{
		SessionId: session.ID,
		Usage:     nil,
	}))
	if gotErr == nil {
		t.Fatalf("expected error")
	}
	if connect.CodeOf(gotErr) != connect.CodeInvalidArgument {
		t.Fatalf("expected %v, got %v: %v", connect.CodeInvalidArgument, connect.CodeOf(gotErr), gotErr)
	}
}

func TestReportUsage_InactiveSession(t *testing.T) {
	key, err := eth.NewRandomPrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	domain := horizon.NewDomain(1337, eth.MustNewAddress("0x1234567890123456789012345678901234567890"))
	s := New(&Config{
		ListenAddr: ":0",
		SignerKey:  key,
		Domain:     domain,
	}, zap.NewNop())

	session := s.sessions.Create(
		eth.MustNewAddress("0x1111111111111111111111111111111111111111"),
		eth.MustNewAddress("0x2222222222222222222222222222222222222222"),
		eth.MustNewAddress("0x3333333333333333333333333333333333333333"),
	)
	session.End(commonv1.EndReason_END_REASON_COMPLETE)

	_, gotErr := s.ReportUsage(context.Background(), connect.NewRequest(&consumerv1.ReportUsageRequest{
		SessionId: session.ID,
		Usage: &commonv1.Usage{
			BlocksProcessed:  1,
			BytesTransferred: 2,
			Requests:         1,
			Cost:             commonv1.GRTFromBigInt(big.NewInt(1)),
		},
	}))
	if gotErr == nil {
		t.Fatalf("expected error")
	}
	if connect.CodeOf(gotErr) != connect.CodeFailedPrecondition {
		t.Fatalf("expected %v, got %v: %v", connect.CodeFailedPrecondition, connect.CodeOf(gotErr), gotErr)
	}
}
