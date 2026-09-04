package vm

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// Typed revert data for the CAS20 precompiles. Business-rule failures use ABI
// custom errors; malformed calldata and unknown selectors revert with empty
// returndata (BEP-702 3.2).

// cas20ErrSigs accumulates every error signature the implementation registers, so
// a test can read what the code actually raises rather than a list of it. That is
// what keeps a newly added error from slipping past the overload check.
var cas20ErrSigs = map[string][4]byte{}

func eventTopic(sig string) common.Hash {
	return crypto.Keccak256Hash([]byte(sig))
}

// cas20ErrorSel also registers the signature, for revert-data assertions.
func cas20ErrorSel(sig string) [4]byte {
	var s [4]byte
	copy(s[:], crypto.Keccak256([]byte(sig)))
	cas20ErrSigs[sig] = s
	return s
}

// cas20RevertError carries an ABI-encoded revert payload through the internal
// call chain. It is never returned to the EVM directly; finishCAS20 converts it.
type cas20RevertError struct {
	sig  string
	data []byte
}

func (e *cas20RevertError) Error() string { return fmt.Sprintf("cas20 revert: %s", e.sig) }

// Is lets errors.Is(err, ErrExecutionReverted) hold before finishCAS20 converts
// the typed revert at the precompile boundary.
func (e *cas20RevertError) Is(target error) bool { return target == ErrExecutionReverted }

// finishCAS20 converts a typed revert into (returndata, ErrExecutionReverted) at
// the precompile boundary. All other results pass through unchanged.
func finishCAS20(ret []byte, err error) ([]byte, error) {
	// A refused call form is a revert, not an exceptional halt: BEP-702 3.2 says
	// DELEGATECALL and CALLCODE MUST revert, and names both this and the
	// write-protection failure as ABI errors. Returning the sentinels straight to
	// the EVM would exhaust the caller's gas and hand back no returndata, so an
	// integrator could neither decode the reason nor keep the gas — a footgun no
	// contract at an ordinary address has. Converted here rather than at each of
	// the twenty guard sites, which keep the plain sentinel internally.
	switch {
	case errors.Is(err, ErrCAS20DelegateCall):
		err = revCAS20("DelegateCallNotAllowed()", errSelDelegateCallDenied)
	case errors.Is(err, ErrWriteProtection):
		err = revCAS20("StaticCallNotAllowed()", errSelStaticCallDenied)
	}
	var rev *cas20RevertError
	if errors.As(err, &rev) {
		return rev.data, ErrExecutionReverted
	}
	return ret, err
}

// finishCAS20Metered is finishCAS20 for a call that has already charged gas. An
// exhausted budget outranks whatever the logic returned: a charge that could not
// be covered is an out-of-gas exception, not a revert, however the handler
// reported it. Guards that run before any charge — a delegated call, a
// value-bearing one — can use finishCAS20 directly, since there is nothing to have
// exhausted yet.
func finishCAS20Metered(ctx *PrecompileContext, ret []byte, err error) ([]byte, error) {
	if ctx.writeProtectionViolated() {
		return finishCAS20(nil, ErrWriteProtection)
	}
	if ctx.OutOfGas() {
		return nil, ErrOutOfGas
	}
	return finishCAS20(ret, err)
}

func revCAS20(sig string, sel [4]byte, words ...common.Hash) error {
	data := make([]byte, 4+32*len(words))
	copy(data, sel[:])
	for i := range words {
		copy(data[4+i*32:], words[i][:])
	}
	return &cas20RevertError{sig: sig, data: data}
}

func revCAS20Bytes(sig string, sel [4]byte, payload []byte) error {
	return &cas20RevertError{sig: sig, data: append(sel[:], encodeTuple(abiBytes(payload))...)}
}

// revCAS20StringBytes builds a revert carrying a string and a bytes argument, the
// shape BSC's system contracts use to report a rejected parameter change.
func revCAS20StringBytes(sig string, sel [4]byte, key string, value []byte) error {
	return &cas20RevertError{sig: sig, data: append(sel[:], encodeTuple(abiString(key), abiBytes(value))...)}
}

func wU256(v *uint256.Int) common.Hash { return v.Bytes32() }
func wU64(v uint64) common.Hash        { return uint256.NewInt(v).Bytes32() }
func wU8(v byte) common.Hash           { var h common.Hash; h[31] = v; return h }

