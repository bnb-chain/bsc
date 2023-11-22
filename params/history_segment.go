package params

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/common"
)

const (
	BoundStartBlock      uint64 = 31268530 // The starting block height of the first segment, was produced on Aug-29-2023
	HistorySegmentLength uint64 = 2592000  // Assume 1 block for every 3 second, 2,592,000 blocks will be produced in 90 days.
)

var (
	historySegmentsInBSCMainnet = []HisSegment{
		{
			Index: 0,
			StartAtBlock: HisBlockInfo{
				Number: 0,
				Hash:   BSCGenesisHash,
			},
		},
	}
	historySegmentsInBSCChapel = unmarshalHisSegments(`
[
    {
        "index": 0,
        "start_at_block": {
            "number": 0,
            "hash": "0x6d3c66c5357ec91d5c43af47e234a939b22557cbb552dc45bebbceeed90fbe34"
        },
        "finality_at_block": {
            "number": 0,
            "hash": "0x0000000000000000000000000000000000000000000000000000000000000000"
        }
    },
    {
        "index": 1,
        "start_at_block": {
            "number": 31268530,
            "hash": "0x2ab32e1541202ac43f3dc9ff80b998002ad9130ecc24c40a1f00a8e45dc1f786"
        },
        "finality_at_block": {
            "number": 31268532,
            "hash": "0x59203b593d2e4c213e65f68db2c19309380416a93592aa8f923d59aebc481c28"
        }
    },
    {
        "index": 2,
        "start_at_block": {
            "number": 33860530,
            "hash": "0x252e966e2420ecb2c5c51da62f147ac89004943e2b76c343bb1b2d8465f29a29"
        },
        "finality_at_block": {
            "number": 33860532,
            "hash": "0x424e526d901ae91897340655c81db7de16428a3322df4fa712693bda83572f8f"
        }
    }
]`)
	historySegmentsInBSCRialto = []HisSegment{
		{
			Index: 0,
			StartAtBlock: HisBlockInfo{
				Number: 0,
				Hash:   RialtoGenesisHash,
			},
		},
	}
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

func (h *HisSegment) String() string {
	return fmt.Sprintf("[Index: %v, StartAt: %v, FinalityAt: %v]", h.Index, h.StartAtBlock, h.FinalityAtBlock)
}

type HistorySegmentConfig struct {
	CustomPath string      // custom HistorySegments file path, need read from the file
	Genesis    common.Hash // specific chain genesis, it may use hard-code config
}

func (cfg *HistorySegmentConfig) LoadCustomSegments() ([]HisSegment, error) {
	if _, err := os.Stat(cfg.CustomPath); err != nil {
		return nil, err
	}
	enc, err := os.ReadFile(cfg.CustomPath)
	if err != nil {
		return nil, err
	}
	var ret []HisSegment
	if err = json.Unmarshal(enc, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

type HistorySegmentManager struct {
	segments []HisSegment
	cfg      *HistorySegmentConfig
}

func NewHistorySegmentManager(cfg *HistorySegmentConfig) (*HistorySegmentManager, error) {
	if cfg == nil {
		return nil, errors.New("cannot init HistorySegmentManager by nil config")
	}

	// if genesis is one of the hard code history segment, just ignore input custom file
	var (
		segments []HisSegment
		err      error
	)
	switch cfg.Genesis {
	case BSCGenesisHash:
		segments = historySegmentsInBSCMainnet
	case ChapelGenesisHash:
		segments = historySegmentsInBSCChapel
	case RialtoGenesisHash:
		segments = historySegmentsInBSCRialto
	default:
		segments, err = cfg.LoadCustomSegments()
		if err != nil {
			return nil, err
		}
	}
	if err = ValidateHisSegments(cfg.Genesis, segments); err != nil {
		return nil, err
	}
	return &HistorySegmentManager{
		segments: segments,
		cfg:      cfg,
	}, nil
}

func ValidateHisSegments(genesis common.Hash, segments []HisSegment) error {
	if len(segments) == 0 {
		return errors.New("history segment length cannot be 0")
	}
	expectSeg0 := HisSegment{
		Index: 0,
		StartAtBlock: HisBlockInfo{
			Number: 0,
			Hash:   genesis,
		},
	}
	if segments[0] != expectSeg0 {
		return fmt.Errorf("wrong segement0 start block, it must be genesis, expect: %v, actual: %v", expectSeg0, segments[0])
	}
	for i := 1; i < len(segments); i++ {
		if segments[i].Index != uint64(i) ||
			segments[i].StartAtBlock.Number <= segments[i-1].StartAtBlock.Number ||
			segments[i].StartAtBlock.Number+2 > segments[i].FinalityAtBlock.Number {
			return fmt.Errorf("wrong segement, index: %v, segment: %v", i, segments[i])
		}
	}

	return nil
}

// HisSegments return all history segments
func (m *HistorySegmentManager) HisSegments() []HisSegment {
	return m.segments
}

// CurSegment return which segment include this block
func (m *HistorySegmentManager) CurSegment(num uint64) HisSegment {
	segments := m.HisSegments()
	i := len(segments) - 1
	for i >= 0 {
		if segments[i].StartAtBlock.Number <= num {
			break
		}
		i--
	}
	return segments[i]
}

// PrevSegment return the current's last segment, because the latest 2 segments is available,
// so user could keep current & prev segment
func (m *HistorySegmentManager) PrevSegment(cur HisSegment) (HisSegment, bool) {
	segments := m.HisSegments()
	if cur.Index == 0 || cur.Index >= uint64(len(segments)) {
		return HisSegment{}, false
	}
	return segments[cur.Index-1], true
}

func unmarshalHisSegments(enc string) []HisSegment {
	var ret []HisSegment
	err := json.Unmarshal([]byte(enc), &ret)
	if err != nil {
		panic(err)
	}
	return ret
}
