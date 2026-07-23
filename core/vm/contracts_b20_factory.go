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
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// B20Factory: the singleton createB20 entry point plus the address-prediction
// and identity views. Token creation writes the initial core storage and an
// initialization marker, then runs initCalls in a privileged bootstrap window
// (role and transfer-side policy gates skipped) so a token can be fully
// configured — roles granted, initial supply minted — in one transaction.
//
// TODO: this uses an internal calldata layout for createB20 and does not yet
// carry name/symbol/decimals/currency params, an ActivationRegistry gate, or
// the B20Created event. Align the external ABI, params tuple, address preimage
// and event with base-std before golden testing.

var (
	selCreateB20        = selector("createB20(uint8,bytes32,address,bytes[])")
	selGetB20Address    = selector("getB20Address(uint8,address,bytes32)")
	selIsB20            = selector("isB20(address)")
	selIsB20Initialized = selector("isB20Initialized(address)")
)

// b20MarkerCode is written to a token address on creation. It is never
// executed (the precompile takes precedence in Call) — it only marks the
// account as an initialized B20 token for resolution and isB20Initialized.
var b20MarkerCode = []byte{0xB2, 0x00}

// b20NoSupplyCap is the "unlimited" sentinel: type(uint128).max.
var b20NoSupplyCap = new(uint256.Int).Sub(new(uint256.Int).Lsh(uint256.NewInt(1), 128), uint256.NewInt(1))

// b20DeriveAddress computes a token's deterministic address:
// 0xb2 ++ 9×0x00 ++ variant ++ keccak256(creator ++ salt)[:9].
//
// TODO: verify the preimage (encoding/ordering of creator and salt) against
// base-std; routing does not depend on it, but cross-client determinism does.
func b20DeriveAddress(variant byte, creator common.Address, salt common.Hash) common.Address {
	h := crypto.Keccak256(creator.Bytes(), salt.Bytes())
	var a common.Address
	a[0] = b20MagicPrefix
	a[10] = variant
	copy(a[11:20], h[:9])
	return a
}

func runB20Factory(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, ErrExecutionReverted
	}
	var sel [4]byte
	copy(sel[:], input[:4])
	args := input[4:]

	switch sel {
	case selGetB20Address:
		variant, err := readWord(args, 0)
		if err != nil {
			return nil, err
		}
		sender, err := readAddress(args, 1)
		if err != nil {
			return nil, err
		}
		salt, err := readWord(args, 2)
		if err != nil {
			return nil, err
		}
		addr := b20DeriveAddress(variant[31], sender, salt)
		return addrKey(addr).Bytes(), nil
	case selIsB20:
		a, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		return encBool(IsB20Address(a)), nil
	case selIsB20Initialized:
		a, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		return encBool(IsB20Address(a) && b20Initialized(ctx.StateDB, a)), nil
	case selCreateB20:
		return createB20(ctx, args)
	}
	return nil, ErrExecutionReverted
}

func createB20(ctx *PrecompileContext, args []byte) ([]byte, error) {
	if ctx.ReadOnly {
		return nil, ErrWriteProtection
	}
	variantWord, err := readWord(args, 0)
	if err != nil {
		return nil, err
	}
	salt, err := readWord(args, 1)
	if err != nil {
		return nil, err
	}
	initialAdmin, err := readAddress(args, 2)
	if err != nil {
		return nil, err
	}
	initCalls, err := readBytesArray(args, 3)
	if err != nil {
		return nil, err
	}

	variant := variantWord[31]
	if variant != b20VariantAsset && variant != b20VariantStablecoin {
		return nil, ErrExecutionReverted
	}
	creator := ctx.Caller
	addr := b20DeriveAddress(variant, creator, salt)

	// TODO: ActivationRegistry feature gate for the variant (P3).
	if b20Initialized(ctx.StateDB, addr) {
		return nil, ErrExecutionReverted // AlreadyExists
	}
	ctx.StateDB.SetCode(addr, b20MarkerCode, tracing.CodeChangeContractCreation)

	// Bootstrap context/token bound to the new address, privileged so initCalls
	// can grant roles and mint before any role holder exists.
	decimals := byte(18) // TODO: Asset decimals param -> extension storage.
	if variant == b20VariantStablecoin {
		decimals = 6
	}
	tokenCtx := &PrecompileContext{
		evm:        ctx.evm,
		StateDB:    ctx.StateDB,
		Self:       addr,
		Caller:     creator,
		DirectCall: true,
		Rules:      ctx.Rules,
		gas:        ctx.gas,
	}
	tok := newB20TokenBootstrap(tokenCtx, decimals)

	// Initial state: no supply cap; initialAdmin (if any) is the first admin.
	tok.s.setSupplyCap(b20NoSupplyCap)
	if initialAdmin != (common.Address{}) {
		tok.s.setRole(roleDefaultAdmin, initialAdmin, true)
		tok.s.setAdminCount(uint256.NewInt(1))
		tokenCtx.AddLog([]common.Hash{b20TopicRoleGranted, roleDefaultAdmin, addrKey(initialAdmin), addrKey(creator)}, nil)
	}

	// Privileged bootstrap: any initCall failure reverts the whole creation.
	for _, call := range initCalls {
		if _, err := tok.dispatch(call); err != nil {
			return nil, err
		}
	}
	if ctx.OutOfGas() {
		return nil, ErrOutOfGas
	}
	// TODO: emit B20Created(token, variant, creator) once the signature is fixed.
	return addrKey(addr).Bytes(), nil
}

// readBytesArray decodes an ABI `bytes[]` argument at head word argIndex.
func readBytesArray(args []byte, argIndex int) ([][]byte, error) {
	L := uint64(len(args))
	base, ok := wordU64(args, uint64(argIndex)*32)
	if !ok || base > L || L-base < 32 {
		return nil, ErrExecutionReverted
	}
	n, _ := wordU64(args, base)
	arrData := base + 32
	if n > (L-arrData)/32 {
		return nil, ErrExecutionReverted
	}
	out := make([][]byte, n)
	for i := uint64(0); i < n; i++ {
		elemOff, _ := wordU64(args, arrData+i*32)
		pos := arrData + elemOff
		if elemOff > L-arrData || pos > L || L-pos < 32 {
			return nil, ErrExecutionReverted
		}
		elemLen, _ := wordU64(args, pos)
		start := pos + 32
		if elemLen > L-start {
			return nil, ErrExecutionReverted
		}
		out[i] = args[start : start+elemLen]
	}
	return out, nil
}

// wordU64 reads the 32-byte word at byte position pos as a uint64 (truncating
// the high bits), reporting false when the word is out of range.
func wordU64(args []byte, pos uint64) (uint64, bool) {
	if pos > uint64(len(args)) || uint64(len(args))-pos < 32 {
		return 0, false
	}
	return new(uint256.Int).SetBytes(args[pos : pos+32]).Uint64(), true
}
