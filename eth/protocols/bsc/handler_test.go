package bsc

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/types/bal"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
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

// mockPeer implements a mock of the Peer for testing
type mockPeer struct {
	*Peer
	sentResponses map[uint64]interface{}
}

func newMockPeer() *mockPeer {
	mp := &mockPeer{
		Peer:          &Peer{},
		sentResponses: make(map[uint64]interface{}),
	}
	mp.id = "mock-peer-id"
	mp.logger = log.New("peer", mp.id)
	mp.term = make(chan struct{})
	mp.dispatcher = &Dispatcher{
		peer:     mp.Peer,
		requests: make(map[uint64]*Request),
	}
	return mp
}

func (mp *mockPeer) Log() log.Logger {
	return mp.logger
}

type rangeReply struct {
	p2p.MsgReadWriter
	packet BlocksByRangePacket
}

func (r *rangeReply) WriteMsg(msg p2p.Msg) error {
	return msg.Decode(&r.packet)
}

func TestHandleGetBlocksByRange(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	defer db.Close()
	config := *params.MergedTestChainConfig
	config.AmsterdamTime = new(uint64)
	chain, err := core.NewBlockChain(db, &core.Genesis{Config: &config, Difficulty: common.Big0}, beacon.NewFaker(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Stop()

	// The child has an encoded empty BAL. Its parent has only the commitment.
	parent := chain.Genesis()
	header := parent.Header()
	header.Number = big.NewInt(1)
	header.ParentHash = parent.Hash()
	child := types.NewBlockWithHeader(header).WithAccessListUnsafe(new(bal.BlockAccessList))
	rawdb.WriteBlock(db, child)
	rawdb.DeleteAccessList(db, parent.Hash(), parent.NumberU64())

	backend := &mockBackend{chain: chain}
	peer := newMockPeer().Peer
	peer.version = Bsc5
	req := &mockMsg{data: &GetBlocksByRangePacket{RequestId: 1, Count: 2, StartBlockHash: child.Hash()}}
	for _, tc := range []struct {
		finalized uint64
		want      int
	}{{0, 1}, {1_344_000, 1}, {1_344_001, 2}} {
		if tc.finalized != 0 {
			chain.SetFinalized(&types.Header{Number: new(big.Int).SetUint64(tc.finalized)})
		}
		reply := new(rangeReply)
		peer.rw = reply
		if err := handleGetBlocksByRange(backend, req, peer); err != nil {
			t.Fatal(err)
		}
		blocks := reply.packet.Blocks
		if len(blocks) != tc.want || blocks[0].Header.Hash() != child.Hash() || !bytes.Equal(blocks[0].BlockAccessList, rlp.EmptyList) {
			t.Fatalf("unexpected range response for finalized height %d: %v", tc.finalized, blocks)
		}
		if tc.want == 2 && (blocks[1].Header.Hash() != parent.Hash() || len(blocks[1].BlockAccessList) != 0) {
			t.Fatal("expected parent without its expired BAL")
		}
	}
	if err := handleGetBlocksByRange(backend, &mockMsg{data: &GetBlocksByRangePacket{}}, peer); err == nil {
		t.Fatal("accepted zero count")
	}
}

func TestHandleBlocksByRange(t *testing.T) {
	// Setup test environment
	backend := &mockBackend{
		chain: &core.BlockChain{}, // You might want to use a more sophisticated mock
	}

	// Create a more complete mock peer
	mockPeer := newMockPeer()
	peer := mockPeer.Peer

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

	// Convert blocks to BlockData
	blockDataList := make([]*BlockData, len(blocks))
	for i, block := range blocks {
		blockDataList[i] = NewBlockData(block)
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
					Blocks:    blockDataList,
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
					Blocks:    []*BlockData{},
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid request ID",
			msg: &mockMsg{
				code: BlocksByRangeMsg,
				data: &BlocksByRangePacket{
					RequestId: 0,
					Blocks:    blockDataList,
				},
			},
			wantErr: false,
		},
		{
			name: "Non-continuous blocks",
			msg: &mockMsg{
				code: BlocksByRangeMsg,
				data: &BlocksByRangePacket{
					RequestId: 3,
					Blocks: []*BlockData{
						blockDataList[0],
						blockDataList[2], // Skip block 1 to create discontinuity
					},
				},
			},
			wantErr: false,
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
