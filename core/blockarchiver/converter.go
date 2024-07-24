package blockarchiver

import (
	"errors"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

// HexToUint64 converts hex string to uint64
func HexToUint64(hexStr string) (uint64, error) {
	intValue, err := strconv.ParseUint(hexStr, 0, 64)
	if err != nil {
		return 0, err
	}
	return intValue, nil
}

// Int64ToHex converts int64 to hex string
func Int64ToHex(int64 int64) string {
	return "0x" + strconv.FormatInt(int64, 16)
}

// HexToBigInt converts hex string to big.Int
func HexToBigInt(hexStr string) (*big.Int, error) {
	if hexStr == "" {
		hexStr = "0x0"
	}
	hexStr = hexStr[2:]
	bigInt := new(big.Int)
	_, success := bigInt.SetString(hexStr, 16)
	if !success {
		return nil, errors.New("error converting hexadecimal string to big.Int")
	}
	return bigInt, nil
}

// convertBlock converts a block to a general block
func convertBlock(block *Block) (*GeneralBlock, error) {
	if block == nil {
		return nil, errors.New("block is nil")
	}
	diffculty, err := HexToBigInt(block.Difficulty)
	if err != nil {
		return nil, err
	}
	number, err := HexToBigInt(block.Number)
	if err != nil {
		return nil, err
	}
	gaslimit, err := HexToUint64(block.GasLimit)
	if err != nil {
		return nil, err
	}
	gasUsed, err := HexToUint64(block.GasUsed)
	if err != nil {
		return nil, err
	}
	ts, err := HexToUint64(block.Timestamp)
	if err != nil {
		return nil, err
	}
	nonce, err := HexToUint64(block.Nonce)
	if err != nil {
		return nil, err
	}
	totalDifficulty, err := HexToBigInt(block.TotalDifficulty)
	if err != nil {
		return nil, err
	}

	var withdrawals *common.Hash
	if block.WithdrawalsRoot != "" {
		hash := common.HexToHash(block.WithdrawalsRoot)
		withdrawals = &hash
	}

	var baseFeePerGas *big.Int
	if block.BaseFeePerGas != "" {
		baseFeePerGas, err = HexToBigInt(block.BaseFeePerGas)
		if err != nil {
			return nil, err
		}
	}
	var blobGasUsed *uint64
	if block.BlobGasUsed != "" {
		blobGas, err := HexToUint64(block.BlobGasUsed)
		if err != nil {
			return nil, err
		}
		blobGasUsed = &blobGas
	}
	var excessBlobGas *uint64
	if block.ExcessBlobGas != "" {
		blobGas, err := HexToUint64(block.ExcessBlobGas)
		if err != nil {
			return nil, err
		}
		excessBlobGas = &blobGas
	}
	var parentBeaconRoot *common.Hash
	if block.ParentBeaconRoot != "" {
		hash := common.HexToHash(block.ParentBeaconRoot)
		parentBeaconRoot = &hash
	}

	header := &types.Header{
		ParentHash:       common.HexToHash(block.ParentHash),
		UncleHash:        common.HexToHash(block.Sha3Uncles),
		Coinbase:         common.HexToAddress(block.Miner),
		Root:             common.HexToHash(block.StateRoot),
		TxHash:           common.HexToHash(block.TransactionsRoot),
		ReceiptHash:      common.HexToHash(block.ReceiptsRoot),
		Bloom:            types.BytesToBloom(hexutil.MustDecode(block.LogsBloom)),
		Difficulty:       diffculty,
		Number:           number,
		GasLimit:         gaslimit,
		GasUsed:          gasUsed,
		Time:             ts,
		Extra:            hexutil.MustDecode(block.ExtraData),
		MixDigest:        common.HexToHash(block.MixHash),
		Nonce:            types.EncodeNonce(nonce),
		WithdrawalsHash:  withdrawals,
		BlobGasUsed:      blobGasUsed,
		ExcessBlobGas:    excessBlobGas,
		ParentBeaconRoot: parentBeaconRoot,
	}
	if baseFeePerGas != nil {
		header.BaseFee = baseFeePerGas
	}

	txs := make([]*types.Transaction, 0)
	for _, tx := range block.Transactions {
		nonce, err := HexToUint64(tx.Nonce)
		if err != nil {
			return nil, err
		}
		var toAddr *common.Address
		if tx.To != "" {
			addr := common.HexToAddress(tx.To)
			toAddr = &addr
		}
		val, err := HexToBigInt(tx.Value)
		if err != nil {
			return nil, err
		}
		gas, err := HexToUint64(tx.Gas)
		if err != nil {
			return nil, err
		}
		gasPrice, err := HexToBigInt(tx.GasPrice)
		if err != nil {
			return nil, err
		}
		v, err := HexToBigInt(tx.V)
		if err != nil {
			return nil, err
		}
		r, err := HexToBigInt(tx.R)
		if err != nil {
			return nil, err
		}
		s, err := HexToBigInt(tx.S)
		if err != nil {
			return nil, err
		}
		input := hexutil.MustDecode(tx.Input)
		switch tx.Type {
		case "0x0":
			// create a new transaction
			legacyTx := &types.LegacyTx{
				Nonce:    nonce,
				To:       toAddr,
				Value:    val,
				Gas:      gas,
				GasPrice: gasPrice,
				Data:     input,
				V:        v,
				R:        r,
				S:        s,
			}
			if toAddr != nil {
				legacyTx.To = toAddr
			}
			txn := types.NewTx(legacyTx)
			txs = append(txs, txn)
		case "0x1":
			chainId, err := HexToBigInt(tx.ChainId)
			if err != nil {
				return nil, err
			}
			var accessList types.AccessList
			for _, access := range tx.AccessList {
				var keys []common.Hash
				for _, key := range access.StorageKeys {
					storageKey := common.HexToHash(key)
					keys = append(keys, storageKey)
				}
				accessList = append(accessList, types.AccessTuple{
					Address:     common.HexToAddress(access.Address),
					StorageKeys: keys,
				})
			}
			txn := types.NewTx(&types.AccessListTx{
				ChainID:    chainId,
				Nonce:      nonce,
				GasPrice:   gasPrice,
				Gas:        gas,
				To:         toAddr,
				Value:      val,
				Data:       input,
				AccessList: accessList,
				V:          v,
				R:          r,
				S:          s,
			})
			txs = append(txs, txn)
		case "0x2":
			chainId, err := HexToBigInt(tx.ChainId)
			if err != nil {
				return nil, err
			}
			gasTipCap, err := HexToBigInt(tx.MaxPriorityFeePerGas)
			if err != nil {
				return nil, err
			}
			gasFeeCap, err := HexToBigInt(tx.MaxFeePerGas)
			if err != nil {
				return nil, err
			}
			var accessList types.AccessList
			for _, access := range tx.AccessList {
				var keys []common.Hash
				for _, key := range access.StorageKeys {
					storageKey := common.HexToHash(key)
					keys = append(keys, storageKey)
				}
				accessList = append(accessList, types.AccessTuple{
					Address:     common.HexToAddress(access.Address),
					StorageKeys: keys,
				})
			}
			txn := types.NewTx(&types.DynamicFeeTx{
				ChainID:    chainId,
				Nonce:      nonce,
				GasTipCap:  gasTipCap,
				GasFeeCap:  gasFeeCap,
				Gas:        gas,
				To:         toAddr,
				Value:      val,
				Data:       input,
				AccessList: accessList,
				V:          v,
				R:          r,
				S:          s,
			})
			txs = append(txs, txn)
		case "0x3":
			chainId, err := HexToUint64(tx.ChainId)
			if err != nil {
				return nil, err
			}
			gasTipCap, err := HexToUint64(tx.MaxPriorityFeePerGas)
			if err != nil {
				return nil, err
			}
			gasFeeCap, err := HexToUint64(tx.MaxFeePerGas)
			if err != nil {
				return nil, err
			}
			maxFeePerBlobGas, err := HexToBigInt(tx.MaxFeePerBlobGas)
			if err != nil {
				return nil, err
			}

			var accessList types.AccessList
			for _, access := range tx.AccessList {
				var keys []common.Hash
				for _, key := range access.StorageKeys {
					storageKey := common.HexToHash(key)
					keys = append(keys, storageKey)
				}
				accessList = append(accessList, types.AccessTuple{
					Address:     common.HexToAddress(access.Address),
					StorageKeys: keys,
				})
			}
			var blobHashes []common.Hash
			for _, blob := range tx.BlobVersionedHashes {
				blobHash := common.HexToHash(blob)
				blobHashes = append(blobHashes, blobHash)
			}
			transaction := types.NewTx(&types.BlobTx{
				ChainID:    uint256.NewInt(chainId),
				Nonce:      nonce,
				GasTipCap:  uint256.NewInt(gasTipCap),
				GasFeeCap:  uint256.NewInt(gasFeeCap),
				Gas:        gas,
				To:         *toAddr,
				Value:      uint256.NewInt(val.Uint64()),
				Data:       input,
				AccessList: accessList,
				V:          uint256.MustFromBig(v),
				R:          uint256.MustFromBig(r),
				S:          uint256.MustFromBig(s),
				BlobFeeCap: uint256.NewInt(maxFeePerBlobGas.Uint64()),
				BlobHashes: blobHashes,
			})
			txs = append(txs, transaction)
		}
	}
	newBlock := types.NewBlockWithHeader(header).WithBody(txs, nil)
	return &GeneralBlock{
		Block:           newBlock,
		TotalDifficulty: totalDifficulty,
	}, nil
}