// Registered error selectors (BEP-702 error surface).
var (
	errSelNonPayable          = cas20ErrorSel("NonPayable()")
	errSelInvalidReceiver     = cas20ErrorSel("InvalidReceiver(address)")
	errSelInvalidSender       = cas20ErrorSel("InvalidSender(address)")
	errSelInvalidSpender      = cas20ErrorSel("InvalidSpender(address)")
	errSelInvalidApprover     = cas20ErrorSel("InvalidApprover(address)")
	errSelInsufficientBalance = cas20ErrorSel("InsufficientBalance(address,uint256,uint256)")
	errSelInsufficientAllow   = cas20ErrorSel("InsufficientAllowance(address,uint256,uint256)")
	errSelSupplyCapExceeded   = cas20ErrorSel("SupplyCapExceeded(uint256,uint256)")
	errSelInvalidSupplyCap    = cas20ErrorSel("InvalidSupplyCap(uint256,uint256)")
	errSelContractPaused      = cas20ErrorSel("ContractPaused(uint8)")
	errSelExpiredSignature    = cas20ErrorSel("ExpiredSignature(uint256)")
	errSelInvalidSigner       = cas20ErrorSel("InvalidSigner(address,address)")
	errSelACUnauthorized      = cas20ErrorSel("AccessControlUnauthorizedAccount(address,bytes32)")
	errSelACBadConfirmation   = cas20ErrorSel("AccessControlBadConfirmation()")
	errSelLastAdminRenounce   = cas20ErrorSel("LastAdminCannotRenounce()")
	errSelNotSoleAdmin        = cas20ErrorSel("NotSoleAdmin()")
	errSelPolicyForbids       = cas20ErrorSel("PolicyForbids(bytes32,uint64)")
	// Two forms: the registry answers about a policy the caller named, so the id
	// adds nothing; a token binding one reports which id it could not find
	// (IPolicyRegistry vs IB20).
	errSelPolicyNotFound     = cas20ErrorSel("PolicyNotFound()")
	errSelPolicyNotFoundID   = cas20ErrorSel("PolicyNotFound(uint64)")
	errSelUnsupportedScope   = cas20ErrorSel("UnsupportedPolicyType(bytes32)")
	errSelIncompatibleType   = cas20ErrorSel("IncompatiblePolicyType()")
	errSelInvalidChildPolicy = cas20ErrorSel("InvalidChildPolicy(uint64)")
	errSelChildrenOutOfRange = cas20ErrorSel("ChildPoliciesOutsideOfRange()")
	errSelBatchTooLarge      = cas20ErrorSel("BatchSizeTooLarge(uint256)")
	errSelEmptyBatch         = cas20ErrorSel("EmptyBatch()")
	errSelLengthMismatch     = cas20ErrorSel("LengthMismatch(uint256,uint256)")
	errSelEmptyFeatureSet    = cas20ErrorSel("EmptyFeatureSet()")
	errSelInvalidMultiplier  = cas20ErrorSel("InvalidMultiplier()")
	errSelEffectiveAtInPast  = cas20ErrorSel("EffectiveAtInPast(uint256)")
	errSelEffectiveAtTooFar  = cas20ErrorSel("EffectiveAtTooFar(uint256)")
	errSelUIMulExists        = cas20ErrorSel("UIMultiplierUpdateExists(uint256)")
	errSelUIMulMissing       = cas20ErrorSel("UIMultiplierUpdateDoesNotExist()")
	errSelInvalidMetadataKey = cas20ErrorSel("InvalidMetadataKey()")
	errSelAnnounceInProgress = cas20ErrorSel("AnnouncementInProgress()")
	errSelAnnounceIdUsed     = cas20ErrorSel("AnnouncementIdAlreadyUsed(string)")
	errSelInternalMalformed  = cas20ErrorSel("InternalCallMalformed(bytes)")
	errSelInternalFailed     = cas20ErrorSel("InternalCallFailed(bytes)")
	errSelInvalidVariant     = cas20ErrorSel("InvalidVariant()")
	errSelTokenExists        = cas20ErrorSel("TokenAlreadyExists(address)")
	errSelInitCallFailed     = cas20ErrorSel("InitCallFailed(uint256)")
	errSelUnauthorized       = cas20ErrorSel("Unauthorized()")
	errSelNoPendingAdmin     = cas20ErrorSel("NoPendingAdmin()")
	errSelZeroAddress        = cas20ErrorSel("ZeroAddress()")
	errSelAccountNotSeizable = cas20ErrorSel("AccountNotSeizable(address)")
	errSelPanic              = cas20ErrorSel("Panic(uint256)")
	errSelFeatureNotActive   = cas20ErrorSel("FeatureNotActivated(bytes32)")
	errSelAlreadyActivated   = cas20ErrorSel("AlreadyActivated(bytes32)")
	errSelUnauthorizedAddr   = cas20ErrorSel("Unauthorized(address)")
	errSelInvalidValue       = cas20ErrorSel("InvalidValue(string,bytes)")
	errSelUnknownParam       = cas20ErrorSel("UnknownParam(string,bytes)")
	errSelDelegateCallDenied = cas20ErrorSel("DelegateCallNotAllowed()")
	errSelStaticCallDenied   = cas20ErrorSel("StaticCallNotAllowed()")
	errSelUnsupportedVersion = cas20ErrorSel("UnsupportedVersion(uint8,uint8)")
	errSelInvalidDecimals    = cas20ErrorSel("InvalidDecimals(uint8)")
	errSelMissingField       = cas20ErrorSel("MissingRequiredField(string)")
	errSelInvalidCurrency    = cas20ErrorSel("InvalidCurrency(string)")
)

// revPanic mirrors Solidity's Panic(code). Only 0x11, arithmetic overflow and
// underflow, arises here: a malformed argument is a decode failure and reverts
// with empty returndata, which is what Solidity's external decoder does.
func revPanic(code byte) error {
	return revCAS20("Panic(uint256)", errSelPanic, wU8(code))
}
