// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
)

const badBlockCacheExpire = 30 * time.Second

type BlockValidatorOption func(*BlockValidator) *BlockValidator

func EnableRemoteVerifyManager(remoteValidator *remoteVerifyManager) BlockValidatorOption {
	return func(bv *BlockValidator) *BlockValidator {
		bv.remoteValidator = remoteValidator
		return bv
	}
}

// BlockValidator is responsible for validating block headers, uncles and
// processed state.
//
// BlockValidator implements Validator.
type BlockValidator struct {
	config          *params.ChainConfig // Chain configuration options
	bc              *BlockChain         // Canonical block chain
	engine          consensus.Engine    // Consensus engine used for validating
	remoteValidator *remoteVerifyManager
}

// NewBlockValidator returns a new block validator which is safe for re-use
func NewBlockValidator(config *params.ChainConfig, blockchain *BlockChain, engine consensus.Engine, opts ...BlockValidatorOption) *BlockValidator {
	validator := &BlockValidator{
		config: config,
		engine: engine,
		bc:     blockchain,
	}

	for _, opt := range opts {
		validator = opt(validator)
	}

	return validator
}

// ValidateBody validates the given block's uncles and verifies the block
// header's transaction and uncle roots. The headers are assumed to be already
// validated at this point.
func (v *BlockValidator) ValidateBody(block *types.Block) error {
	// Check whether the block's known, and if not, that it's linkable
	if v.bc.HasBlockAndState(block.Hash(), block.NumberU64()) {
		return ErrKnownBlock
	}
	if v.bc.isCachedBadBlock(block) {
		return ErrKnownBadBlock
	}
	// Header validity is known at this point, check the uncles and transactions
	header := block.Header()
	if err := v.engine.VerifyUncles(v.bc, block); err != nil {
		return err
	}
	if hash := types.CalcUncleHash(block.Uncles()); hash != header.UncleHash {
		return fmt.Errorf("uncle root hash mismatch: have %x, want %x", hash, header.UncleHash)
	}

	validateFuns := []func() error{
		func() error {
			if hash := types.DeriveSha(block.Transactions(), trie.NewStackTrie(nil)); hash != header.TxHash {
				return fmt.Errorf("transaction root hash mismatch: have %x, want %x", hash, header.TxHash)
			}
			return nil
		},
		func() error {
			if !v.bc.HasBlockAndState(block.ParentHash(), block.NumberU64()-1) {
				if !v.bc.HasBlock(block.ParentHash(), block.NumberU64()-1) {
					return consensus.ErrUnknownAncestor
				}
				return consensus.ErrPrunedAncestor
			}
			return nil
		},
		func() error {
			if v.remoteValidator != nil && !v.remoteValidator.AncestorVerified(block.Header()) {
				return fmt.Errorf("%w, number: %s, hash: %s", ErrAncestorHasNotBeenVerified, block.Number(), block.Hash())
			}
			return nil
		},
	}
	validateRes := make(chan error, len(validateFuns))
	for _, f := range validateFuns {
		tmpFunc := f
		go func() {
			validateRes <- tmpFunc()
		}()
	}
	for i := 0; i < len(validateFuns); i++ {
		r := <-validateRes
		if r != nil {
			return r
		}
	}
	return nil
}

// ValidateState validates the various changes that happen after a state
// transition, such as amount of used gas, the receipt roots and the state root
// itself. ValidateState returns a database batch if the validation was a success
// otherwise nil and an error is returned.
func (v *BlockValidator) ValidateState(block *types.Block, statedb *state.StateDB, receipts types.Receipts, usedGas uint64) error {
	header := block.Header()
	if block.GasUsed() != usedGas {
		return fmt.Errorf("invalid gas used (remote: %d local: %d)", block.GasUsed(), usedGas)
	}
	// Validate the received block's bloom with the one derived from the generated receipts.
	// For valid blocks this should always validate to true.
	validateFuns := []func() error{
		func() error {
			rbloom := types.CreateBloom(receipts)
			if rbloom != header.Bloom {
				return fmt.Errorf("invalid bloom (remote: %x  local: %x)", header.Bloom, rbloom)
			}
			return nil
		},
		func() error {
			receiptSha := types.DeriveSha(receipts, trie.NewStackTrie(nil))
			if receiptSha != header.ReceiptHash {
				return fmt.Errorf("invalid receipt root hash (remote: %x local: %x)", header.ReceiptHash, receiptSha)
			}
			return nil
		},
	}
	if statedb.IsPipeCommit() {
		validateFuns = append(validateFuns, func() error {
			if err := statedb.WaitPipeVerification(); err != nil {
				return err
			}
			statedb.CorrectAccountsRoot(common.Hash{})
			statedb.Finalise(v.config.IsEIP158(header.Number))
			return nil
		})
	} else {
		validateFuns = append(validateFuns, func() error {
			if root := statedb.IntermediateRoot(v.config.IsEIP158(header.Number)); header.Root != root {
				return fmt.Errorf("invalid merkle root (remote: %x local: %x)", header.Root, root)
			}
			return nil
		})
	}
	validateRes := make(chan error, len(validateFuns))
	for _, f := range validateFuns {
		tmpFunc := f
		go func() {
			validateRes <- tmpFunc()
		}()
	}

	var err error
	for i := 0; i < len(validateFuns); i++ {
		r := <-validateRes
		if r != nil && err == nil {
			err = r
		}
	}
	return err
}

func (v *BlockValidator) RemoteVerifyManager() *remoteVerifyManager {
	return v.remoteValidator
}

// CalcGasLimit computes the gas limit of the next block after parent. It aims
// to keep the baseline gas close to the provided target, and increase it towards
// the target if the baseline gas is lower.
func CalcGasLimit(parentGasLimit, desiredLimit uint64) uint64 {
	delta := parentGasLimit/params.GasLimitBoundDivisor - 1
	limit := parentGasLimit
	if desiredLimit < params.MinGasLimit {
		desiredLimit = params.MinGasLimit
	}
	// If we're outside our allowed gas range, we try to hone towards them
	if limit < desiredLimit {
		limit = parentGasLimit + delta
		if limit > desiredLimit {
			limit = desiredLimit
		}
		return limit
	}
	if limit > desiredLimit {
		limit = parentGasLimit - delta
		if limit < desiredLimit {
			limit = desiredLimit
		}
	}
	return limit
}
