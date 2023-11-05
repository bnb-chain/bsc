package core

import (
	"context"
	"errors"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"math/big"
	"strings"
	"time"
)

type puissantRuntime struct {
	env            *MinerEnvironment
	snapshots      *snapshotSet
	chain          *BlockChain
	chainConfig    *params.ChainConfig
	bloomProcessor *ReceiptBloomGenerator

	pTxsStatuses   map[types.PuissantID][]*ptxCommitStatus
	pSeqID2PIDBook map[int]types.PuissantID
	succeedGroup   mapset.Set[types.PuissantID]
	failedGroup    mapset.Set[types.PuissantID]
	failedTx       mapset.Set[common.Hash]
}

func RunPuissantCommitter(
	timeLeft time.Duration,
	workerEnv *MinerEnvironment,
	pendingPool map[common.Address][]*types.Transaction,
	puissantPool types.PuissantBundles,
	chain *BlockChain,
	chainConfig *params.ChainConfig,
	poolRemoveFn func(mapset.Set[types.PuissantID]),

	packedPuissantTxs *lru.Cache[common.Hash, struct{}],
) ([]*CommitterReport, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeLeft)
	defer cancel()

	var committer = &puissantRuntime{
		env:            workerEnv,
		snapshots:      newSnapshotSet(),
		chain:          chain,
		chainConfig:    chainConfig,
		bloomProcessor: NewReceiptBloomGenerator(),
		pTxsStatuses:   make(map[types.PuissantID][]*ptxCommitStatus),
		pSeqID2PIDBook: make(map[int]types.PuissantID),
		succeedGroup:   mapset.NewThreadUnsafeSet[types.PuissantID](),
		failedGroup:    mapset.NewThreadUnsafeSet[types.PuissantID](),
		failedTx:       mapset.NewThreadUnsafeSet[common.Hash](),
	}
	defer poolRemoveFn(committer.failedGroup)

	for pSeq, pGroup := range puissantPool {
		committer.pTxsStatuses[pGroup.ID()] = make([]*ptxCommitStatus, pGroup.TxCount())
		committer.pSeqID2PIDBook[pSeq] = pGroup.ID()
		for index, tx := range pGroup.Txs() {
			committer.pTxsStatuses[pGroup.ID()][index] = initPTxCommitStatus(tx, workerEnv.Signer)
		}
	}
	workerEnv.PuissantTxQueue = types.NewTransactionsPuissant(workerEnv.Signer, pendingPool, puissantPool)
	workerEnv.PuissantTxQueue.LogPuissantTxs()
	committer.resetRunningGroup()

	for {
		select {
		case <-ctx.Done():
			return committer.finalStatistics(true)
		default:
		}

		if gasLeft := workerEnv.GasPool.Gas(); gasLeft < params.TxGas {
			log.Warn(" 🐶 not enough gas for further transactions, commit directly", "packed-count", workerEnv.TxCount, "have", gasLeft, "want", params.TxGas)
			return committer.finalStatistics(true)
		}

		tx := workerEnv.PuissantTxQueue.Peek()
		if tx == nil {
			return committer.finalStatistics(false)
		}
		if _, exist := packedPuissantTxs.Get(tx.Hash()); exist {
			log.Warn(" 👮 tx already packed", "tx", tx.Hash())
			if tx.IsPuissant() {
				committer.failedGroup.Add(tx.PuissantID())
				return nil, errors.New("👮 previous packed puissant included, abort this round")
			}
			workerEnv.PuissantTxQueue.Shift()
			continue
		}

		committer.commitTransaction(tx)
	}
}

type CommitterReport struct {
	PuissantID types.PuissantID
	Status     puissantStatusCode
	Info       puissantInfoCode
	Txs        []*ptxCommitStatus
	Income     *big.Int
	EvmRun     bool
}

