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
	ratio  uint64
	listed map[common.Address]struct{}
}

// Ratio is BEP-703 section 3.6.1's value, already past its guard.
func (m *Meta) Ratio() uint64 { return m.ratio }

// Quota is this block's reservation, section 3.4.1 over the block's own gas limit.
func (m *Meta) Quota(gasLimit uint64) uint64 { return paymentlane.Quota(m.ratio, gasLimit) }

func (m *Meta) NewClassifier(code paymentlane.CodeReader) *paymentlane.Classifier {
	return paymentlane.NewClassifier(code, m.listed)
}
