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
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// Typed revert data for the B20 precompiles (BEP-702). Business-rule failures
// revert with an ABI-encoded error — selector ++ arguments — exactly as a
// Solidity `revert CustomError(...)` would, so integrators decode one error
// surface whether a token is native or a contract.
//
// Inside the B20 code a failure is a *b20RevertError carrying the encoded
// payload; finishB20 converts it at the precompile boundary into the
// (returndata, ErrExecutionReverted) pair the EVM call path already knows how
// to propagate. ABI *decode* failures (malformed calldata) revert with empty
// returndata, mirroring base-std's dispatch-level AbiDecodeFailed.

// b20SigRegistry accumulates every function / event / error signature the B20
// implementation registers, for the ABI-baseline conformance test.
var (
	b20FnSigs    = map[string][4]byte{}
	b20EventSigs = map[string]common.Hash{}
	b20ErrSigs   = map[string][4]byte{}
)

// eventTopic computes (and registers) topic0 for an event signature.
func eventTopic(sig string) common.Hash {
	h := crypto.Keccak256Hash([]byte(sig))
	b20EventSigs[sig] = h
	return h
}

// b20ErrorSel computes (and registers) the 4-byte selector of an error signature.
func b20ErrorSel(sig string) [4]byte {
	var s [4]byte
	copy(s[:], crypto.Keccak256([]byte(sig)))
	b20ErrSigs[sig] = s
	return s
}

// b20RevertError carries an ABI-encoded revert payload through the internal
// call chain. It is never returned to the EVM directly; finishB20 converts it.
type b20RevertError struct {
	sig  string
	data []byte
}

func (e *b20RevertError) Error() string { return fmt.Sprintf("b20 revert: %s", e.sig) }

// Is lets errors.Is(err, ErrExecutionReverted) hold before finishB20 converts
// the typed revert at the precompile boundary.
func (e *b20RevertError) Is(target error) bool { return target == ErrExecutionReverted }

// finishB20 converts a typed revert into (returndata, ErrExecutionReverted) at
// the precompile boundary. All other results pass through unchanged.
func finishB20(ret []byte, err error) ([]byte, error) {
	var rev *b20RevertError
	if errors.As(err, &rev) {
		return rev.data, ErrExecutionReverted
	}
	return ret, err
}

// revB20 builds a typed revert from a registered selector and static 32-byte words.
func revB20(sig string, sel [4]byte, words ...common.Hash) error {
	data := make([]byte, 4+32*len(words))
	copy(data, sel[:])
	for i := range words {
		copy(data[4+i*32:], words[i][:])
	}
	return &b20RevertError{sig: sig, data: data}
}

// revB20Bytes builds a typed revert for an error with a single dynamic
// `bytes`/`string` argument.
func revB20Bytes(sig string, sel [4]byte, payload []byte) error {
	padded := (len(payload) + 31) / 32 * 32
	data := make([]byte, 4+64+padded)
	copy(data, sel[:])
	data[4+31] = 0x20
	l := uint256.NewInt(uint64(len(payload))).Bytes32()
	copy(data[4+32:4+64], l[:])
	copy(data[4+64:], payload)
	return &b20RevertError{sig: sig, data: data}
}

// Word-building helpers.
func wU256(v *uint256.Int) common.Hash { return common.Hash(v.Bytes32()) }
func wU64(v uint64) common.Hash        { return common.Hash(uint256.NewInt(v).Bytes32()) }
func wU8(v byte) common.Hash           { var h common.Hash; h[31] = v; return h }

