package params

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/common"
)

func init() {
	LocalGenesisHash = RialtoGenesisHash
	historySegmentsInBSCRialto = []HisSegment{
		{
			Index: 0,
			StartAtBlock: HisBlockInfo{
				Number: 0,
				Hash:   common.Hash{},
			},
		},
		{
			Index: 1,
			StartAtBlock: HisBlockInfo{
				Number: BoundStartBlock,
				Hash:   common.HexToHash("0xdb8a505f19ef04cb21ae79e3cb641963ffc44f3666e6fde499be55a72b6c7865"),
			},
			FinalityAtBlock: HisBlockInfo{
				Number: 31268532,
				Hash:   common.HexToHash("0xaa1b4e4d251289d21da95e66cf9b57f641b2dbc8031a2bb145ae58ee7ade03e7"),
			},
		},
		{
			Index: 2,
			StartAtBlock: HisBlockInfo{
				Number: 33860530,
				Hash:   common.HexToHash("0xbf6d408bce0d531c41b00410e1c567e46b359db6e14d842cd8c8325039dff498"),
			},
			FinalityAtBlock: HisBlockInfo{
				Number: 33860532,
				Hash:   common.HexToHash("0xb22bf5eb6fe8ed39894d32b148fdedd91bd11497e7744e6c84c6b104aa577a15"),
			},
		},
	}
}

func TestUnmarshalHisSegments(t *testing.T) {
	enc, err := json.MarshalIndent(HisSegments(), "", "    ")
	assert.NoError(t, err)
	t.Log(string(enc))
	segments := unmarshalHisSegments(string(enc))
	assert.Equal(t, HisSegments(), segments)
}

func TestIndexSegment(t *testing.T) {
	segments := HisSegments()
	assert.Equal(t, segments[0], CurrentSegment(0))
	assert.Equal(t, segments[0], CurrentSegment(BoundStartBlock-1))
	assert.Equal(t, segments[1], CurrentSegment(BoundStartBlock))
	assert.Equal(t, segments[1], CurrentSegment(BoundStartBlock+HistorySegmentLength-1))
	assert.Equal(t, segments[2], CurrentSegment(BoundStartBlock+HistorySegmentLength))
	assert.Equal(t, segments[2], CurrentSegment(BoundStartBlock+HistorySegmentLength*2))

	prev, ok := FindPrevSegment(segments[0])
	assert.Equal(t, false, ok)
	prev, ok = FindPrevSegment(segments[1])
	assert.Equal(t, true, ok)
	assert.Equal(t, segments[0], prev)
	prev, ok = FindPrevSegment(segments[2])
	assert.Equal(t, true, ok)
	assert.Equal(t, segments[1], prev)
	_, ok = FindPrevSegment(HisSegment{
		Index: uint64(len(segments)),
	})
	assert.Equal(t, false, ok)
}
