package paymentlanemeta

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
)

const (
	pageSize           uint64 = 128
	maxListedContracts uint64 = 100_000
)

// Meta is the parent-derived lane metadata needed before the block executes.
// Once loaded, it is shared as read-only cache data.
type Meta struct {
	governanceParams paymentlane.GovernanceParams
	listed           map[common.Address]struct{}
}

func (m *Meta) GovernanceParams() paymentlane.GovernanceParams {
	return m.governanceParams
}

func (m *Meta) NewClassifier(code paymentlane.CodeReader) *paymentlane.Classifier {
	return paymentlane.NewClassifier(code, m.listed)
}
