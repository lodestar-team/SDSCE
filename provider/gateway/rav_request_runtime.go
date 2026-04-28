package gateway

import (
	"math/big"

	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
)

type pendingRAVRequest struct {
	usage           *commonv1.Usage
	currentRAVValue *big.Int
	targetValue     *big.Int
	baselineBlocks  uint64
	baselineBytes   uint64
	baselineReqs    uint64
	baselineCost    *big.Int
}

func newPendingRAVRequest(session *repository.Session, usage *commonv1.Usage) *pendingRAVRequest {
	if session == nil || session.CurrentRAV == nil || session.CurrentRAV.Message == nil || usage == nil || usage.Cost == nil {
		return nil
	}

	currentValue := cloneBigInt(session.CurrentRAV.Message.ValueAggregate)
	deltaCost := usage.Cost.ToBigInt()

	return &pendingRAVRequest{
		usage:           cloneUsage(usage),
		currentRAVValue: currentValue,
		targetValue:     new(big.Int).Add(currentValue, deltaCost),
		baselineBlocks:  session.BlocksProcessed,
		baselineBytes:   session.BytesTransferred,
		baselineReqs:    session.Requests,
		baselineCost:    cloneBigInt(session.TotalCost),
	}
}

func (r *pendingRAVRequest) matchesUsage(usage *commonv1.Usage) bool {
	if r == nil {
		return false
	}

	return usageEqual(r.usage, usage)
}

func (r *pendingRAVRequest) clone() *pendingRAVRequest {
	if r == nil {
		return nil
	}

	return &pendingRAVRequest{
		usage:           cloneUsage(r.usage),
		currentRAVValue: cloneBigInt(r.currentRAVValue),
		targetValue:     cloneBigInt(r.targetValue),
		baselineBlocks:  r.baselineBlocks,
		baselineBytes:   r.baselineBytes,
		baselineReqs:    r.baselineReqs,
		baselineCost:    cloneBigInt(r.baselineCost),
	}
}

func cloneUsage(usage *commonv1.Usage) *commonv1.Usage {
	if usage == nil {
		return nil
	}

	cloned := &commonv1.Usage{
		BlocksProcessed:  usage.BlocksProcessed,
		BytesTransferred: usage.BytesTransferred,
		Requests:         usage.Requests,
	}
	if usage.Cost != nil {
		cloned.Cost = commonv1.GRTFromBigInt(usage.Cost.ToBigInt())
	}

	return cloned
}

func usageEqual(a, b *commonv1.Usage) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	if a.BlocksProcessed != b.BlocksProcessed || a.BytesTransferred != b.BytesTransferred || a.Requests != b.Requests {
		return false
	}

	switch {
	case a.Cost == nil || b.Cost == nil:
		return a.Cost == nil && b.Cost == nil
	default:
		return a.Cost.ToBigInt().Cmp(b.Cost.ToBigInt()) == 0
	}
}

func cloneBigInt(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}

	return new(big.Int).Set(v)
}
