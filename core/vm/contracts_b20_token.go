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

// b20Token is the shared IB20 logic both variants delegate to. It wraps a
// gas-metered core-storage view and the call context. Variant-specific
// selectors (Asset's multiplier/announce/…, Stablecoin's currency) are
// layered on top by the variant precompiles.
//
// Scope of this layer: the ERC-20 core (name/symbol/decimals/totalSupply/
// balanceOf/allowance/approve/transfer/transferFrom) plus the TRANSFER pause
// gate. Roles, mint/burn, compliance policies and permit are follow-ups.
type b20Token struct {
	ctx      *PrecompileContext
	s        b20Storage
	decimals uint8

	// privileged is set on the factory's bootstrap path, where role and
	// transfer-side policy gates are skipped (anti-revival and MINT_RECEIVER
	// checks are still enforced). Always false for ordinary calls.
	privileged bool
}

func newB20Token(ctx *PrecompileContext, decimals uint8) b20Token {
	return b20Token{ctx: ctx, s: newMeteredB20Storage(ctx), decimals: decimals}
}

// newB20TokenBootstrap returns a token in the factory's privileged bootstrap
// mode, where role and transfer-side policy gates are skipped.
func newB20TokenBootstrap(ctx *PrecompileContext, decimals uint8) b20Token {
	t := newB20Token(ctx, decimals)
	t.privileged = true
	return t
}

// pause feature bits in the paused bitmask (slot 11).
const (
	b20PauseTransfer = 0
	b20PauseMint     = 1
	b20PauseBurn     = 2
	b20PauseSeize    = 3
)

func selector(sig string) (s [4]byte) {
	copy(s[:], crypto.Keccak256([]byte(sig)))
	return s
}

var (
	selName         = selector("name()")
	selSymbol       = selector("symbol()")
	selDecimals     = selector("decimals()")
	selTotalSupply  = selector("totalSupply()")
	selBalanceOf    = selector("balanceOf(address)")
	selAllowance    = selector("allowance(address,address)")
	selApprove      = selector("approve(address,uint256)")
	selTransfer     = selector("transfer(address,uint256)")
	selTransferFrom = selector("transferFrom(address,address,uint256)")

	b20TopicTransfer = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	b20TopicApproval = crypto.Keccak256Hash([]byte("Approval(address,address,uint256)"))

	maxU256 = new(uint256.Int).Not(new(uint256.Int))
)

// dispatch routes a call by selector. It returns the ABI-encoded result on
// success. Business-rule failures and unknown selectors revert
// (ErrExecutionReverted); a write reached in a read-only frame throws
// (ErrWriteProtection), matching SSTORE-in-STATICCALL semantics.
func (t b20Token) dispatch(input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, ErrExecutionReverted
	}
	var sel [4]byte
	copy(sel[:], input[:4])
	args := input[4:]

	switch sel {
	case selName:
		return encString(t.s.name()), nil
	case selSymbol:
		return encString(t.s.symbol()), nil
	case selDecimals:
		return encU256(uint256.NewInt(uint64(t.decimals))), nil
	case selTotalSupply:
		return encU256(t.s.totalSupply()), nil
	case selBalanceOf:
		a, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		return encU256(t.s.balanceOf(a)), nil
	case selAllowance:
		owner, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		spender, err := readAddress(args, 1)
		if err != nil {
			return nil, err
		}
		return encU256(t.s.allowance(owner, spender)), nil
	case selApprove:
		spender, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		amount, err := readU256(args, 1)
		if err != nil {
			return nil, err
		}
		return t.approve(t.ctx.Caller, spender, amount)
	case selTransfer:
		to, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		amount, err := readU256(args, 1)
		if err != nil {
			return nil, err
		}
		return t.transfer(t.ctx.Caller, to, amount)
	case selTransferFrom:
		from, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		to, err := readAddress(args, 1)
		if err != nil {
			return nil, err
		}
		amount, err := readU256(args, 2)
		if err != nil {
			return nil, err
		}
		return t.transferFrom(t.ctx.Caller, from, to, amount)
	}
	// RBAC / pause / mint-burn / configurable selectors.
	if ret, err, ok := t.dispatchAdmin(sel, args); ok {
		return ret, err
	}
	// permit (EIP-2612) and the *WithMemo family.
	if ret, err, ok := t.dispatchPermitMemo(sel, args); ok {
		return ret, err
	}
	return nil, ErrExecutionReverted
}

// --- ERC-20 core ------------------------------------------------------------

func (t b20Token) approve(owner, spender common.Address, amount *uint256.Int) ([]byte, error) {
	if t.ctx.ReadOnly {
		return nil, ErrWriteProtection
	}
	// approve is intentionally not gated by pause or policy.
	t.s.setAllowance(owner, spender, amount)
	t.emit(b20TopicApproval, owner, spender, amount)
	return encBool(true), nil
}

