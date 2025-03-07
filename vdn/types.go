package vdn

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/types"
)

type Signature struct {
	V *big.Int
	R *big.Int
	S *big.Int
}

type HandshakeMsg struct {
	ChainID     uint64
	ForkID      forkid.ID
	GenesisHash common.Hash
	NodeVersion string
	Extend      []byte
	Sign        Signature
}

type ContactInfoMsg struct {
	PeerID         string
	ListenP2PAddrs []string         // validator can connect it by the addresses.
	Cache          []ContactInfoMsg // max 8 cache node's contact
	CreateTime     uint64
	Sign           Signature
}

type StatusCode uint8

const (
	NoErrorCode StatusCode = iota
)

type StatusCodeRespBody struct {
	Code StatusCode
}

// BlockMsg is same as NewBlockPacket in eth/protocols/eth/protocol.go now
type BlockMsg struct {
	Block      *types.Block
	TD         *big.Int
	Sidecars   types.BlobSidecars `rlp:"optional"`
	CreateTime uint64             `rlp:"optional"`
}

// SanityCheck verifies that the values are reasonable, as a DoS protection
func (b *BlockMsg) SanityCheck() error {
	if err := b.Block.SanityCheck(); err != nil {
		return err
	}
	//TD at mainnet block #7753254 is 76 bits. If it becomes 100 million times
	// larger, it will still fit within 100 bits
	if tdlen := b.TD.BitLen(); tdlen > 100 {
		return fmt.Errorf("too large block TD: bitlen %d", tdlen)
	}

	if hash := types.CalcUncleHash(b.Block.Uncles()); hash != b.Block.UncleHash() {
		log.Warn("Propagated block has invalid uncles", "have", hash, "exp", b.Block.UncleHash())
		return nil
	}
	if hash := types.DeriveSha(b.Block.Transactions(), trie.NewStackTrie(nil)); hash != b.Block.TxHash() {
		log.Warn("Propagated block has invalid body", "have", hash, "exp", b.Block.TxHash())
		return nil
	}
	if len(b.Sidecars) > 0 {
		for _, sidecar := range b.Sidecars {
			if err := sidecar.SanityCheck(b.Block.Number(), b.Block.Hash()); err != nil {
				return err
			}
		}
	}

	return nil
}

type BlockByRangeReq struct {
	StartHeight uint64
	Count       uint64
}

// BlockPacket represents the data content of a single block.
type BlockPacket struct {
	Header       *types.Header
	Transactions []*types.Transaction // Transactions contained within a block
	Uncles       []*types.Header      // Uncles contained within a block
	Withdrawals  []*types.Withdrawal  `rlp:"optional"` // Withdrawals contained within a block
	Sidecars     types.BlobSidecars   `rlp:"optional"` // Sidecars contained within a block
}

type BlockByRangeResp struct {
	StatusCode
	Blocks []*BlockPacket
}

type VoteMsg struct {
	Vote       *types.VoteEnvelope
	CreateTime uint64
}

// SanityCheck verifies that the values are reasonable, as a DoS protection
func (v *VoteMsg) SanityCheck() error {
	if v.Vote == nil {
		return fmt.Errorf("missing VoteEnvelope")
	}
	if v.Vote.Data == nil {
		return fmt.Errorf("missing VoteData")
	}
	return nil
}

type TransactionsMsg struct {
	Txs []*types.Transaction // not exceed 10MB in total msg size
}
