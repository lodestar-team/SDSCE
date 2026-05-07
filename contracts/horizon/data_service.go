package horizoncontracts

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphprotocol/substreams-data-service/contracts/artifacts"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/streamingfast/eth-go"
)

const PaymentTypeQueryFee uint8 = 0

type DataService struct {
	abi abi.ABI
}

func NewDataService() (*DataService, error) {
	artifact, err := artifacts.Load("SubstreamsDataService")
	if err != nil {
		return nil, err
	}
	parsed, err := abi.JSON(strings.NewReader(string(artifact.ABI)))
	if err != nil {
		return nil, fmt.Errorf("parsing SubstreamsDataService ABI: %w", err)
	}
	return &DataService{abi: parsed}, nil
}

func MustNewDataService() *DataService {
	dataService, err := NewDataService()
	if err != nil {
		panic(err)
	}
	return dataService
}

func (d *DataService) PackCollect(indexer common.Address, paymentType uint8, data []byte) ([]byte, error) {
	return d.abi.Pack("collect", indexer, paymentType, data)
}

func (d *DataService) PackQueryFeeCollect(signedRAV *horizon.SignedRAV, dataServiceCutPPM uint64) ([]byte, error) {
	data, err := EncodeDataServiceCollectData(signedRAV, dataServiceCutPPM)
	if err != nil {
		return nil, err
	}
	return d.PackCollect(ethAddressToCommon(signedRAV.Message.ServiceProvider), PaymentTypeQueryFee, data)
}

var dataServiceCollectDataABI = mustParseDataServiceCollectDataABI()

func EncodeDataServiceCollectData(signedRAV *horizon.SignedRAV, dataServiceCutPPM uint64) ([]byte, error) {
	if signedRAV == nil {
		return nil, errors.New("signed RAV is required")
	}
	rav := signedRAV.Message
	if rav == nil {
		return nil, errors.New("signed RAV message is required")
	}
	if rav.ValueAggregate == nil {
		return nil, errors.New("signed RAV value aggregate is required")
	}

	arguments := dataServiceCollectDataABI.Methods["encode"].Inputs
	return arguments.Pack(
		dataServiceCollectSignedRAV{
			RAV: dataServiceCollectRAV{
				CollectionID:    rav.CollectionID,
				Payer:           ethAddressToCommon(rav.Payer),
				ServiceProvider: ethAddressToCommon(rav.ServiceProvider),
				DataService:     ethAddressToCommon(rav.DataService),
				TimestampNs:     rav.TimestampNs,
				ValueAggregate:  rav.ValueAggregate,
				Metadata:        rav.Metadata,
			},
			Signature: ravSignatureRSV(signedRAV.Signature),
		},
		new(big.Int).SetUint64(dataServiceCutPPM),
	)
}

type dataServiceCollectSignedRAV struct {
	RAV       dataServiceCollectRAV `abi:"rav"`
	Signature []byte                `abi:"signature"`
}

type dataServiceCollectRAV struct {
	CollectionID    horizon.CollectionID `abi:"collectionId"`
	Payer           common.Address       `abi:"payer"`
	ServiceProvider common.Address       `abi:"serviceProvider"`
	DataService     common.Address       `abi:"dataService"`
	TimestampNs     uint64               `abi:"timestampNs"`
	ValueAggregate  *big.Int             `abi:"valueAggregate"`
	Metadata        []byte               `abi:"metadata"`
}

func ravSignatureRSV(sig eth.Signature) []byte {
	rsv := make([]byte, 65)
	copy(rsv[0:32], sig[1:33])
	copy(rsv[32:64], sig[33:65])
	rsv[64] = sig[0]
	return rsv
}

func ethAddressToCommon(address eth.Address) common.Address {
	return common.BytesToAddress(address)
}

func mustParseDataServiceCollectDataABI() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(`[
		{
			"type": "function",
			"name": "encode",
			"inputs": [
				{
					"name": "signedRAV",
					"type": "tuple",
					"components": [
						{
							"name": "rav",
							"type": "tuple",
							"components": [
								{"name": "collectionId", "type": "bytes32"},
								{"name": "payer", "type": "address"},
								{"name": "serviceProvider", "type": "address"},
								{"name": "dataService", "type": "address"},
								{"name": "timestampNs", "type": "uint64"},
								{"name": "valueAggregate", "type": "uint128"},
								{"name": "metadata", "type": "bytes"}
							]
						},
						{"name": "signature", "type": "bytes"}
					]
				},
				{"name": "dataServiceCut", "type": "uint256"}
			]
		}
	]`))
	if err != nil {
		panic(fmt.Sprintf("parsing SubstreamsDataService collect data ABI: %v", err))
	}
	return parsed
}
