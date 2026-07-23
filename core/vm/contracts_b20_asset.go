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

package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// B20 Asset (RWA) variant extensions: the net-asset-value multiplier (rebase),
// decimals held in extension storage, batch minting and the OPERATOR role.
// announce/extraMetadata are follow-ups.
//
// TODO: verify the extension namespace and decimals param against base-std;
// decimals defaults to 18 until createB20 carries the param.

const b20AssetNamespace = "base.b20.asset"

const (
	b20AssetSlotDecimals   = 0
	b20AssetSlotMultiplier = 1
)

var (
	b20AssetRoot = erc7201Root(b20AssetNamespace)
	// b20WAD is the multiplier fixed-point base (1e18 = 1.0x).
	b20WAD = uint256.NewInt(1_000_000_000_000_000_000)

	roleOperator = crypto.Keccak256Hash([]byte("OPERATOR_ROLE"))

	selMultiplier       = selector("multiplier()")
	selWadPrecision     = selector("WAD_PRECISION()")
	selScaledBalanceOf  = selector("scaledBalanceOf(address)")
	selToScaledBalance  = selector("toScaledBalance(uint256)")
	selToRawBalance     = selector("toRawBalance(uint256)")
	selUpdateMultiplier = selector("updateMultiplier(uint256)")
	selOperatorRole     = selector("OPERATOR_ROLE()")
	selBatchMint        = selector("batchMint(address[],uint256[])")

	b20TopicMultiplierUpdated = crypto.Keccak256Hash([]byte("MultiplierUpdated(uint256)"))
)

// assetExt is a gas-metered view over the Asset extension storage.
type assetExt struct{ s b20Storage }

func newAssetExt(ctx *PrecompileContext) assetExt { return assetExt{s: newMeteredB20Storage(ctx)} }

func assetSlot(offset uint64) common.Hash {
	x := new(uint256.Int).SetBytes(b20AssetRoot.Bytes())
	x.AddUint64(x, offset)
	return common.Hash(x.Bytes32())
}

func (e assetExt) decimals() uint8 {
	return uint8(new(uint256.Int).SetBytes(e.s.getWord(assetSlot(b20AssetSlotDecimals)).Bytes()).Uint64())
}
func (e assetExt) setDecimals(d uint8) {
	e.s.setWord(assetSlot(b20AssetSlotDecimals), common.Hash(uint256.NewInt(uint64(d)).Bytes32()))
}
func (e assetExt) multiplier() *uint256.Int {
	return new(uint256.Int).SetBytes(e.s.getWord(assetSlot(b20AssetSlotMultiplier)).Bytes())
}
func (e assetExt) setMultiplier(m *uint256.Int) {
	e.s.setWord(assetSlot(b20AssetSlotMultiplier), common.Hash(m.Bytes32()))
}

// initAssetExtension seeds a new Asset token's extension storage.
func initAssetExtension(ctx *PrecompileContext) {
	e := newAssetExt(ctx)
	e.setDecimals(18) // TODO: from createB20 decimals param.
	e.setMultiplier(b20WAD)
}

func applyMultiplier(raw, mul *uint256.Int) *uint256.Int {
	return new(uint256.Int).Div(new(uint256.Int).Mul(raw, mul), b20WAD)
}

func removeMultiplier(scaled, mul *uint256.Int) *uint256.Int {
	if mul.IsZero() {
		return new(uint256.Int)
	}
	return new(uint256.Int).Div(new(uint256.Int).Mul(scaled, b20WAD), mul)
}

// dispatchAsset handles the Asset-variant selectors, returning ok=false so the
// caller falls back to the shared IB20 dispatch.
func dispatchAsset(tok b20Token, ext assetExt, input []byte) (ret []byte, err error, ok bool) {
	if len(input) < 4 {
		return nil, nil, false
	}
	var sel [4]byte
	copy(sel[:], input[:4])
	args := input[4:]

	switch sel {
	case selDecimals:
		return encU256(uint256.NewInt(uint64(ext.decimals()))), nil, true
	case selMultiplier:
		return encU256(ext.multiplier()), nil, true
	case selWadPrecision:
		return encU256(b20WAD), nil, true
	case selOperatorRole:
		return roleOperator.Bytes(), nil, true
	case selScaledBalanceOf:
		a, err := readAddress(args, 0)
		if err != nil {
			return nil, err, true
		}
		return encU256(applyMultiplier(tok.s.balanceOf(a), ext.multiplier())), nil, true
	case selToScaledBalance:
		raw, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		return encU256(applyMultiplier(raw, ext.multiplier())), nil, true
	case selToRawBalance:
		scaled, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		return encU256(removeMultiplier(scaled, ext.multiplier())), nil, true
	case selUpdateMultiplier:
		m, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		return nil, updateMultiplier(tok, ext, m), true
	case selBatchMint:
		return nil, batchMint(tok, args), true
	}
	return nil, nil, false
}

func updateMultiplier(tok b20Token, ext assetExt, newMul *uint256.Int) error {
	if tok.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if err := tok.ensureRole(roleOperator); err != nil {
		return err
	}
	if newMul.IsZero() {
		return ErrExecutionReverted // InvalidMultiplier
	}
	ext.setMultiplier(newMul)
	mb := newMul.Bytes32()
	tok.ctx.AddLog([]common.Hash{b20TopicMultiplierUpdated}, mb[:])
	return nil
}

func batchMint(tok b20Token, args []byte) error {
	if tok.ctx.ReadOnly {
		return ErrWriteProtection
	}
	if tok.isPaused(b20PauseMint) {
		return ErrExecutionReverted
	}
	if err := tok.ensureRole(roleMint); err != nil {
		return err
	}
	recipients, err := readWordArray(args, 0)
	if err != nil {
		return err
	}
	amounts, err := readWordArray(args, 1)
	if err != nil {
		return err
	}
	if len(recipients) == 0 || len(recipients) != len(amounts) {
		return ErrExecutionReverted // LengthMismatch / empty
	}
	for i := range recipients {
		to := common.BytesToAddress(recipients[i].Bytes())
		amount := new(uint256.Int).SetBytes(amounts[i].Bytes())
		if err := tok.mintCore(to, amount); err != nil {
			return err
		}
	}
	return nil
}

// readWordArray decodes an ABI dynamic array of 32-byte words (address[] /
// uint256[]) at head word argIndex.
func readWordArray(args []byte, argIndex int) ([]common.Hash, error) {
	L := uint64(len(args))
	base, ok := wordU64(args, uint64(argIndex)*32)
	if !ok || base > L || L-base < 32 {
		return nil, ErrExecutionReverted
	}
	n, _ := wordU64(args, base)
	dataPos := base + 32
	if n > (L-dataPos)/32 {
		return nil, ErrExecutionReverted
	}
	out := make([]common.Hash, n)
	for i := uint64(0); i < n; i++ {
		out[i] = common.BytesToHash(args[dataPos+i*32 : dataPos+i*32+32])
	}
	return out, nil
}
