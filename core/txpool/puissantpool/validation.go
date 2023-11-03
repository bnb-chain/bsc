package puissantpool

import (
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

var (

	// ErrAlreadyKnown is returned if the transactions is already contained
	// within the pool.
	ErrAlreadyKnown = errors.New("already known")

	// ErrInvalidSender is returned if the transaction contains an invalid signature.
	ErrInvalidSender = errors.New("invalid sender")

	// ErrUnderpriced is returned if a transaction's gas price is below the minimum
	// configured for the transaction pool.
	ErrUnderpriced = errors.New("transaction underpriced")

	// ErrReplaceUnderpriced is returned if a transaction is attempted to be replaced
	// with a different one without the required price bump.
	ErrReplaceUnderpriced = errors.New("replacement transaction underpriced")

	// ErrAccountLimitExceeded is returned if a transaction would exceed the number
	// allowed by a pool for a single account.
	ErrAccountLimitExceeded = errors.New("account limit exceeded")

	// ErrGasLimit is returned if a transaction's requested gas limit exceeds the
	// maximum allowance of the current block.
	ErrGasLimit = errors.New("exceeds block gas limit")

	// ErrNegativeValue is a sanity error to ensure no one is able to specify a
	// transaction with a negative value.
	ErrNegativeValue = errors.New("negative value")

	// ErrOversizedData is returned if the input data of a transaction is greater
	// than some meaningful limit a user might use. This is not a consensus error
	// making the transaction invalid, rather a DOS protection.
	ErrOversizedData = errors.New("oversized data")

	// ErrFutureReplacePending is returned if a future transaction replaces a pending
	// transaction. Future transactions should only be able to replace other future transactions.
	ErrFutureReplacePending = errors.New("future transaction tries to replace pending")
)

func (pool *PuissantPool) validatePuissantTxs(txs types.Transactions) error {

	head := pool.currentHead.Load()
	gasTip := pool.gasTip.Load()
	for index, tx := range txs {

		// Before performing any expensive validations, sanity check that the tx is
		// smaller than the maximum limit the pool can meaningfully handle
		if tx.Size() > txMaxSize {
			return fmt.Errorf("%w: transaction size %v, limit %v", ErrOversizedData, tx.Size(), txMaxSize)
		}
		// Ensure only transactions that have been enabled are accepted
		if !pool.chainconfig.IsBerlin(head.Number) && tx.Type() != types.LegacyTxType {
			return fmt.Errorf("%w: type %d rejected, pool not yet in Berlin", core.ErrTxTypeNotSupported, tx.Type())
		}
		if !pool.chainconfig.IsLondon(head.Number) && tx.Type() == types.DynamicFeeTxType {
			return fmt.Errorf("%w: type %d rejected, pool not yet in London", core.ErrTxTypeNotSupported, tx.Type())
		}
		if !pool.chainconfig.IsCancun(head.Number, head.Time) && tx.Type() == types.BlobTxType {
			return fmt.Errorf("%w: type %d rejected, pool not yet in Cancun", core.ErrTxTypeNotSupported, tx.Type())
		}
		// Check whether the init code size has been exceeded
		if pool.chainconfig.IsShanghai(head.Number, head.Time) && tx.To() == nil && len(tx.Data()) > params.MaxInitCodeSize {
			return fmt.Errorf("%w: code size %v, limit %v", core.ErrMaxInitCodeSizeExceeded, len(tx.Data()), params.MaxInitCodeSize)
		}
		// Transactions can't be negative. This may never happen using RLP decoded
		// transactions but may occur for transactions created using the RPC.
		if tx.Value().Sign() < 0 {
			return ErrNegativeValue
		}
		// Ensure the transaction doesn't exceed the current block limit gas
		if head.GasLimit < tx.Gas() {
			return ErrGasLimit
		}
		// Sanity check for extremely large numbers (supported by RLP or RPC)
		if tx.GasFeeCap().BitLen() > 256 {
			return core.ErrFeeCapVeryHigh
		}
		if tx.GasTipCap().BitLen() > 256 {
			return core.ErrTipVeryHigh
		}
		// Ensure gasFeeCap is greater than or equal to gasTipCap
		if tx.GasFeeCapIntCmp(tx.GasTipCap()) < 0 {
			return core.ErrTipAboveFeeCap
		}
		// Make sure the transaction is signed properly
		from, err := types.Sender(pool.signer, tx)
		if err != nil {
			return ErrInvalidSender
		}

		for _, blackAddr := range types.NanoBlackList {
			if from == blackAddr || (tx.To() != nil && *tx.To() == blackAddr) {
				return ErrInBlackList
			}
		}

		// Ensure the transaction has more gas than the bare minimum needed to cover
		// the transaction metadata
		intrGas, err := core.IntrinsicGas(tx.Data(), tx.AccessList(), tx.To() == nil, true, pool.chainconfig.IsIstanbul(head.Number), pool.chainconfig.IsShanghai(head.Number, head.Time))
		if err != nil {
			return err
		}
		if tx.Gas() < intrGas {
			return fmt.Errorf("%w: needed %v, allowed %v", core.ErrIntrinsicGas, intrGas, tx.Gas())
		}
		// Ensure the gasprice is high enough to cover the requirement of the calling
		// pool and/or block producer
		if tx.GasTipCapIntCmp(gasTip) < 0 {
			return fmt.Errorf("%w: tip needed %v, tip permitted %v", ErrUnderpriced, gasTip, tx.GasTipCap())
		}

		validNonce := pool.currentState.GetNonce(from)
		if index == 0 && validNonce != tx.Nonce() {
			return fmt.Errorf("invalid payment tx nonce, have %d, want %d", tx.Nonce(), validNonce)

		} else if validNonce > tx.Nonce() {
			return core.ErrNonceTooLow
		}

		if index == 0 && pool.currentState.GetBalance(from).Cmp(tx.Cost()) < 0 {
			return core.ErrInsufficientFunds
		}
	}
	return nil
}
