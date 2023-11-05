package core

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"math/big"
	"strconv"
)

type ptxCommitStatus struct {
	hash      common.Hash
	gasPrice  *big.Int
	gasUsed   uint64
	gasLimit  uint64
	hasData   bool
	error     error
	status    puissantTxStatusCode
	revertMsg string

	uniqueID string
}

func initPTxCommitStatus(tx *types.Transaction, signer types.Signer) *ptxCommitStatus {
	sender, _ := types.Sender(signer, tx)

	return &ptxCommitStatus{
		hash:      tx.Hash(),
		gasPrice:  tx.GasPrice(),
		gasLimit:  tx.Gas(),
		hasData:   len(tx.Data()) > 4,
		error:     types.PuiErrTxNoRun,
		status:    PuissantTransactionStatusNoRun,
		gasUsed:   0,
		revertMsg: "",

		uniqueID: sender.Hex() + strconv.FormatUint(tx.Nonce(), 10),
	}
}

func commitTransaction(tx *types.Transaction, chain *BlockChain, chainConfig *params.ChainConfig, coinbase common.Address, envState *state.StateDB, envGasPool *GasPool, envHeader *types.Header, revertIfFailed, gasReq21000 bool, receiptProcessors ...ReceiptProcessor) (*types.Receipt, error) {
	snap := envState.Snapshot()
	receipt, err := ApplyTransaction(chainConfig, chain, &coinbase, envGasPool, envState, envHeader, tx, &envHeader.GasUsed, *chain.GetVMConfig(), revertIfFailed, gasReq21000, receiptProcessors...)
	if err != nil {
		envState.RevertToSnapshot(snap)
		return nil, err
	}

	return receipt, nil
}

func CreateGasPool(srcGasPool *GasPool, chainConf *params.ChainConfig, header *types.Header) *GasPool {
	if srcGasPool != nil {
		return new(GasPool).AddGas(srcGasPool.Gas())
	}

	gasPool := new(GasPool).AddGas(header.GasLimit)
	if chainConf.IsEuler(header.Number) {
		gasPool.SubGas(params.SystemTxsGas * 3)
	} else {
		gasPool.SubGas(params.SystemTxsGas)
	}
	return gasPool
}
