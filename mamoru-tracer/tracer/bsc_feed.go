package tracer

import (
	"fmt"
	"github.com/Mamoru-Foundation/mamoru-sniffer-go/evm_types"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

var _ Feeder = &BSCFeed{}

type BSCFeed struct {
	chainConfig *params.ChainConfig
}

func NewFeed(chainConfig *params.ChainConfig) Feeder {
	return &BSCFeed{chainConfig: chainConfig}
}

func (f *BSCFeed) FeedBlock(block *types.Block) evm_types.Block {
	var blockData evm_types.Block

	blockData.BlockIndex = block.NumberU64()
	blockData.Hash = block.Hash().String()
	blockData.ParentHash = block.ParentHash().String()
	blockData.StateRoot = block.Root().String()
	blockData.Nonce = block.Nonce()
	blockData.Status = ""
	blockData.Timestamp = block.Time()
	blockData.BlockReward = block.Coinbase().Bytes()      // ?
	blockData.FeeRecipient = block.ReceiptHash().String() // ?
	blockData.TotalDifficulty = block.Difficulty().Uint64()
	blockData.Size = float64(block.Size()) // bytes
	blockData.GasUsed = block.GasUsed()
	blockData.GasLimit = block.GasLimit()
	blockData.BurntFees = nil                //? gasprice
	blockData.PosProposedOnTime = 0          // ?
	blockData.PosSlot = 0                    // ?
	blockData.PosEpoch = 0                   // ?
	blockData.PosProposerIndex = 0           // ?
	blockData.PosSlotRootHash = nil          // ?
	blockData.PosBeaconChainDepositCount = 0 // ?
	blockData.PosSlotGraffiti = nil          // ?
	blockData.PosBlockRandomness = nil       // ?
	blockData.PosRandomReveal = nil          // ?

	return blockData
}

func (f *BSCFeed) FeedTransactions(block *types.Block, receipts types.Receipts) ([]evm_types.Transaction, []evm_types.TransactionArg) {
	signer := types.MakeSigner(f.chainConfig, block.Number())
	var transactions []evm_types.Transaction
	var transactionArgs []evm_types.TransactionArg

	for i, tx := range block.Transactions() {
		var transaction evm_types.Transaction
		transaction.TxIndex = uint32(i)
		transaction.TxHash = tx.Hash().String()
		transaction.Type = tx.Type()
		transaction.Nonce = tx.Nonce()
		transaction.Status = receipts[i].Status
		transaction.BlockIndex = block.NumberU64()
		transaction.Timestamp = uint64(tx.Time().Unix())
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
		transaction.GasUsed = tx.Cost().Uint64() // Cost returns gas * gasPrice + value.
		transaction.Size = float64(tx.Size())
		transaction.Method = "" // TBD

		transactions = append(transactions, transaction)
	}

	return transactions, transactionArgs
}

func (f *BSCFeed) FeedCalTraces(callFrames []*TxTraceResult, blockNumber uint64) ([]evm_types.CallTrace, []evm_types.CallTraceArg) {
	var callTraces []evm_types.CallTrace
	var callTraceArgs []evm_types.CallTraceArg
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
			callTrace.MethodId = call.MethodId

			for _, arg := range call.MethodArgs {
				callTraceArgs = append(callTraceArgs, evm_types.CallTraceArg{
					CallTraceSeq: uint32(seq),
					BlockIndex:   blockNumber,
					TxIndex:      uint32(i),
					Depth:        call.Depth,
					Arg:          arg,
				})
			}

			callTraces = append(callTraces, callTrace)
		}
	}

	return callTraces, callTraceArgs
}

func (f *BSCFeed) FeedEvents(receipts types.Receipts) ([]evm_types.Event, []evm_types.EventTopic) {
	var events []evm_types.Event
	var eventTopics []evm_types.EventTopic
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

			for _, t := range rlog.Topics {
				eventTopics = append(eventTopics, evm_types.EventTopic{
					Index: uint32(rlog.Index),
					Topic: t.String(),
				})
			}

			events = append(events, event)
		}
	}

	return events, eventTopics
}
