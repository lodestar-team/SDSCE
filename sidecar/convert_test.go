package sidecar

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtoRAVToHorizon(t *testing.T) {
	payer := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	dataService := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	serviceProvider := eth.MustNewAddress("0x3333333333333333333333333333333333333333")

	protoRAV := &commonv1.RAV{
		Payer:           commonv1.AddressFromEth(payer),
		DataService:     commonv1.AddressFromEth(dataService),
		ServiceProvider: commonv1.AddressFromEth(serviceProvider),
		TimestampNs:     1234567890,
		ValueAggregate:  commonv1.BigIntFromNative(big.NewInt(1000)),
		Metadata:        []byte("test-metadata"),
	}

	result, err := ProtoRAVToHorizon(protoRAV)
	require.NoError(t, err)

	assert.NotNil(t, result)
	assert.True(t, bytes.Equal(payer, result.Payer))
	assert.True(t, bytes.Equal(dataService, result.DataService))
	assert.True(t, bytes.Equal(serviceProvider, result.ServiceProvider))
	assert.Equal(t, uint64(1234567890), result.TimestampNs)
	assert.Equal(t, int64(1000), result.ValueAggregate.Int64())
}

func TestHorizonRAVToProto(t *testing.T) {
	payer := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	dataService := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	serviceProvider := eth.MustNewAddress("0x3333333333333333333333333333333333333333")

	horizonRAV := &horizon.RAV{
		Payer:           payer,
		DataService:     dataService,
		ServiceProvider: serviceProvider,
		TimestampNs:     1234567890,
		ValueAggregate:  big.NewInt(1000),
		Metadata:        []byte("test-metadata"),
	}

	result := HorizonRAVToProto(horizonRAV)

	assert.NotNil(t, result)
	gotPayer, err := result.Payer.ToEth()
	require.NoError(t, err)
	gotDataService, err := result.DataService.ToEth()
	require.NoError(t, err)
	gotServiceProvider, err := result.ServiceProvider.ToEth()
	require.NoError(t, err)

	assert.True(t, bytes.Equal(payer, gotPayer))
	assert.True(t, bytes.Equal(dataService, gotDataService))
	assert.True(t, bytes.Equal(serviceProvider, gotServiceProvider))
	assert.Equal(t, uint64(1234567890), result.TimestampNs)
	assert.Equal(t, big.NewInt(1000).Bytes(), result.ValueAggregate.Bytes)
}

func TestProtoRAVToHorizon_InvalidAddressLength(t *testing.T) {
	protoRAV := &commonv1.RAV{
		Payer:           &commonv1.Address{Bytes: make([]byte, 19)},
		DataService:     &commonv1.Address{Bytes: make([]byte, 20)},
		ServiceProvider: &commonv1.Address{Bytes: make([]byte, 20)},
		TimestampNs:     123,
		ValueAggregate:  commonv1.BigIntFromNative(big.NewInt(1)),
	}

	_, err := ProtoRAVToHorizon(protoRAV)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payer")
	assert.Contains(t, err.Error(), "want 20")
}

func TestProtoSignedRAVToHorizon_InvalidSignatureLength(t *testing.T) {
	protoSigned := &commonv1.SignedRAV{
		Rav: &commonv1.RAV{
			Payer:           &commonv1.Address{Bytes: make([]byte, 20)},
			DataService:     &commonv1.Address{Bytes: make([]byte, 20)},
			ServiceProvider: &commonv1.Address{Bytes: make([]byte, 20)},
			TimestampNs:     123,
			ValueAggregate:  commonv1.BigIntFromNative(big.NewInt(1)),
		},
		Signature: []byte{0x01, 0x02},
	}

	_, err := ProtoSignedRAVToHorizon(protoSigned)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature")
	assert.Contains(t, err.Error(), "65")
}

func TestAddressesEqual(t *testing.T) {
	addr1 := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	addr2 := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	addr3 := eth.MustNewAddress("0x2222222222222222222222222222222222222222")

	assert.True(t, AddressesEqual(addr1, addr2))
	assert.False(t, AddressesEqual(addr1, addr3))
}
