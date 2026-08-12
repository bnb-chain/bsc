// Copyright 2024 The go-ethereum Authors
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

package override

import (
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

// OverrideAccount indicates the overriding fields of account during the execution
// of a message call.
// Note, state and stateDiff can't be specified at the same time. If state is
// set, message execution will only use the data in the given state. Otherwise
// if stateDiff is set, all diff will be applied first and then execute the call
// message.
type OverrideAccount struct {
	Nonce            *hexutil.Uint64             `json:"nonce"`
	Code             *hexutil.Bytes              `json:"code"`
	Balance          *hexutil.Big                `json:"balance"`
	State            map[common.Hash]common.Hash `json:"state"`
	StateDiff        map[common.Hash]common.Hash `json:"stateDiff"`
	MovePrecompileTo *common.Address             `json:"movePrecompileToAddress"`
}

// StateOverride is the collection of overridden accounts.
type StateOverride map[common.Address]OverrideAccount

func (diff *StateOverride) has(address common.Address) bool {
	_, ok := (*diff)[address]
	return ok
}

// Apply overrides the fields of specified accounts into the given state.
func (diff *StateOverride) Apply(statedb *state.StateDB, precompiles vm.PrecompiledContracts) error {
	if diff == nil {
		return nil
	}
	// Iterate in deterministic order so error messages and behavior are stable (e.g. for tests).
	addrs := slices.SortedFunc(maps.Keys(*diff), common.Address.Cmp)

	// Tracks destinations of precompiles that were moved.
	dirtyAddrs := make(map[common.Address]struct{})
	for _, addr := range addrs {
		account := (*diff)[addr]
		// If a precompile was moved to this address already, it can't be overridden.
		if _, ok := dirtyAddrs[addr]; ok {
			return fmt.Errorf("account %s has already been overridden by a precompile", addr.Hex())
		}
		p, isPrecompile := precompiles[addr]
		// The MoveTo feature makes it possible to move a precompile
		// code to another address. If the target address is another precompile
		// the code for the latter is lost for this session.
		// Note the destination account is not cleared upon move.
		if account.MovePrecompileTo != nil {
			if !isPrecompile {
				return fmt.Errorf("account %s is not a precompile", addr.Hex())
			}
			// Refuse to move a precompile to an address that has been
			// or will be overridden.
			if diff.has(*account.MovePrecompileTo) {
				return fmt.Errorf("account %s is already overridden", account.MovePrecompileTo.Hex())
			}
			precompiles[*account.MovePrecompileTo] = p
			dirtyAddrs[*account.MovePrecompileTo] = struct{}{}
		}
		if isPrecompile {
			delete(precompiles, addr)
		}
		// Override account nonce.
		if account.Nonce != nil {
			statedb.SetNonce(addr, uint64(*account.Nonce), tracing.NonceChangeUnspecified)
		}
		// Override account(contract) code.
		if account.Code != nil {
			statedb.SetCode(addr, *account.Code, tracing.CodeChangeUnspecified)
		}
		// Override account balance.
		if account.Balance != nil {
			u256Balance, _ := uint256.FromBig((*big.Int)(account.Balance))
			statedb.SetBalance(addr, u256Balance, tracing.BalanceChangeUnspecified)
		}
		if account.State != nil && account.StateDiff != nil {
			return fmt.Errorf("account %s has both 'state' and 'stateDiff'", addr.Hex())
		}
		// Replace entire state if caller requires.
		if account.State != nil {
			statedb.SetStorage(addr, account.State)
		}
		// Apply state diff into specified accounts.
		if account.StateDiff != nil {
			for key, value := range account.StateDiff {
				statedb.SetState(addr, key, value)
			}
		}
	}
	// Now finalize the changes. Finalize is normally performed between transactions.
	// By using finalize, the overrides are semantically behaving as
	// if they were created in a transaction just before the tracing occur.
	statedb.Finalise(false)
	return nil
}

// BlockOverrides is a set of header fields to override.
type BlockOverrides struct {
	Number       *hexutil.Big
	Difficulty   *hexutil.Big // No-op if we're simulating post-merge calls.
	Time         *hexutil.Uint64
	GasLimit     *hexutil.Uint64
	FeeRecipient *common.Address
	// PrevRandao maps to the header's MixDigest, which BSC repurposes as the
	// millisecond remainder of the block timestamp (BEP-520): the value must
	// be less than 1000 and is assembled into the millisecond timestamp
	// reported by the BEP-706 precompile (time*1000 + prevRandao). Do NOT
	// pass arbitrary random values on this client.
	PrevRandao    *common.Hash
	BaseFeePerGas *hexutil.Big
	BlobBaseFee   *hexutil.Big
	BeaconRoot    *common.Hash
	Withdrawals   *types.Withdrawals
}

// MaxBSCMilliRemainder bounds the value accepted for the "prevRandao" block
// override: on BSC the underlying header field (MixDigest) carries the
// sub-second millisecond remainder of the block timestamp (BEP-520) instead
// of a randomness beacon. Consensus enforces the same bound on real headers
// (MilliTimestamp()/1000 must equal Time).
const MaxBSCMilliRemainder = 1000

// BSCMilliRemainder interprets a "prevRandao" override the BSC way: the
// 32-byte value is the millisecond remainder and must be below
// MaxBSCMilliRemainder, exactly like the MixDigest of a real BSC header.
func BSCMilliRemainder(prevRandao *common.Hash) (uint64, error) {
	ms := new(big.Int).SetBytes(prevRandao[:])
	if ms.Cmp(big.NewInt(MaxBSCMilliRemainder)) >= 0 {
		return 0, fmt.Errorf(`block override "prevRandao" on BSC carries the millisecond remainder of the block timestamp (BEP-520/BEP-706) and must be less than %d, got %s`, MaxBSCMilliRemainder, ms)
	}
	return ms.Uint64(), nil
}

// Apply overrides the given header fields into the given block context.
//
// This is the BSC client, so the "prevRandao" override follows the BSC
// header semantics unconditionally: BSC headers repurpose MixDigest as the
// millisecond remainder of the block timestamp (BEP-520), so the override is
// assembled into the millisecond timestamp consumed by the BEP-706
// precompile — time*1000 + prevRandao — and rejected when it is not a valid
// remainder (>= 1000, mirroring the consensus rule on real headers). The
// value still replaces blockCtx.Random as well. Callers must NOT pass
// arbitrary random values for this field on this client.
func (o *BlockOverrides) Apply(blockCtx *vm.BlockContext) error {
	if o == nil {
		return nil
	}
	if o.BeaconRoot != nil {
		return errors.New(`block override "beaconRoot" is not supported for this RPC method`)
	}
	if o.Withdrawals != nil {
		return errors.New(`block override "withdrawals" is not supported for this RPC method`)
	}
	if o.Number != nil {
		blockCtx.BlockNumber = o.Number.ToInt()
	}
	if o.Difficulty != nil {
		blockCtx.Difficulty = o.Difficulty.ToInt()
	}
	if o.Time != nil {
		blockCtx.Time = uint64(*o.Time)
		// Keep the millisecond timestamp (consumed by the BEP-706 precompile
		// on BSC) consistent with the overridden time. The remainder starts
		// at .000 and can be set through the prevRandao override below.
		// The field is inert on non-BSC chains, so no gate is needed.
		blockCtx.MilliTimestamp = uint64(*o.Time) * 1000
	}
	if o.GasLimit != nil {
		blockCtx.GasLimit = uint64(*o.GasLimit)
	}
	if o.FeeRecipient != nil {
		blockCtx.Coinbase = *o.FeeRecipient
	}
	if o.PrevRandao != nil {
		// prevRandao is the millisecond remainder on BSC — validate it and
		// assemble it into the millisecond timestamp on top of the (possibly
		// overridden) seconds.
		ms, err := BSCMilliRemainder(o.PrevRandao)
		if err != nil {
			return err
		}
		blockCtx.MilliTimestamp = blockCtx.Time*1000 + ms
		blockCtx.Random = o.PrevRandao
	}
	if o.BaseFeePerGas != nil {
		blockCtx.BaseFee = o.BaseFeePerGas.ToInt()
	}
	if o.BlobBaseFee != nil {
		blockCtx.BlobBaseFee = o.BlobBaseFee.ToInt()
	}
	return nil
}

// MakeHeader returns a new header object with the overridden
// fields.
// Note: MakeHeader ignores BlobBaseFee if set. That's because
// header has no such field.
func (o *BlockOverrides) MakeHeader(header *types.Header) *types.Header {
	if o == nil {
		return header
	}
	h := types.CopyHeader(header)
	if o.Number != nil {
		h.Number = o.Number.ToInt()
	}
	if o.Difficulty != nil {
		h.Difficulty = o.Difficulty.ToInt()
	}
	if o.Time != nil {
		h.Time = uint64(*o.Time)
	}
	if o.GasLimit != nil {
		h.GasLimit = uint64(*o.GasLimit)
	}
	if o.FeeRecipient != nil {
		h.Coinbase = *o.FeeRecipient
	}
	if o.PrevRandao != nil {
		h.MixDigest = *o.PrevRandao
	}
	if o.BaseFeePerGas != nil {
		h.BaseFee = o.BaseFeePerGas.ToInt()
	}
	return h
}
