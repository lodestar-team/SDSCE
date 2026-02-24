package sidecar

import (
	"bytes"
	"fmt"

	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/streamingfast/eth-go"
)

// ProtoRAVToHorizon converts a proto RAV to a horizon RAV
func ProtoRAVToHorizon(pr *commonv1.RAV) (*horizon.RAV, error) {
	if pr == nil {
		return nil, nil
	}

	if len(pr.CollectionId) == 0 {
		return nil, fmt.Errorf("collection_id is required")
	}
	if len(pr.CollectionId) != 32 {
		return nil, fmt.Errorf("collection_id: want 32 bytes, got %d", len(pr.CollectionId))
	}
	var collectionID horizon.CollectionID
	copy(collectionID[:], pr.CollectionId)

	payer, err := pr.Payer.ToEth()
	if err != nil {
		return nil, fmt.Errorf("payer: %w", err)
	}
	dataService, err := pr.DataService.ToEth()
	if err != nil {
		return nil, fmt.Errorf("data_service: %w", err)
	}
	serviceProvider, err := pr.ServiceProvider.ToEth()
	if err != nil {
		return nil, fmt.Errorf("service_provider: %w", err)
	}
	if pr.ValueAggregate == nil {
		return nil, fmt.Errorf("value_aggregate is required")
	}

	return &horizon.RAV{
		CollectionID:    collectionID,
		Payer:           payer,
		DataService:     dataService,
		ServiceProvider: serviceProvider,
		TimestampNs:     pr.TimestampNs,
		ValueAggregate:  pr.ValueAggregate.ToBigInt(),
		Metadata:        pr.Metadata,
	}, nil
}

// HorizonRAVToProto converts a horizon RAV to a proto RAV
func HorizonRAVToProto(hr *horizon.RAV) *commonv1.RAV {
	if hr == nil {
		return nil
	}

	return &commonv1.RAV{
		CollectionId:    hr.CollectionID[:],
		Payer:           commonv1.AddressFromEth(hr.Payer),
		DataService:     commonv1.AddressFromEth(hr.DataService),
		ServiceProvider: commonv1.AddressFromEth(hr.ServiceProvider),
		TimestampNs:     hr.TimestampNs,
		ValueAggregate:  commonv1.GRTFromBigInt(hr.ValueAggregate),
		Metadata:        hr.Metadata,
	}
}

// ProtoSignedRAVToHorizon converts a proto SignedRAV to a horizon SignedRAV
func ProtoSignedRAVToHorizon(psr *commonv1.SignedRAV) (*horizon.SignedRAV, error) {
	if psr == nil {
		return nil, nil
	}

	rav, err := ProtoRAVToHorizon(psr.Rav)
	if err != nil {
		return nil, err
	}
	if rav == nil {
		return nil, fmt.Errorf("rav is required")
	}

	sig, err := eth.NewSignatureFromBytes(psr.Signature)
	if err != nil {
		return nil, fmt.Errorf("signature: %w", err)
	}

	return &horizon.SignedRAV{
		Message:   rav,
		Signature: sig,
	}, nil
}

// HorizonSignedRAVToProto converts a horizon SignedRAV to a proto SignedRAV
func HorizonSignedRAVToProto(hsr *horizon.SignedRAV) *commonv1.SignedRAV {
	if hsr == nil {
		return nil
	}

	return &commonv1.SignedRAV{
		Rav:       HorizonRAVToProto(hsr.Message),
		Signature: hsr.Signature[:],
	}
}

// AddressesEqual compares two eth.Address values
func AddressesEqual(a, b eth.Address) bool {
	return bytes.Equal(a, b)
}