func (p *puissantRuntime) finalStatistics(integrityCheck bool) ([]*CommitterReport, error) {
	if integrityCheck {
		var unfinished = mapset.NewThreadUnsafeSet[types.PuissantID]()
		for pid := range p.pTxsStatuses {
			if !p.succeedGroup.Contains(pid) {
				unfinished.Add(pid)
			}
		}
		for _, tx := range p.env.PackedTxs {
			if unfinished.Contains(tx.PuissantID()) {
				return nil, errors.New("unfinished puissant included")
			}
		}
	}

	var report = make([]*CommitterReport, len(p.pSeqID2PIDBook))

	for bSeq := 0; bSeq < len(p.pSeqID2PIDBook); bSeq++ {
		var (
			income = big.NewInt(0)
			pid    = p.pSeqID2PIDBook[bSeq]
			pRpt   = &CommitterReport{
				PuissantID: pid,
				Status:     PuissantStatusWellDone,
				Info:       PuissantInfoCodeOk,
				Txs:        make([]*ptxCommitStatus, len(p.pTxsStatuses[pid])),
				EvmRun:     p.failedGroup.Contains(pid) || p.succeedGroup.Contains(pid),
			}
		)

		for txSeq, txRpt := range p.pTxsStatuses[pid] {
			if txRpt.error != nil {

				switch {
				case errors.Is(txRpt.error, types.PuiErrTxConflict):
					txRpt.status = PuissantTransactionStatusConflictedBeaten
					if pRpt.Info == PuissantInfoCodeOk {
						pRpt.Info = PuissantInfoCodeBeaten
					}

				case errors.Is(txRpt.error, types.PuiErrTxConflict):
					txRpt.status = PuissantTransactionStatusNoRun

				default:
					txRpt.status = PuissantTransactionStatusRevert
					pRpt.Info = PuissantInfoCodeRevert
				}
				pRpt.Status = PuissantStatusDropped
				txRpt.revertMsg = txRpt.error.Error()
			}
			if txSeq == 0 {
				income = new(big.Int).Mul(txRpt.gasPrice, new(big.Int).SetUint64(txRpt.gasUsed))
			}
			pRpt.Txs[txSeq] = txRpt
		}
		if pRpt.Status == PuissantStatusWellDone {
			pRpt.Income = income
		}
		report[bSeq] = pRpt
	}

	return report, nil
}

func (p *puissantRuntime) commitTransaction(tx *types.Transaction) {
	var (
		pid, pSeq, pTxSeq = tx.PuissantInfo()
		isBundleTx        = pid.IsPuissant()
	)

	if isBundleTx {
		p.snapshots.save(pid, p.env)
	}

	p.env.State.SetTxContext(tx.Hash(), p.env.TxCount)

	txReceipt, err := commitTransaction(tx, p.chain, p.chainConfig, p.env.Header.Coinbase,
		p.env.State, p.env.GasPool, p.env.Header,
		isBundleTx && !tx.AcceptsReverting(), isBundleTx && pTxSeq == 0, p.bloomProcessor)

	if err == nil {
		p.env.PackTx(tx, txReceipt)
	}

	// If it's not a puissant tx, return and run next. Otherwise, update puissant status
	if !isBundleTx {
		if errors.Is(err, ErrGasLimitReached) ||
			errors.Is(err, ErrNonceTooHigh) ||
			errors.Is(err, ErrTxTypeNotSupported) {
			p.env.PuissantTxQueue.Pop()
		} else {
			p.env.PuissantTxQueue.Shift()
		}

		return
	}

	txStatus := p.pTxsStatuses[pid][pTxSeq]
	txStatus.error = err
	txStatus.hash = tx.Hash() // update hash again for auto burning tx

	if err != nil {
		log.Warn(" 🐶 puissant-tx ❌", "seq", pSeq, "tx", pTxSeq, "err", err)
		txStatus.status = PuissantTransactionStatusRevert

		p.failedGroup.Add(pid)

		if !strings.HasPrefix(err.Error(), "execution reverted") {
			p.failedTx.Add(tx.Hash())
		} else {
			var hasPreAffairs = false
			for _txSeq := 0; _txSeq < pTxSeq; _txSeq++ {
				if txStatus.gasUsed > 25000 && txStatus.hasData {
					hasPreAffairs = true
					break
				}
			}
			if !hasPreAffairs {
				p.failedTx.Add(tx.Hash())
			}
		}
		p.snapshots.revert(pid, p.env)
		// revert first, then reset
		p.resetRunningGroup()

	} else {
		log.Info(" 🐶 puissant-tx ✅", "seq", pSeq, "tx", pTxSeq)
		txStatus.gasUsed = txReceipt.GasUsed
		txStatus.status = PuissantTransactionStatusOk
		if p.pTxsStatuses[pid][len(p.pTxsStatuses[pid])-1].error == nil {
			p.succeedGroup.Add(pid)
		}
		p.env.PuissantTxQueue.Pop()
	}
}

