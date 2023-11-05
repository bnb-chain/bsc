package core

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
)

// MinerEnvironment is the worker's current environment and holds all information of the sealing block generation.
type MinerEnvironment struct {
	Signer   types.Signer
	Coinbase common.Address

	GasPool   *GasPool
	State     *state.StateDB
	Header    *types.Header
	PackedTxs types.Transactions
	Receipts  types.Receipts
	TxCount   int

	// for puissant only
	PuissantTxQueue *types.TransactionsPuissant
}

// Copy creates a deep copy of environment.
func (env *MinerEnvironment) Copy() *MinerEnvironment {
	cpy := &MinerEnvironment{
		Signer:          env.Signer,
		State:           env.State.Copy(),
		TxCount:         env.TxCount,
		Coinbase:        env.Coinbase,
		Header:          types.CopyHeader(env.Header),
		Receipts:        types.CopyReceipts(env.Receipts),
		PuissantTxQueue: env.PuissantTxQueue.Copy(),
	}
	if env.GasPool != nil {
		gasPool := *env.GasPool
		cpy.GasPool = &gasPool
	}
	// The content of txs and uncles are immutable, unnecessary
	// to do the expensive deep copy for them.
	cpy.PackedTxs = make([]*types.Transaction, len(env.PackedTxs))
	copy(cpy.PackedTxs, env.PackedTxs)
	return cpy
}

// Discard terminates the background prefetcher go-routine. It should
// always be called for all created environment instances otherwise
// the go-routine leak can happen.
func (env *MinerEnvironment) Discard() {
	if env.State != nil {
		//env.State.StopPrefetcher()
	}
}

func (env *MinerEnvironment) PackTx(tx *types.Transaction, receipt *types.Receipt) {
	env.PackedTxs = append(env.PackedTxs, tx)
	env.Receipts = append(env.Receipts, receipt)
	env.TxCount++
}
