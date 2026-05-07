package main

import (
	"bytes"
	"io"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/require"
)

func TestParseProviderOperatorEndpoint(t *testing.T) {
	parsed, err := parseProviderOperatorEndpoint("operator.example:9443", false)
	require.NoError(t, err)
	require.Equal(t, "https://operator.example:9443", parsed.URL)
	require.False(t, parsed.Plaintext)

	parsed, err = parseProviderOperatorEndpoint("localhost:9005", true)
	require.NoError(t, err)
	require.Equal(t, "http://localhost:9005", parsed.URL)
	require.True(t, parsed.Plaintext)

	_, err = parseProviderOperatorEndpoint("http://localhost:9005", false)
	require.ErrorContains(t, err, "http endpoint requires explicit --plaintext")

	_, err = parseProviderOperatorEndpoint("https://localhost:9005", true)
	require.ErrorContains(t, err, "--plaintext requires an http endpoint")
}

func TestResolveProviderOperatorToken(t *testing.T) {
	token, err := resolveProviderOperatorToken("direct-token", "", nil)
	require.NoError(t, err)
	require.Equal(t, "direct-token", token)

	token, err = resolveProviderOperatorToken("", "SDS_OPERATOR_TOKEN", providerOperatorMapLookupEnv(map[string]string{
		"SDS_OPERATOR_TOKEN": "env-token",
	}))
	require.NoError(t, err)
	require.Equal(t, "env-token", token)

	_, err = resolveProviderOperatorToken("", "", nil)
	require.ErrorContains(t, err, "exactly one")

	_, err = resolveProviderOperatorToken("direct-token", "SDS_OPERATOR_TOKEN", providerOperatorMapLookupEnv(map[string]string{
		"SDS_OPERATOR_TOKEN": "env-token",
	}))
	require.ErrorContains(t, err, "exactly one")

	_, err = resolveProviderOperatorToken("bad token", "", nil)
	require.ErrorContains(t, err, "contains whitespace")

	_, err = resolveProviderOperatorToken("", "SDS_OPERATOR_TOKEN", providerOperatorMapLookupEnv(map[string]string{
		"SDS_OPERATOR_TOKEN": "bad token",
	}))
	require.ErrorContains(t, err, "contains whitespace")
}

