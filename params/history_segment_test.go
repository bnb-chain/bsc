package params

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/common"
)

var (
	historySegmentsInTest = []HistorySegment{
		{
			Index:           0,
			ReGenesisNumber: 0,
			ReGenesisHash:   common.Hash{},
		},
		{
			Index:           1,
			ReGenesisNumber: BoundStartBlock,
			ReGenesisHash:   common.HexToHash("0xdb8a505f19ef04cb21ae79e3cb641963ffc44f3666e6fde499be55a72b6c7865"),
		},
		{
			Index:           2,
			ReGenesisNumber: BoundStartBlock + HistorySegmentLength,
			ReGenesisHash:   common.HexToHash("0xbf6d408bce0d531c41b00410e1c567e46b359db6e14d842cd8c8325039dff498"),
		},
	}
	testGenesis = common.HexToHash("0x50b168d3ba07cc77c13a5469b9a1aad8752ba725ff989b76bc7df89dc936e866")
)

func TestNewHistorySegmentManager_HardCode(t *testing.T) {
	tests := []struct {
		cfg *HistorySegmentConfig
	}{
		{
			cfg: &HistorySegmentConfig{
				CustomPath: "",
				Genesis:    NewHistoryBlock(0, BSCGenesisHash, 1),
			},
		},
		{
			cfg: &HistorySegmentConfig{
				CustomPath: "",
				Genesis:    NewHistoryBlock(0, ChapelGenesisHash, 1),
			},
		},
	}
	for i, item := range tests {
		_, err := NewHistorySegmentManager(item.cfg)
		assert.NoError(t, err, i)
	}
}

func TestHistorySegmentManager_Validate(t *testing.T) {
	tests := []struct {
		genesis  common.Hash
		segments []HistorySegment
		err      bool
	}{
		{
			genesis: testGenesis,
			segments: []HistorySegment{
				{
					Index:           1,
					ReGenesisNumber: 1,
					ReGenesisHash:   common.Hash{},
				},
			},
			err: true,
		},
		{
			genesis: testGenesis,
			segments: []HistorySegment{
				{
					Index:           0,
					ReGenesisNumber: 0,
					ReGenesisHash:   testGenesis,
				},
			},
		},
		{
			genesis: testGenesis,
			segments: []HistorySegment{
				{
					Index:           0,
					ReGenesisNumber: 0,
					ReGenesisHash:   testGenesis,
				},
				{
					Index:           1,
					ReGenesisNumber: 0,
					ReGenesisHash:   common.HexToHash("0xaa1b4e4d251289d21da95e66cf9b57f641b2dbc8031a2bb145ae58ee7ade03e7"),
				},
			},
			err: true,
		},
		{
			genesis: testGenesis,
			segments: []HistorySegment{
				{
					Index:           0,
					ReGenesisNumber: 0,
					ReGenesisHash:   testGenesis,
				},
				{
					Index:           0,
					ReGenesisNumber: 1,
					ReGenesisHash:   common.HexToHash("0xaa1b4e4d251289d21da95e66cf9b57f641b2dbc8031a2bb145ae58ee7ade03e7"),
				},
			},
			err: true,
		},
		{
			genesis: testGenesis,
			segments: []HistorySegment{
				{
					Index:           0,
					ReGenesisNumber: 0,
					ReGenesisHash:   testGenesis,
				},
				{
					Index:           1,
					ReGenesisNumber: 1,
					ReGenesisHash:   common.HexToHash("0xaa1b4e4d251289d21da95e66cf9b57f641b2dbc8031a2bb145ae58ee7ade03e7"),
				},
			},
		},
	}
	for i, item := range tests {
		err := ValidateHistorySegments(NewHistoryBlock(0, item.genesis, 0), item.segments)
		if item.err {
			assert.Error(t, err, i)
			continue
		}
		assert.NoError(t, err, i)
	}
}

func TestUnmarshalHistorySegments(t *testing.T) {
	enc, err := json.MarshalIndent(historySegmentsInTest, "", "    ")
	assert.NoError(t, err)
	//t.Log(string(enc))
	segments := unmarshalHistorySegments(string(enc))
	assert.Equal(t, historySegmentsInTest, segments)
}

func TestIndexSegment(t *testing.T) {
	segments := historySegmentsInTest
	hsm := HistorySegmentManager{
		segments: historySegmentsInTest,
	}
	assert.Equal(t, &segments[0], hsm.CurSegment(0))
	assert.Equal(t, &segments[0], hsm.CurSegment(BoundStartBlock-1))
	assert.Equal(t, &segments[1], hsm.CurSegment(BoundStartBlock))
	assert.Equal(t, &segments[1], hsm.CurSegment(BoundStartBlock+HistorySegmentLength-1))
	assert.Equal(t, &segments[2], hsm.CurSegment(BoundStartBlock+HistorySegmentLength))
	assert.Equal(t, &segments[2], hsm.CurSegment(BoundStartBlock+HistorySegmentLength*2))

	var (
		prev *HistorySegment
		ok   bool
	)
	_, ok = hsm.PrevSegment(&segments[0])
	assert.Equal(t, false, ok)
	prev, ok = hsm.PrevSegment(&segments[1])
	assert.Equal(t, true, ok)
	assert.Equal(t, &segments[0], prev)
	prev, ok = hsm.PrevSegment(&segments[2])
	assert.Equal(t, true, ok)
	assert.Equal(t, &segments[1], prev)
	_, ok = hsm.PrevSegment(&HistorySegment{
		Index: uint64(len(segments)),
	})
	assert.Equal(t, false, ok)
}