func (t b20Token) transfer(from, to common.Address, amount *uint256.Int) ([]byte, error) {
	if t.ctx.ReadOnly {
		return nil, ErrWriteProtection
	}
	if t.isPaused(b20PauseTransfer) {
		return nil, ErrExecutionReverted
	}
	if err := t.move(from, to, amount); err != nil {
		return nil, err
	}
	t.emit(b20TopicTransfer, from, to, amount)
	return encBool(true), nil
}

func (t b20Token) transferFrom(spender, from, to common.Address, amount *uint256.Int) ([]byte, error) {
	if t.ctx.ReadOnly {
		return nil, ErrWriteProtection
	}
	if t.isPaused(b20PauseTransfer) {
		return nil, ErrExecutionReverted
	}
	// Spend allowance unless the caller is the owner. U256::MAX is treated as
	// an infinite, non-decreasing allowance.
	if spender != from {
		if !t.privileged && !t.policyAllows(t.s.transferExecutorPolicy(), spender) {
			return nil, ErrExecutionReverted
		}
		allowed := t.s.allowance(from, spender)
		if !allowed.Eq(maxU256) {
			if allowed.Lt(amount) {
				return nil, ErrExecutionReverted
			}
			t.s.setAllowance(from, spender, new(uint256.Int).Sub(allowed, amount))
		}
	}
	if err := t.move(from, to, amount); err != nil {
		return nil, err
	}
	t.emit(b20TopicTransfer, from, to, amount)
	return encBool(true), nil
}

// policyAllows reads the PolicyRegistry directly (no cross-contract CALL) to
// decide whether account may be operated under the given policy id. Id 0
// (ALWAYS_ALLOW) short-circuits to true.
func (t b20Token) policyAllows(id uint64, account common.Address) bool {
	if id == 0 {
		return true
	}
	return newPolicyReg(t.ctx).isAuthorized(id, account)
}

// move debits from and credits to, reverting on insufficient balance. In a
// consistent token the balance sum equals totalSupply, so the credit cannot
// overflow.
func (t b20Token) move(from, to common.Address, amount *uint256.Int) error {
	// TRANSFER_SENDER / TRANSFER_RECEIVER compliance (skipped when privileged).
	if !t.privileged {
		if !t.policyAllows(t.s.transferSenderPolicy(), from) ||
			!t.policyAllows(t.s.transferReceiverPolicy(), to) {
			return ErrExecutionReverted
		}
	}
	bal := t.s.balanceOf(from)
	if bal.Lt(amount) {
		return ErrExecutionReverted
	}
	t.s.setBalance(from, new(uint256.Int).Sub(bal, amount))
	t.s.setBalance(to, new(uint256.Int).Add(t.s.balanceOf(to), amount))
	return nil
}

func (t b20Token) isPaused(bit uint) bool {
	return new(uint256.Int).Rsh(t.s.paused(), bit).Uint64()&1 == 1
}

// emit logs an indexed (from/owner, to/spender, value) event.
func (t b20Token) emit(topic0 common.Hash, a, b common.Address, value *uint256.Int) {
	v := value.Bytes32()
	t.ctx.AddLog([]common.Hash{topic0, addrKey(a), addrKey(b)}, v[:])
}

// --- ABI helpers ------------------------------------------------------------

func readWord(args []byte, i int) (common.Hash, error) {
	off := i * 32
	if len(args) < off+32 {
		return common.Hash{}, ErrExecutionReverted
	}
	return common.BytesToHash(args[off : off+32]), nil
}

func readAddress(args []byte, i int) (common.Address, error) {
	w, err := readWord(args, i)
	if err != nil {
		return common.Address{}, err
	}
	return common.BytesToAddress(w.Bytes()), nil
}

func readU256(args []byte, i int) (*uint256.Int, error) {
	w, err := readWord(args, i)
	if err != nil {
		return nil, err
	}
	return new(uint256.Int).SetBytes(w.Bytes()), nil
}

func encU256(v *uint256.Int) []byte {
	b := v.Bytes32()
	return b[:]
}

func encBool(b bool) []byte {
	out := make([]byte, 32)
	if b {
		out[31] = 1
	}
	return out
}

// encString ABI-encodes a string: head offset (0x20), length word, then the
// data right-padded to a 32-byte boundary.
func encString(s string) []byte {
	data := []byte(s)
	padded := (len(data) + 31) / 32 * 32
	out := make([]byte, 64+padded)
	out[31] = 0x20
	lenWord := uint256.NewInt(uint64(len(data))).Bytes32()
	copy(out[32:64], lenWord[:])
	copy(out[64:], data)
	return out
}
