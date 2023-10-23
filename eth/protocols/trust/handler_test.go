package trust

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/clique"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
)

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")

	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)
)

// testBackend is a mock implementation of the live Ethereum message handler. Its
// purpose is to allow testing the request/reply workflows and wire serialization
// in the `eth` protocol without actually doing any data processing.
type testBackend struct {
	db     ethdb.Database
	chain  *core.BlockChain
	txpool *legacypool.LegacyPool
}

// newTestBackend creates an empty chain and wraps it into a mock backend.
func newTestBackend(blocks int) *testBackend {
	return newTestBackendWithGenerator(blocks)
}

// newTestBackend creates a chain with a number of explicitly defined blocks and
// wraps it into a mock backend.
func newTestBackendWithGenerator(blocks int) *testBackend {
	signer := types.HomesteadSigner{}
	db := rawdb.NewMemoryDatabase()
	engine := clique.New(params.AllCliqueProtocolChanges.Clique, db)
	genspec := &core.Genesis{
		Config:    params.AllCliqueProtocolChanges,
		ExtraData: make([]byte, 32+common.AddressLength+65),
		Alloc:     core.GenesisAlloc{testAddr: {Balance: big.NewInt(100000000000000000)}},
		BaseFee:   big.NewInt(0),
	}
	copy(genspec.ExtraData[32:], testAddr[:])
	genesis := genspec.MustCommit(db, trie.NewDatabase(db, nil))

	chain, _ := core.NewBlockChain(db, nil, genspec, nil, engine, vm.Config{}, nil, nil)
	generator := func(i int, block *core.BlockGen) {
		// The chain maker doesn't have access to a chain, so the difficulty will be
		// lets unset (nil). Set it here to the correct value.
		// block.SetCoinbase(testAddr)
		block.SetDifficulty(big.NewInt(2))

		// We want to simulate an empty middle block, having the same state as the
		// first one. The last is needs a state change again to force a reorg.
		tx, err := types.SignTx(types.NewTransaction(block.TxNonce(testAddr), common.Address{0x01}, big.NewInt(1), params.TxGas, nil, nil), signer, testKey)
		if err != nil {
			panic(err)
		}
		block.AddTxWithChain(chain, tx)
	}

	bs, _ := core.GenerateChain(params.AllCliqueProtocolChanges, genesis, engine, db, blocks, generator)
	for i, block := range bs {
		header := block.Header()
		if i > 0 {
			header.ParentHash = bs[i-1].Hash()
		}
		header.Extra = make([]byte, 32+65)
		header.Difficulty = big.NewInt(2)

		sig, _ := crypto.Sign(clique.SealHash(header).Bytes(), testKey)
		copy(header.Extra[len(header.Extra)-65:], sig)
		bs[i] = block.WithSeal(header)
	}

	if _, err := chain.InsertChain(bs); err != nil {
		panic(err)
	}

	txconfig := legacypool.DefaultConfig
	txconfig.Journal = "" // Don't litter the disk with test journals

	return &testBackend{
		db:     db,
		chain:  chain,
		txpool: legacypool.New(txconfig, chain),
	}
}

// close tears down the transaction pool and chain behind the mock backend.
func (b *testBackend) close() {
	b.txpool.Close()
	b.chain.Stop()
}

func (b *testBackend) Chain() *core.BlockChain { return b.chain }

func (b *testBackend) RunPeer(peer *Peer, handler Handler) error {
	// Normally the backend would do peer mainentance and handshakes. All that
	// is omitted and we will just give control back to the handler.
	return handler(peer)
}
func (b *testBackend) PeerInfo(enode.ID) interface{} { panic("not implemented") }

func (b *testBackend) Handle(*Peer, Packet) error {
	panic("data processing tests should be done in the handler package")
}

func TestRequestRoot(t *testing.T) { testRequestRoot(t, Trust1) }

