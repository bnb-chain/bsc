package types

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type BidBlockPermissionStatus struct {
	Allowed   bool
	Reason    string // err detail for auto revokes (InsertChain failure), or "manual" for admin revokes
	BlockHash common.Hash
	BlockNum  uint64
	RevokedAt time.Time
	ResetAt   time.Time
}
