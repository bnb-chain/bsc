package params

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
)

const (
	BoundStartBlock      uint64 = 31268530 // The starting block height of the first segment, was produced on Aug-29-2023
	HistorySegmentLength uint64 = 2592000  // Assume 1 block for every 3 second, 2,592,000 blocks will be produced in 90 days.
)

var (
	historySegmentsInBSCMainnet = unmarshalHisSegments(`
{
	{
		index: 0,
		start_at_block: {
			number: 0,
			hash: 0x0000000000000000000000000000000000000000000000000000000000000000
		}
	}
}
`)
	historySegmentsInBSCChapel = unmarshalHisSegments(`
{
	{
		index: 0,
		start_at_block: {
			number: 0,
			hash: 0x0000000000000000000000000000000000000000000000000000000000000000
		}
	}
}
`)
	historySegmentsInBSCRialto []HisSegment
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

func HisSegments() []HisSegment {
	switch LocalGenesisHash {
	case BSCGenesisHash:
		return historySegmentsInBSCMainnet
	case ChapelGenesisHash:
		return historySegmentsInBSCChapel
	case RialtoGenesisHash:
		return historySegmentsInBSCRialto
	default:
		panic("sorry, this chain is not support history block segment, or init with wrong genesis hash")
	}
}

// CurrentSegment return which segment include this block
func CurrentSegment(num uint64) HisSegment {
	segments := HisSegments()
	i := len(segments) - 1
	for i >= 0 {
		if segments[i].StartAtBlock.Number <= num {
			break
		}
		i--
	}
	return segments[i]
}

// FindPrevSegment return the current's last segment, because the latest 2 segments is available,
// so user could keep current & prev segment
func FindPrevSegment(cur HisSegment) (HisSegment, bool) {
	segments := HisSegments()
	if cur.Index == 0 || cur.Index >= uint64(len(segments)) {
		return HisSegment{}, false
	}
	return segments[cur.Index-1], true
}

func unmarshalHisSegments(enc string) []HisSegment {
	var ret []HisSegment
	json.Unmarshal([]byte(enc), &ret)
	return ret
}