func testRequestRoot(t *testing.T, protocol uint) {
	t.Parallel()

	blockNum := 1032 // The latest 1024 blocks' DiffLayer will be cached.
	backend := newTestBackend(blockNum)
	defer backend.close()

	peer, _ := newTestPeer("peer", protocol, backend)
	defer peer.close()

	pairs := []struct {
		req RootRequestPacket
		res RootResponsePacket
	}{
		{
			req: RootRequestPacket{
				RequestId:   1,
				BlockNumber: 1,
			},
			res: RootResponsePacket{
				RequestId:   1,
				Status:      types.StatusPartiallyVerified,
				BlockNumber: 1,
				Extra:       defaultExtra,
			},
		},
		{
			req: RootRequestPacket{
				RequestId:   2,
				BlockNumber: 128,
			},
			res: RootResponsePacket{
				RequestId:   2,
				Status:      types.StatusFullVerified,
				BlockNumber: 128,
				Extra:       defaultExtra,
			},
		},
		{
			req: RootRequestPacket{
				RequestId:   3,
				BlockNumber: 128,
				BlockHash:   types.EmptyRootHash,
				DiffHash:    types.EmptyRootHash,
			},
			res: RootResponsePacket{
				RequestId:   3,
				Status:      types.StatusImpossibleFork,
				BlockNumber: 128,
				BlockHash:   types.EmptyRootHash,
				Root:        common.Hash{},
				Extra:       defaultExtra,
			},
		},
		{
			req: RootRequestPacket{
				RequestId:   4,
				BlockNumber: 128,
				DiffHash:    types.EmptyRootHash,
			},
			res: RootResponsePacket{
				RequestId:   4,
				Status:      types.StatusDiffHashMismatch,
				BlockNumber: 128,
				Root:        common.Hash{},
				Extra:       defaultExtra,
			},
		},
		{
			req: RootRequestPacket{
				RequestId:   5,
				BlockNumber: 1024,
			},
			res: RootResponsePacket{
				RequestId:   5,
				Status:      types.StatusFullVerified,
				BlockNumber: 1024,
				Extra:       defaultExtra,
			},
		},
		{
			req: RootRequestPacket{
				RequestId:   6,
				BlockNumber: 1024,
				BlockHash:   types.EmptyRootHash,
				DiffHash:    types.EmptyRootHash,
			},
			res: RootResponsePacket{
				RequestId:   6,
				Status:      types.StatusPossibleFork,
				BlockNumber: 1024,
				BlockHash:   types.EmptyRootHash,
				Root:        common.Hash{},
				Extra:       defaultExtra,
			},
		},
		{
			req: RootRequestPacket{
				RequestId:   7,
				BlockNumber: 1033,
				BlockHash:   types.EmptyRootHash,
				DiffHash:    types.EmptyRootHash,
			},
			res: RootResponsePacket{
				RequestId:   7,
				Status:      types.StatusBlockNewer,
				BlockNumber: 1033,
				BlockHash:   types.EmptyRootHash,
				Root:        common.Hash{},
				Extra:       defaultExtra,
			},
		},
		{
			req: RootRequestPacket{
				RequestId:   8,
				BlockNumber: 1044,
				BlockHash:   types.EmptyRootHash,
				DiffHash:    types.EmptyRootHash,
			},
			res: RootResponsePacket{
				RequestId:   8,
				Status:      types.StatusBlockTooNew,
				BlockNumber: 1044,
				BlockHash:   types.EmptyRootHash,
				Root:        common.Hash{},
				Extra:       defaultExtra,
			},
		},
	}

	for idx, pair := range pairs {
		header := backend.Chain().GetHeaderByNumber(pair.req.BlockNumber)
		if header != nil {
			if pair.res.Status.Code&0xFF00 == types.StatusVerified.Code {
				pair.req.BlockHash = header.Hash()
				pair.req.DiffHash, _ = core.CalculateDiffHash(backend.Chain().GetTrustedDiffLayer(header.Hash()))
				pair.res.BlockHash = pair.req.BlockHash
				pair.res.Root = header.Root
			} else if pair.res.Status.Code == types.StatusDiffHashMismatch.Code {
				pair.req.BlockHash = header.Hash()
				pair.res.BlockHash = pair.req.BlockHash
			}
		}

		p2p.Send(peer.app, RequestRootMsg, pair.req)
		if err := p2p.ExpectMsg(peer.app, RespondRootMsg, pair.res); err != nil {
			t.Errorf("test %d: root response not expected: %v", idx, err)
		}
	}
}
