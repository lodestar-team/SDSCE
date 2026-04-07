package sidecar

import (
	"fmt"
	"math/big"
	"time"

	"github.com/graphprotocol/substreams-data-service/horizon"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
)

func (s *Sidecar) signRAVForRequest(session *sidecarlib.Session, req *providerv1.RAVRequest) (*horizon.SignedRAV, error) {
	if req == nil {
		return nil, fmt.Errorf("rav_request is required")
	}
	if req.CurrentRav == nil {
		return nil, fmt.Errorf("rav_request.current_rav is required")
	}
	if req.Usage == nil {
		return nil, fmt.Errorf("rav_request.usage is required")
	}
	if req.Usage.Cost == nil {
		return nil, fmt.Errorf("rav_request.usage.cost is required")
	}

	current, err := sidecarlib.ProtoSignedRAVToHorizon(req.CurrentRav)
	if err != nil {
		return nil, fmt.Errorf("invalid rav_request.current_rav: %w", err)
	}
	if current == nil || current.Message == nil {
		return nil, fmt.Errorf("rav_request.current_rav.rav is required")
	}

	deltaCost := req.Usage.Cost.ToBigInt()
	nextValue := new(big.Int).Add(current.Message.ValueAggregate, deltaCost)

	return s.signRAV(
		current.Message.CollectionID,
		session.Payer,
		session.DataService,
		session.Receiver,
		uint64(time.Now().UnixNano()),
		nextValue,
		nil,
	)
}
