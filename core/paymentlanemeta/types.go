package paymentlanemeta

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
)

const pageSize uint64 = 128

// Meta is the parent-derived lane metadata needed before the block executes.
// Once loaded, it is shared as read-only cache data.
type Meta struct {
	params paymentlane.Params
	listed map[common.Address]struct{}
}

func (m *Meta) Params() paymentlane.Params {
	return m.params
}

func (m *Meta) NewClassifier(code paymentlane.CodeReader) *paymentlane.Classifier {
	return paymentlane.NewClassifier(code, m.listed)
}