func providerOperatorMapLookupEnv(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func TestProviderOperatorTextFormatting(t *testing.T) {
	var out strings.Builder
	withStdout(t, &out, func() {
		printAcceptedRAV(&providerv1.AcceptedRAV{
			SessionId:       "session-1",
			CollectionId:    []byte{1, 2, 3},
			ValueAggregate:  commonv1.GRTFromBigInt(mustBigInt("1000000000000000000")),
			CollectionState: providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE,
		}, false)
	})

	require.Contains(t, out.String(), "session_id: session-1")
	require.Contains(t, out.String(), "collection_id: 0x010203")
	require.Contains(t, out.String(), "value_aggregate: 1 GRT")
	require.NotContains(t, out.String(), "signed_rav_base64")
}

func TestProviderOperatorSessionPaymentStateFormatting(t *testing.T) {
	var out strings.Builder
	withStdout(t, &out, func() {
		printOperatorSession(&providerv1.OperatorSession{
			SessionId:             "session-low-funds",
			Status:                providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_TERMINATED,
			EndReason:             commonv1.EndReason_END_REASON_PAYMENT_ISSUE,
			PaymentControlPending: false,
			PaymentState: &providerv1.OperatorPaymentState{
				PaymentStatus: &commonv1.PaymentStatus{
					CurrentRavValue:       commonv1.GRTFromBigInt(big.NewInt(1000)),
					AccumulatedUsageValue: commonv1.GRTFromBigInt(big.NewInt(1500)),
					EscrowBalance:         commonv1.GRTFromBigInt(big.NewInt(900)),
					FundsSufficient:       false,
				},
				FundsStatus:          providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_INSUFFICIENT,
				CurrentOutstanding:   commonv1.GRTFromBigInt(big.NewInt(1000)),
				ProjectedOutstanding: commonv1.GRTFromBigInt(big.NewInt(1500)),
				MinimumNeeded:        commonv1.GRTFromBigInt(big.NewInt(600)),
				OperatorHint:         "top up escrow before starting a replacement session",
			},
		})
	})

	require.Contains(t, out.String(), "funds_status: insufficient")
	require.Contains(t, out.String(), "funds_sufficient: false")
	require.Contains(t, out.String(), "minimum_needed: 0.0000000000000006 GRT")
	require.Contains(t, out.String(), "operator_hint: top up escrow")
}

func TestProviderOperatorRedactsSignedRAVPayloads(t *testing.T) {
	rav := &providerv1.AcceptedRAV{SignedRav: &commonv1.SignedRAV{}}
	redactAcceptedRAVPayload(rav, false)
	require.Nil(t, rav.GetSignedRav())

	rav = &providerv1.AcceptedRAV{SignedRav: &commonv1.SignedRAV{}}
	redactAcceptedRAVPayload(rav, true)
	require.NotNil(t, rav.GetSignedRav())

	record := &providerv1.CollectionRecord{SignedRav: &commonv1.SignedRAV{}}
	redactCollectionRAVPayload(record, false)
	require.Nil(t, record.GetSignedRav())

	record = &providerv1.CollectionRecord{SignedRav: &commonv1.SignedRAV{}}
	redactCollectionRAVPayload(record, true)
	require.NotNil(t, record.GetSignedRav())
}

func TestValidateProviderCollectRecord(t *testing.T) {
	record := providerCollectTestRecord(providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE)
	signedRAV, err := validateProviderCollectRecord(
		record,
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	)
	require.NoError(t, err)
	require.Equal(t, record.GetKey().GetCollectionId(), signedRAV.Message.CollectionID[:])

	_, err = validateProviderCollectRecord(
		providerCollectTestRecord(providerv1.CollectionState_COLLECTION_STATE_COLLECT_PENDING),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	)
	require.ErrorContains(t, err, "expected collectible or collect_failed_retryable")

	_, err = validateProviderCollectRecord(
		record,
		common.HexToAddress("0x4444444444444444444444444444444444444444"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	)
	require.ErrorContains(t, err, "does not match RAV service provider")
}

func providerCollectTestRecord(state providerv1.CollectionState) *providerv1.CollectionRecord {
	collectionID := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Bytes()
	payer := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	serviceProvider := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	dataService := eth.MustNewAddress("0x3333333333333333333333333333333333333333")

	signature := make([]byte, 65)
	for i := range signature {
		signature[i] = byte(i + 1)
	}

	return &providerv1.CollectionRecord{
		Key: &providerv1.CollectionKey{
			SessionId:       "session-1",
			CollectionId:    collectionID,
			Payer:           commonv1.AddressFromEth(payer),
			ServiceProvider: commonv1.AddressFromEth(serviceProvider),
			DataService:     commonv1.AddressFromEth(dataService),
		},
		State: state,
		SignedRav: &commonv1.SignedRAV{
			Rav: &commonv1.RAV{
				CollectionId:    collectionID,
				Payer:           commonv1.AddressFromEth(payer),
				ServiceProvider: commonv1.AddressFromEth(serviceProvider),
				DataService:     commonv1.AddressFromEth(dataService),
				TimestampNs:     123,
				ValueAggregate:  commonv1.GRTFromBigInt(big.NewInt(1000)),
				Metadata:        []byte("metadata"),
			},
			Signature: signature,
		},
		ValueAggregate: commonv1.GRTFromBigInt(big.NewInt(1000)),
	}
}

func withStdout(t *testing.T, out *strings.Builder, fn func()) {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer

	fn()

	require.NoError(t, writer.Close())
	os.Stdout = original

	var buffer bytes.Buffer
	_, err = io.Copy(&buffer, reader)
	require.NoError(t, err)
	out.WriteString(buffer.String())
}

func mustBigInt(raw string) *big.Int {
	out, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		panic(raw)
	}
	return out
}
