package tracer

import (
	"fmt"
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

func (f *EthFeed) FeedTransactions(block *types.Block, receipts types.Receipts) []evm_types.Transaction {
	signer := types.MakeSigner(f.chainConfig, block.Number())
	var transactions []evm_types.Transaction

	for i, tx := range block.Transactions() {
		var transaction evm_types.Transaction
		transaction.TxIndex = uint32(i)
		transaction.TxHash = tx.Hash().String()
		transaction.Type = tx.Type()
		transaction.Nonce = tx.Nonce()
		transaction.Status = receipts[i].Status
		transaction.BlockIndex = block.NumberU64()
		address, err := types.Sender(signer, tx)
		if err != nil {
			fmt.Println("Sender error", err)
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

func (f *EthFeed) FeedCalTraces(callFrames []*TxTraceResult, blockNumber uint64) []evm_types.CallTrace {
	var callTraces []evm_types.CallTrace
	for i, frame := range callFrames {
		if frame != nil && frame.Error != "" {
			log.Error("call frame error", "err", frame.Error, "mamoru-tracer", "bsc_feed")
			continue
		}
		if frame == nil {
			log.Error("call frame is empty", "number", blockNumber, "callFrame", i, "mamoru-tracer", "bsc_feed")
			continue
		}
		for seq, call := range frame.Result {
			var callTrace evm_types.CallTrace
			callTrace.Seq = uint32(seq)
			callTrace.TxIndex = uint32(i)
			callTrace.BlockIndex = blockNumber
			callTrace.Type = call.Type
			callTrace.To = call.To
			callTrace.From = call.From
			callTrace.Value = call.Value
			callTrace.GasLimit = call.Gas
			callTrace.GasUsed = call.GasUsed
			callTrace.Input = call.Input

			callTraces = append(callTraces, callTrace)
		}
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

			events = append(events, event)
		}
	}

	return events
}
