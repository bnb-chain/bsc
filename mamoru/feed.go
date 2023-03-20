package mamoru

import (
	"math/big"

	"github.com/Mamoru-Foundation/mamoru-sniffer-go/evm_types"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

var _ Feeder = &EthFeed{}

type EthFeed struct {
	chainConfig *params.ChainConfig
}

func NewFeed(chainConfig *params.ChainConfig) Feeder {
	return &EthFeed{chainConfig: chainConfig}
}

func (f *EthFeed) FeedBlock(block *types.Block) evm_types.Block {
	var blockData evm_types.Block
	blockData.BlockIndex = block.NumberU64()
	blockData.Hash = block.Hash().String()
	blockData.ParentHash = block.ParentHash().String()
	blockData.StateRoot = block.Root().String()
	blockData.Nonce = block.Nonce()
	blockData.Status = ""
	blockData.Timestamp = block.Time()
	blockData.BlockReward = block.Coinbase().Bytes()
	blockData.FeeRecipient = block.ReceiptHash().String()
	blockData.TotalDifficulty = block.Difficulty().Uint64()
	blockData.Size = float64(block.Size())
	blockData.GasUsed = block.GasUsed()
	blockData.GasLimit = block.GasLimit()

	return blockData
}

func (f *EthFeed) FeedTransactions(blockNumber *big.Int, txs types.Transactions, receipts types.Receipts) []evm_types.Transaction {
	signer := types.MakeSigner(f.chainConfig, blockNumber)
	var transactions []evm_types.Transaction

	for i, tx := range txs {
		var transaction evm_types.Transaction
		transaction.TxIndex = uint32(i)
		transaction.TxHash = tx.Hash().String()
		transaction.Type = tx.Type()
		transaction.Nonce = tx.Nonce()
		if receipts.Len() > i {
			transaction.Status = receipts[i].Status
		}
		transaction.BlockIndex = blockNumber.Uint64()
		address, err := types.Sender(signer, tx)
		if err != nil {
			log.Error("Sender error", "err", err, "mamoru-tracer", "bsc_feed")
		}
		transaction.From = address.String()
		if tx.To() != nil {
			transaction.To = tx.To().String()
		}
		transaction.Value = tx.Value().Uint64()
		transaction.Fee = tx.GasFeeCap().Uint64()
		transaction.GasPrice = tx.GasPrice().Uint64()
		transaction.GasLimit = tx.Gas()
		transaction.GasUsed = tx.Cost().Uint64()
		transaction.Size = float64(tx.Size())
		transaction.Input = tx.Data()

		transactions = append(transactions, transaction)
	}

	return transactions
}

func (f *EthFeed) FeedCallTraces(callFrames []*CallFrame, blockNumber uint64) []evm_types.CallTrace {
	var callTraces []evm_types.CallTrace
	for i, frame := range callFrames {
		var callTrace evm_types.CallTrace
		callTrace.TxIndex = uint32(i)
		callTrace.BlockIndex = blockNumber
		callTrace.Type = frame.Type
		callTrace.To = frame.To
		callTrace.From = frame.From
		callTrace.Value = frame.Value
		callTrace.GasLimit = frame.Gas
		callTrace.GasUsed = frame.GasUsed
		callTrace.Input = frame.Input

		callTraces = append(callTraces, callTrace)
	}

	return callTraces
}

func (f *EthFeed) FeedEvents(receipts types.Receipts) []evm_types.Event {
	var events []evm_types.Event
	for _, receipt := range receipts {
		for _, rlog := range receipt.Logs {
			var event evm_types.Event
			event.Index = uint32(rlog.Index)
			event.BlockNumber = rlog.BlockNumber
			event.BlockHash = rlog.BlockHash.String()
			event.TxIndex = uint32(rlog.TxIndex)
			event.TxHash = rlog.TxHash.String()
			event.Address = rlog.Address.String()
			event.Data = rlog.Data
			for i, topic := range rlog.Topics {
				switch i {
				case 0:
					event.Topic0 = topic.Bytes()
				case 1:
					event.Topic1 = topic.Bytes()
				case 2:
					event.Topic2 = topic.Bytes()
				case 3:
					event.Topic3 = topic.Bytes()
				case 4:
					event.Topic4 = topic.Bytes()
				}
			}
			events = append(events, event)
		}
	}

	return events
}
