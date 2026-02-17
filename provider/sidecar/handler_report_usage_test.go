package sidecar

import (
	"context"
	"math/big"
	"testing"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestReportUsage_ComputesCostAndOverridesMismatch(t *testing.T) {
	pricingConfig := &sidecarlib.PricingConfig{
		PricePerBlock: sidecarlib.NewPriceFromWei(big.NewInt(10)),
		PricePerByte:  sidecarlib.NewPriceFromWei(big.NewInt(2)),
	}

	s := New(&Config{
		ListenAddr:      ":0",
		ServiceProvider: eth.MustNewAddress("0x2222222222222222222222222222222222222222"),
		Domain:          horizon.NewDomain(1337, eth.MustNewAddress("0x1234567890123456789012345678901234567890")),
		PricingConfig:   pricingConfig,
	}, zap.NewNop())

	session := s.sessions.Create(
		eth.MustNewAddress("0x1111111111111111111111111111111111111111"),
		s.serviceProvider,
		eth.MustNewAddress("0x3333333333333333333333333333333333333333"),
	)
	session.SetPricingConfig(pricingConfig)

	// 3 blocks * 10 + 4 bytes * 2 = 38 wei
	expected := big.NewInt(0).Add(
		big.NewInt(0).Mul(big.NewInt(10), big.NewInt(3)),
		big.NewInt(0).Mul(big.NewInt(2), big.NewInt(4)),
	)

	_, err := s.ReportUsage(context.Background(), connect.NewRequest(&providerv1.ReportUsageRequest{
		SessionId: session.ID,
		Usage: &commonv1.Usage{
			BlocksProcessed:  3,
			BytesTransferred: 4,
			Requests:         1,
			Cost:             commonv1.BigIntFromNative(big.NewInt(1)), // intentionally wrong
		},
	}))
	require.NoError(t, err)
	require.Equal(t, expected.String(), session.TotalCost.String())

	// Cost can be omitted; provider computes it.
	_, err = s.ReportUsage(context.Background(), connect.NewRequest(&providerv1.ReportUsageRequest{
		SessionId: session.ID,
		Usage: &commonv1.Usage{
			BlocksProcessed:  1,
			BytesTransferred: 0,
			Requests:         1,
			Cost:             nil,
		},
	}))
	require.NoError(t, err)
	require.Equal(t, big.NewInt(0).Add(expected, big.NewInt(10)).String(), session.TotalCost.String())
}
