package params

import "github.com/ethereum/go-ethereum/common"

const (
	BoundStartBlock      uint64 = 31268530 // The starting block height of the first segment, was produced on Aug-29-2023
	HistorySegmentLength uint64 = 2592000  // Assume 1 block for every 3 second, 2,592,000 blocks will be produced in 90 days.
)

type HisBlockInfo struct {
	Number uint64      `json:"number"`
	Hash   common.Hash `json:"hash"`
}

type HisSegment struct {
	Index           uint64       `json:"index"`             // segment index number
	StartAtBlock    HisBlockInfo `json:"start_at_block"`    // target segment start from here
	FinalityAtBlock HisBlockInfo `json:"finality_at_block"` // the StartAtBlock finality's block
	// TODO(0xbundler): if need add more finality evidence? like signature?
}