// Registered error selectors (BEP-702 error surface, aligned with base-std).
var (
	errSelNonPayable          = b20ErrorSel("NonPayable()")
	errSelInvalidReceiver     = b20ErrorSel("InvalidReceiver(address)")
	errSelInvalidSender       = b20ErrorSel("InvalidSender(address)")
	errSelInvalidSpender      = b20ErrorSel("InvalidSpender(address)")
	errSelInvalidApprover     = b20ErrorSel("InvalidApprover(address)")
	errSelInsufficientBalance = b20ErrorSel("InsufficientBalance(address,uint256,uint256)")
	errSelInsufficientAllow   = b20ErrorSel("InsufficientAllowance(address,uint256,uint256)")
	errSelSupplyCapExceeded   = b20ErrorSel("SupplyCapExceeded(uint256,uint256)")
	errSelInvalidSupplyCap    = b20ErrorSel("InvalidSupplyCap(uint256,uint256)")
	errSelContractPaused      = b20ErrorSel("ContractPaused(uint8)")
	errSelExpiredSignature    = b20ErrorSel("ExpiredSignature(uint256)")
	errSelInvalidSigner       = b20ErrorSel("InvalidSigner(address,address)")
	errSelACUnauthorized      = b20ErrorSel("AccessControlUnauthorizedAccount(address,bytes32)")
	errSelACBadConfirmation   = b20ErrorSel("AccessControlBadConfirmation()")
	errSelLastAdminRenounce   = b20ErrorSel("LastAdminCannotRenounce()")
	errSelNotSoleAdmin        = b20ErrorSel("NotSoleAdmin()")
	errSelPolicyForbids       = b20ErrorSel("PolicyForbids(bytes32,uint64)")
	errSelPolicyNotFound      = b20ErrorSel("PolicyNotFound()")
	errSelUnsupportedScope    = b20ErrorSel("UnsupportedPolicyType(bytes32)")
	errSelIncompatibleType    = b20ErrorSel("IncompatiblePolicyType()")
	errSelBatchTooLarge       = b20ErrorSel("BatchSizeTooLarge(uint256)")
	errSelEmptyBatch          = b20ErrorSel("EmptyBatch()")
	errSelLengthMismatch      = b20ErrorSel("LengthMismatch(uint256,uint256)")
	errSelEmptyFeatureSet     = b20ErrorSel("EmptyFeatureSet()")
	errSelInvalidMultiplier   = b20ErrorSel("InvalidMultiplier()")
	errSelInvalidMetadataKey  = b20ErrorSel("InvalidMetadataKey()")
	errSelAnnounceInProgress  = b20ErrorSel("AnnouncementInProgress()")
	errSelAnnounceIdUsed      = b20ErrorSel("AnnouncementIdAlreadyUsed(uint256)")
	errSelInternalMalformed   = b20ErrorSel("InternalCallMalformed(bytes)")
	errSelInternalFailed      = b20ErrorSel("InternalCallFailed(bytes)")
	errSelInvalidVariant      = b20ErrorSel("InvalidVariant()")
	errSelTokenExists         = b20ErrorSel("TokenAlreadyExists(address)")
	errSelInitCallFailed      = b20ErrorSel("InitCallFailed(uint256)")
	errSelUnauthorized        = b20ErrorSel("Unauthorized()")
	errSelNoPendingAdmin      = b20ErrorSel("NoPendingAdmin()")
	errSelZeroAddress         = b20ErrorSel("ZeroAddress()")
	errSelAccountNotSeizable  = b20ErrorSel("AccountNotSeizable(address)")
	errSelPanic               = b20ErrorSel("Panic(uint256)")
	errSelFeatureNotActive    = b20ErrorSel("FeatureNotActivated(bytes32)")
	errSelAlreadyActivated    = b20ErrorSel("AlreadyActivated(bytes32)")
	errSelUnauthorizedAddr    = b20ErrorSel("Unauthorized(address)")
	errSelZeroAdminAddress    = b20ErrorSel("ZeroAdminAddress()")
)

// revPanic mirrors Solidity's Panic(code): 0x11 arithmetic over/underflow,
// 0x21 invalid enum value.
func revPanic(code byte) error {
	return revB20("Panic(uint256)", errSelPanic, wU8(code))
}
