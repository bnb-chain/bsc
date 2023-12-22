package params

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

type HistoryBlock struct {
	Number uint64
	hash   common.Hash
	TD     uint64
}

func NewHistoryBlock(num uint64, hash common.Hash, td uint64) HistoryBlock {
	return HistoryBlock{
		Number: num,
		hash:   hash,
		TD:     td,
	}
}

type HistorySegment struct {
	Index           uint64      `json:"index"`                    // segment index number
	ReGenesisNumber uint64      `json:"re_genesis_number"`        // new history segment start at a finality block number, called ReGenesisNumber
	ReGenesisHash   common.Hash `json:"re_genesis_hash"`          // new history segment start at a finality block hash, called ReGenesisHash
	TD              uint64      `json:"td"`                       // the ReGenesisBlock's TD
	ConsensusData   string      `json:"consensus_data,omitempty"` // the ReGenesisBlock's consensus data
}

func (s *HistorySegment) String() string {
	return fmt.Sprintf("{Index: %v, ReGenesisNumber: %v, ReGenesisHash: %v, TD: %v}", s.Index, s.ReGenesisNumber, s.ReGenesisHash, s.TD)
}

func (s *HistorySegment) MatchBlock(h common.Hash, n uint64) bool {
	if s.ReGenesisNumber == n && s.ReGenesisHash == h {
		return true
	}
	return false
}

func (s *HistorySegment) Equals(compared *HistorySegment) bool {
	if s == nil || compared == nil {
		return s == compared
	}
	if s.Index != compared.Index {
		return false
	}
	if !s.MatchBlock(compared.ReGenesisHash, compared.ReGenesisNumber) {
		return false
	}
	if s.TD != compared.TD {
		return false
	}
	if !strings.EqualFold(s.ConsensusData, compared.ConsensusData) {
		return false
	}
	return true
}

type HistorySegmentConfig struct {
	CustomPath string       // custom HistorySegments file path, need read from the file
	Genesis    HistoryBlock // specific chain genesis, it may use hard-code config
}

func (cfg *HistorySegmentConfig) LoadCustomSegments() ([]HistorySegment, error) {
	if _, err := os.Stat(cfg.CustomPath); err != nil {
		return nil, err
	}
	enc, err := os.ReadFile(cfg.CustomPath)
	if err != nil {
		return nil, err
	}
	var ret []HistorySegment
	if err = json.Unmarshal(enc, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

type HistorySegmentManager struct {
	segments []HistorySegment
	cfg      *HistorySegmentConfig
}

func NewHistorySegmentManager(cfg *HistorySegmentConfig) (*HistorySegmentManager, error) {
	if cfg == nil {
		return nil, errors.New("cannot init HistorySegmentManager by nil config")
	}

	// if genesis is one of the hard code history segment, just ignore input custom file
	var (
		segments []HistorySegment
		err      error
	)
	switch cfg.Genesis.hash {
	case BSCGenesisHash:
		segments = historySegmentsInBSCMainnet
	case ChapelGenesisHash:
		segments = historySegmentsInBSCChapel
	}

	// try load from config files
	if len(segments) == 0 {
		segments, err = cfg.LoadCustomSegments()
		if err != nil {
			return nil, fmt.Errorf("LoadCustomSegments err %v", err)
		}
	}
	if err = ValidateHistorySegments(cfg.Genesis, segments); err != nil {
		return nil, err
	}
	return &HistorySegmentManager{
		segments: segments,
		cfg:      cfg,
	}, nil
}

func NewHistorySegmentManagerWithSegments(genesis HistoryBlock, segments []HistorySegment) (*HistorySegmentManager, error) {
	if err := ValidateHistorySegments(genesis, segments); err != nil {
		return nil, err
	}
	return &HistorySegmentManager{
		segments: segments,
		cfg: &HistorySegmentConfig{
			Genesis: genesis,
		},
	}, nil
}

func ValidateHistorySegments(genesis HistoryBlock, segments []HistorySegment) error {
	if len(segments) == 0 {
		return errors.New("history segment length cannot be 0")
	}
	expectSeg0 := HistorySegment{
		Index:           0,
		ReGenesisNumber: genesis.Number,
		ReGenesisHash:   genesis.hash,
		TD:              genesis.TD,
	}
	if !segments[0].Equals(&expectSeg0) {
		return fmt.Errorf("wrong segement0 start block, it must be genesis, expect: %v, actual: %v", expectSeg0, segments[0])
	}
	for i := 1; i < len(segments); i++ {
		if segments[i].Index != uint64(i) ||
			segments[i].ReGenesisNumber <= segments[i-1].ReGenesisNumber {
			return fmt.Errorf("wrong segement, index: %v, segment: %v", i, segments[i])
		}
	}

	return nil
}

// HistorySegments return all history segments
func (m *HistorySegmentManager) HistorySegments() []HistorySegment {
	return m.segments
}

// CurSegment return which segment include this block
func (m *HistorySegmentManager) CurSegment(num uint64) *HistorySegment {
	segments := m.HistorySegments()
	i := len(segments) - 1
	for i >= 0 {
		if segments[i].ReGenesisNumber <= num {
			break
		}
		i--
	}
	return &segments[i]
}

// PrevSegment return the current's last segment, because the latest 2 segments is available,
// so user could keep current & prev segment
func (m *HistorySegmentManager) PrevSegment(cur *HistorySegment) (*HistorySegment, bool) {
	if cur == nil {
		return nil, false
	}
	segments := m.HistorySegments()
	if cur.Index == 0 || cur.Index >= uint64(len(segments)) {
		return nil, false
	}
	return &segments[cur.Index-1], true
}

// PrevSegmentByNumber return the current's last segment
func (m *HistorySegmentManager) PrevSegmentByNumber(num uint64) (*HistorySegment, bool) {
	cur := m.CurSegment(num)
	return m.PrevSegment(cur)
}

func unmarshalHistorySegments(enc string) []HistorySegment {
	var ret []HistorySegment
	err := json.Unmarshal([]byte(enc), &ret)
	if err != nil {
		panic(err)
	}
	return ret
}
