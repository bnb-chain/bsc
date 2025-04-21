package bsc

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

// mockBackend implements the Backend interface for testing
type mockBackend struct {
	chain *core.BlockChain
}

func (b *mockBackend) Chain() *core.BlockChain {
	return b.chain
}

func (b *mockBackend) RunPeer(peer *Peer, handler Handler) error {
	return nil
}

func (b *mockBackend) PeerInfo(id enode.ID) interface{} {
	return nil
}

func (b *mockBackend) Handle(peer *Peer, packet Packet) error {
	return nil
}

// mockMsg implements the Decoder interface for testing
type mockMsg struct {
	code uint64
	data interface{}
}

func (m *mockMsg) Decode(val interface{}) error {
	// Simple implementation for testing
	switch v := val.(type) {
	case *GetBlocksByRangePacket:
		*v = *m.data.(*GetBlocksByRangePacket)
	case *BlocksByRangePacket:
		*v = *m.data.(*BlocksByRangePacket)
	}
	return nil
}

func TestHandleGetBlocksByRange(t *testing.T) {
	// Setup test environment
	backend := &mockBackend{
		chain: &core.BlockChain{}, // You might want to use a more sophisticated mock
	}
	peer := &Peer{}

	// Test cases
	tests := []struct {
		name    string
		msg     *mockMsg
		wantErr bool
	}{
		{
			name: "Valid request with block hash",
			msg: &mockMsg{
				code: GetBlocksByRangeMsg,
				data: &GetBlocksByRangePacket{
					RequestId:        1,
					StartBlockHash:   common.HexToHash("0x123"),
					StartBlockHeight: 100,
					Count:            5,
				},
			},
			wantErr: false,
		},
		{
			name: "Valid request with block height",
			msg: &mockMsg{
				code: GetBlocksByRangeMsg,
				data: &GetBlocksByRangePacket{
					RequestId:        2,
					StartBlockHeight: 100,
					Count:            5,
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid count",
			msg: &mockMsg{
				code: GetBlocksByRangeMsg,
				data: &GetBlocksByRangePacket{
					RequestId:        3,
					StartBlockHeight: 100,
					Count:            0,
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid request ID",
			msg: &mockMsg{
				code: GetBlocksByRangeMsg,
				data: &GetBlocksByRangePacket{
					RequestId:        0,
					StartBlockHeight: 100,
					Count:            5,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handleGetBlocksByRange(backend, tt.msg, peer)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleGetBlocksByRange() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandleBlocksByRange(t *testing.T) {
	// Setup test environment
	backend := &mockBackend{
		chain: &core.BlockChain{}, // You might want to use a more sophisticated mock
	}
	peer := &Peer{}

	// Create test blocks
	blocks := make([]*types.Block, 3)
	for i := 0; i < 3; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(100 - i)),
			ParentHash: common.HexToHash("0x123"),
		}
		body := &types.Body{
			Transactions: []*types.Transaction{},
			Uncles:       []*types.Header{},
		}
		blocks[i] = types.NewBlock(header, body, []*types.Receipt{}, nil)
	}

	// Test cases
	tests := []struct {
		name    string
		msg     *mockMsg
		wantErr bool
	}{
		{
			name: "Valid blocks response",
			msg: &mockMsg{
				code: BlocksByRangeMsg,
				data: &BlocksByRangePacket{
					RequestId: 1,
					Blocks:    blocks,
				},
			},
			wantErr: false,
		},
		{
			name: "Empty blocks response",
			msg: &mockMsg{
				code: BlocksByRangeMsg,
				data: &BlocksByRangePacket{
					RequestId: 2,
					Blocks:    []*types.Block{},
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid request ID",
			msg: &mockMsg{
				code: BlocksByRangeMsg,
				data: &BlocksByRangePacket{
					RequestId: 0,
					Blocks:    blocks,
				},
			},
			wantErr: true,
		},
		{
			name: "Non-continuous blocks",
			msg: &mockMsg{
				code: BlocksByRangeMsg,
				data: &BlocksByRangePacket{
					RequestId: 3,
					Blocks: []*types.Block{
						blocks[0],
						blocks[2], // Skip block 1 to create discontinuity
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handleBlocksByRange(backend, tt.msg, peer)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleBlocksByRange() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
