package paymentlanemeta

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
)

const pageSize uint64 = 128

// Meta is the parent-derived lane metadata needed before the block executes.
// Once loaded, it is shared as read-only cache data.
type Meta struct {
	ratio  uint64
	listed map[common.Address]struct{}
}

func (m *Meta) Quota(gasLimit uint64) uint64 { return paymentlane.Quota(m.ratio, gasLimit) }

func (m *Meta) NewClassifier(isSystemTx paymentlane.SystemTxOracle, code paymentlane.CodeReader) *paymentlane.Classifier {
	return paymentlane.NewClassifier(isSystemTx, code, m.listed)
}