func (p *puissantRuntime) resetRunningGroup() {
	var (
		included   = mapset.NewThreadUnsafeSet[string]()
		enabled    []types.PuissantID
		enabledSeq []int
	)

	for pSeq := 0; pSeq < len(p.pSeqID2PIDBook); pSeq++ {
		var (
			conflict      = false
			failedHistory = -1
			pid           = p.pSeqID2PIDBook[pSeq]
			txsStatus     = p.pTxsStatuses[pid]
		)

		if p.failedGroup.Contains(pid) {
			continue
		}

		for _txSeq, tx := range txsStatus {
			if included.Contains(tx.uniqueID) {
				conflict = true
				tx.error = types.PuiErrTxConflict
				tx.status = PuissantTransactionStatusConflictedBeaten
				break
			}
			if p.failedTx.Contains(tx.hash) && failedHistory == -1 {
				failedHistory = _txSeq
			}
		}
		if !conflict {
			if failedHistory != -1 {
				var couldRecover = false
				for _txSeq := 0; _txSeq < failedHistory; _txSeq++ {
					if txsStatus[_txSeq].hasData && txsStatus[_txSeq].gasLimit > 25000 {
						couldRecover = true
					}
				}
				if !couldRecover {
					continue
				}
			}
			for _, tx := range txsStatus {
				included.Add(tx.uniqueID)
			}
			enabled = append(enabled, pid)
			enabledSeq = append(enabledSeq, pSeq)
		}
	}
	if len(enabled) > 0 {
		log.Info(" 🐶 running-group-reset 🔧", "len", len(enabled), "new-set", enabledSeq)
	}
	p.env.PuissantTxQueue.ResetEnable(enabled)
}

type envData struct {
	storeEnv   *MinerEnvironment
	snapshotID int
}

type snapshotSet struct {
	snapshotID int
	snapshots  map[types.PuissantID]*envData
}

func newSnapshotSet() *snapshotSet {
	return &snapshotSet{snapshots: make(map[types.PuissantID]*envData)}
}

func (pc *snapshotSet) save(pid types.PuissantID, currEnv *MinerEnvironment) bool {
	if _, ok := pc.snapshots[pid]; ok {
		return false
	}
	pc.snapshots[pid] = &envData{
		storeEnv:   currEnv.Copy(),
		snapshotID: pc.snapshotID,
	}
	//log.Info(" 🐶 📷 take snapshot", "snapshotID", pc.snapshotID, "packed-count", currEnv.TxCount)
	pc.snapshotID++
	return true
}

func (pc *snapshotSet) revert(bID types.PuissantID, currEnv *MinerEnvironment) {
	load := pc.snapshots[bID]

	if currEnv.TxCount != load.storeEnv.TxCount {
		// should reload work env
		//load.storeEnv.State.TransferPrefetcher(currEnv.State)
		*currEnv = *load.storeEnv
	}

	for _bID, data := range pc.snapshots {
		if data.snapshotID >= load.snapshotID {
			delete(pc.snapshots, _bID)
			//log.Warn(" 🐶 remove state-snapshot", "snapshotID", data.snapshotID)
		}
	}
}
