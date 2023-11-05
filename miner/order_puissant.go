package miner

import (
	"fmt"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"sort"
)

// transactions queue
type puissantTxQueue types.Transactions

func (s puissantTxQueue) Len() int { return len(s) }
func (s puissantTxQueue) Less(i, j int) bool {

	bundleSort := func(txI, txJ *types.Transaction) bool {
		_, txIBSeq, txIInnerSeq := txI.PuissantInfo()
		_, txJBSeq, txJInnerSeq := txJ.PuissantInfo()

		if txIBSeq == txJBSeq {
			return txIInnerSeq < txJInnerSeq
		}
		return txIBSeq < txJBSeq
	}

	cmp := s[i].GasPrice().Cmp(s[j].GasPrice())
	if cmp == 0 {
		iIsBundle := s[i].IsPuissant()
		jIsBundle := s[j].IsPuissant()

		if !iIsBundle && !jIsBundle {
			return s[i].Time().Before(s[j].Time())

		} else if iIsBundle && jIsBundle {
			return bundleSort(s[i], s[j])

		} else if iIsBundle {
			return true

		} else if jIsBundle {
			return false

		}
	}
	return cmp > 0
}

func (s puissantTxQueue) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

type TransactionsPuissant struct {
	txs                map[common.Address][]*types.Transaction
	txHeadsAndPuissant puissantTxQueue
	signer             types.Signer
	enabled            mapset.Set[types.PuissantID]
}

func NewTransactionsPuissant(signer types.Signer, txs map[common.Address][]*txpool.LazyTransaction, bundles types.PuissantBundles) *TransactionsPuissant {
	var tmpTxs = make(map[common.Address][]*types.Transaction)

	headsAndBundleTxs := make(puissantTxQueue, 0, len(txs))
	for from, accTxs := range txs {
		// Ensure the sender address is from the signer
		if acc, _ := types.Sender(signer, accTxs[0].Tx.Tx); acc != from {
			delete(txs, from)
			continue
		}
		headsAndBundleTxs = append(headsAndBundleTxs, accTxs[0].Tx.Tx)
		txs[from] = accTxs[1:]
	}

	for _, each := range bundles {
		for _, tx := range each.Txs() {
			headsAndBundleTxs = append(headsAndBundleTxs, tx)
		}
	}

	for from, accTxs := range txs {
		for _, tx := range accTxs {
			tmpTxs[from] = append(tmpTxs[from], tx.Tx.Tx)
		}
	}

	sort.Sort(&headsAndBundleTxs)
	return &TransactionsPuissant{
		enabled:            mapset.NewThreadUnsafeSet[types.PuissantID](),
		txs:                tmpTxs,
		txHeadsAndPuissant: headsAndBundleTxs,
		signer:             signer,
	}
}

func (t *TransactionsPuissant) ResetEnable(pids []types.PuissantID) {
	t.enabled.Clear()
	for _, pid := range pids {
		t.enabled.Add(pid)
	}
}

func (t *TransactionsPuissant) Copy() *TransactionsPuissant {
	if t == nil {
		return nil
	}

	newHeadsAndBundleTxs := make([]*types.Transaction, len(t.txHeadsAndPuissant))
	copy(newHeadsAndBundleTxs, t.txHeadsAndPuissant)
	txs := make(map[common.Address][]*types.Transaction, len(t.txs))
	for acc, txsTmp := range t.txs {
		txs[acc] = txsTmp
	}
	return &TransactionsPuissant{txHeadsAndPuissant: newHeadsAndBundleTxs, txs: txs, signer: t.signer, enabled: t.enabled.Clone()}
}

func (t *TransactionsPuissant) LogPuissantTxs() {
	for _, tx := range t.txHeadsAndPuissant {
		if tx.IsPuissant() {
			_, pSeq, bInnerSeq := tx.PuissantInfo()
			log.Info("puissant-tx", "seq", fmt.Sprintf("%2d - %d", pSeq, bInnerSeq), "hash", tx.Hash(), "revert", tx.AcceptsReverting(), "gp", tx.GasPrice().Uint64())
		}
	}
}

func (t *TransactionsPuissant) Peek() *types.Transaction {
	if len(t.txHeadsAndPuissant) == 0 {
		return nil
	}
	next := t.txHeadsAndPuissant[0]
	if pid := next.PuissantID(); pid.IsPuissant() && !t.enabled.Contains(pid) {
		t.Pop()
		return t.Peek()
	}
	return next
}

func (t *TransactionsPuissant) Shift() {
	acc, _ := types.Sender(t.signer, t.txHeadsAndPuissant[0])
	if !t.txHeadsAndPuissant[0].IsPuissant() {
		if txs, ok := t.txs[acc]; ok && len(txs) > 0 {
			t.txHeadsAndPuissant[0], t.txs[acc] = txs[0], txs[1:]
			sort.Sort(&t.txHeadsAndPuissant)
			return
		}
	}
	t.Pop()
}

// Pop removes the best transaction, *not* replacing it with the next one from
// the same account. This should be used when a transaction cannot be executed
// and hence all subsequent ones should be discarded from the same account.
func (t *TransactionsPuissant) Pop() {
	if len(t.txHeadsAndPuissant) > 0 {
		t.txHeadsAndPuissant = t.txHeadsAndPuissant[1:]
	}
}
