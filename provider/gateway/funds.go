package gateway

import (
	"context"
	"math/big"

	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/sidecar"
)

const (
	fundsStatusOK           = "ok"
	fundsStatusInsufficient = "insufficient"
	fundsStatusUnknown      = "unknown"

	fundsStatusKey                  = "funds_status"
	fundsEscrowBalanceWeiKey        = "funds_escrow_balance_wei"
	fundsCurrentOutstandingWeiKey   = "funds_current_outstanding_wei"
	fundsProjectedOutstandingWeiKey = "funds_projected_outstanding_wei"
	fundsMinimumNeededWeiKey        = "funds_minimum_needed_wei"
	fundsCheckErrorKey              = "funds_check_error"
)

type fundsAssessment struct {
	status               string
	currentOutstanding   *big.Int
	projectedOutstanding *big.Int
	escrowBalance        *big.Int
	minimumNeeded        *big.Int
	checkErr             error
}

func (a *fundsAssessment) insufficient() bool {
	return a.status == fundsStatusInsufficient
}

func (a *fundsAssessment) unknown() bool {
	return a.status == fundsStatusUnknown
}

func (s *Gateway) assessSessionFunds(ctx context.Context, session *repository.Session) *fundsAssessment {
	currentOutstanding := big.NewInt(0)
	if session.CurrentRAV != nil && session.CurrentRAV.Message != nil && session.CurrentRAV.Message.ValueAggregate != nil {
		currentOutstanding = new(big.Int).Set(session.CurrentRAV.Message.ValueAggregate)
	}

	_, _, _, deltaCost := session.UsageDeltaSinceBaseline()
	projectedOutstanding := new(big.Int).Add(new(big.Int).Set(currentOutstanding), deltaCost)

	assessment := &fundsAssessment{
		status:               fundsStatusUnknown,
		currentOutstanding:   currentOutstanding,
		projectedOutstanding: projectedOutstanding,
		minimumNeeded:        big.NewInt(0),
	}

	escrowBalance, err := s.GetEscrowBalance(ctx, session.Payer)
	if err != nil {
		assessment.checkErr = err
		return assessment
	}
	if escrowBalance == nil {
		return assessment
	}

	assessment.escrowBalance = new(big.Int).Set(escrowBalance)
	if projectedOutstanding.Cmp(escrowBalance) > 0 {
		assessment.status = fundsStatusInsufficient
		assessment.minimumNeeded = new(big.Int).Sub(projectedOutstanding, escrowBalance)
		return assessment
	}

	assessment.status = fundsStatusOK
	return assessment
}

func applyFundsAssessmentMetadata(session *repository.Session, assessment *fundsAssessment) {
	if session.Metadata == nil {
		session.Metadata = make(map[string]string)
	}

	session.Metadata[fundsStatusKey] = assessment.status
	session.Metadata[fundsCurrentOutstandingWeiKey] = assessment.currentOutstanding.String()
	session.Metadata[fundsProjectedOutstandingWeiKey] = assessment.projectedOutstanding.String()

	if assessment.escrowBalance != nil {
		session.Metadata[fundsEscrowBalanceWeiKey] = assessment.escrowBalance.String()
		session.Metadata[fundsMinimumNeededWeiKey] = assessment.minimumNeeded.String()
	} else {
		delete(session.Metadata, fundsEscrowBalanceWeiKey)
		delete(session.Metadata, fundsMinimumNeededWeiKey)
	}

	if assessment.checkErr != nil {
		session.Metadata[fundsCheckErrorKey] = assessment.checkErr.Error()
	} else {
		delete(session.Metadata, fundsCheckErrorKey)
	}
}

func needMoreFundsResponse(session *repository.Session, assessment *fundsAssessment) *providerv1.PaymentSessionResponse {
	outstandingRAVs := make([]*commonv1.SignedRAV, 0, 1)
	if session.CurrentRAV != nil {
		outstandingRAVs = append(outstandingRAVs, sidecar.HorizonSignedRAVToProto(session.CurrentRAV))
	}

	return &providerv1.PaymentSessionResponse{
		Message: &providerv1.PaymentSessionResponse_NeedMoreFunds{
			NeedMoreFunds: &providerv1.NeedMoreFunds{
				OutstandingRavs:  outstandingRAVs,
				TotalOutstanding: commonv1.GRTFromBigInt(assessment.currentOutstanding),
				EscrowBalance:    commonv1.GRTFromBigInt(assessment.escrowBalance),
				MinimumNeeded:    commonv1.GRTFromBigInt(assessment.minimumNeeded),
			},
		},
	}
}
